// Copyright 2026 The casbin Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package syslog forwards audit events to a syslog server, as RFC 5424 messages with a JSON body or as CEF.
// The standard log/syslog does not build on Windows, so this is a small sender of its own.
package syslog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/beego/beego/logs"
)

const (
	SeverityWarning = 4
	SeverityNotice  = 5
	SeverityInfo    = 6

	// the "log audit" facility of RFC 5424
	facility = 13

	FormatRfc5424 = "rfc5424"
	FormatCef     = "cef"

	queueSize     = 10000
	maxValueSize  = 2000
	writeTimeout  = 5 * time.Second
	maxRetryDelay = 30 * time.Second
)

// Field is one value of an event: Key names it in the JSON body, CefKey in CEF,
// where a custom string key such as cs1 gets Key as its label.
type Field struct {
	Key    string
	CefKey string
	Value  string
	// the value in CEF when it is written differently there, such as a time in milliseconds
	CefValue string
}

type Event struct {
	Type     string
	Name     string
	Severity int
	Time     time.Time
	Fields   []Field
}

type sender struct {
	network string
	address string
	format  string
	queue   chan Event
	dropped atomic.Int64
}

var (
	current  *sender
	hostname string
	once     sync.Once
)

// Start forwards the events to syslogAddress, such as udp://10.0.0.5:514 or tcp://10.0.0.5:6514; an empty address turns it off.
func Start(address string, format string) error {
	var err error
	once.Do(func() {
		if address == "" {
			return
		}

		network := "udp"
		if i := strings.Index(address, "://"); i >= 0 {
			network = strings.ToLower(address[:i])
			address = address[i+3:]
		}
		if network != "udp" && network != "tcp" {
			err = fmt.Errorf("invalid syslogAddress: the scheme should be udp:// or tcp://")
			return
		}
		if _, _, splitErr := net.SplitHostPort(address); splitErr != nil {
			err = fmt.Errorf("invalid syslogAddress: %s", splitErr.Error())
			return
		}

		format = strings.ToLower(format)
		if format == "" {
			format = FormatRfc5424
		}
		if format != FormatRfc5424 && format != FormatCef {
			err = fmt.Errorf("invalid syslogFormat: %s, it should be rfc5424 or cef", format)
			return
		}

		hostname, _ = os.Hostname()
		if hostname == "" {
			hostname = "-"
		}

		s := &sender{network: network, address: address, format: format, queue: make(chan Event, queueSize)}
		current = s
		go s.loop()
		go s.reportDropped()
		logs.Info("forwarding audit events to syslog: %s://%s as %s", network, address, format)
	})
	return err
}

// Send queues an event without waiting; when the queue is full the event is dropped and counted.
func Send(event Event) {
	s := current
	if s == nil {
		return
	}

	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	select {
	case s.queue <- event:
	default:
		s.dropped.Add(1)
	}
}

func (s *sender) reportDropped() {
	for range time.Tick(time.Minute) {
		if n := s.dropped.Swap(0); n > 0 {
			logs.Warning("syslog: dropped %d audit events, the queue of %d was full", n, queueSize)
		}
	}
}

func (s *sender) loop() {
	var conn net.Conn
	delay := time.Second
	failing := false
	var lastWarning time.Time
	warn := func(format string, args ...interface{}) {
		failing = true
		if time.Since(lastWarning) >= time.Minute {
			lastWarning = time.Now()
			logs.Warning("syslog: "+format, args...)
		}
	}

	for event := range s.queue {
		message := s.encode(event)
		for attempt := 0; ; attempt++ {
			if conn == nil {
				var err error
				conn, err = net.DialTimeout(s.network, s.address, writeTimeout)
				if err != nil {
					conn = nil
					warn("failed to connect to %s://%s: %s, retrying", s.network, s.address, err.Error())
					time.Sleep(delay)
					delay = min(delay*2, maxRetryDelay)
					continue
				}
			}

			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			_, err := conn.Write(message)
			if err == nil {
				if failing && s.network == "tcp" {
					logs.Info("syslog: connected to %s://%s again", s.network, s.address)
				}
				failing = false
				delay = time.Second
				break
			}

			_ = conn.Close()
			conn = nil
			warn("failed to send to %s://%s: %s, reconnecting", s.network, s.address, err.Error())
			// a UDP write fails only on a local error or an ICMP error of an earlier datagram, so it is retried once
			if s.network == "udp" && attempt > 0 {
				break
			}
		}
	}
}

func (s *sender) encode(event Event) []byte {
	var buf bytes.Buffer
	pri := facility*8 + event.Severity
	if s.format == FormatCef {
		fmt.Fprintf(&buf, "<%d>%s %s %s", pri, event.Time.Format(time.Stamp), hostname, formatCef(event))
	} else {
		fmt.Fprintf(&buf, "<%d>1 %s %s casvisor %d %s - %s", pri, event.Time.Format("2006-01-02T15:04:05.000Z07:00"), hostname, os.Getpid(), event.Type, formatJson(event))
	}
	if s.network == "tcp" {
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func truncate(s string) string {
	if len(s) <= maxValueSize {
		return s
	}
	return strings.ToValidUTF8(s[:maxValueSize], "") + "..."
}

func formatJson(event Event) []byte {
	var buf bytes.Buffer
	buf.WriteString(`{"type":`)
	writeJson(&buf, event.Type)
	buf.WriteString(`,"time":`)
	writeJson(&buf, event.Time.Format(time.RFC3339))
	for _, field := range event.Fields {
		buf.WriteByte(',')
		writeJson(&buf, field.Key)
		buf.WriteByte(':')
		writeJson(&buf, truncate(field.Value))
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

func writeJson(buf *bytes.Buffer, s string) {
	b, _ := json.Marshal(s)
	buf.Write(b)
}

var (
	cefHeaderEscaper    = strings.NewReplacer(`\`, `\\`, `|`, `\|`)
	cefExtensionEscaper = strings.NewReplacer(`\`, `\\`, `=`, `\=`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`)
)

func getCefSeverity(severity int) int {
	switch {
	case severity <= SeverityWarning:
		return 8
	case severity == SeverityNotice:
		return 5
	default:
		return 3
	}
}

func formatCef(event Event) string {
	var ext []string
	ext = append(ext, "rt="+strconv.FormatInt(event.Time.UnixMilli(), 10))
	for _, field := range event.Fields {
		value := field.Value
		if field.CefValue != "" {
			value = field.CefValue
		}
		if field.CefKey == "" || value == "" {
			continue
		}
		ext = append(ext, field.CefKey+"="+cefExtensionEscaper.Replace(truncate(value)))
		if strings.HasPrefix(field.CefKey, "cs") || strings.HasPrefix(field.CefKey, "cn") {
			ext = append(ext, field.CefKey+"Label="+cefExtensionEscaper.Replace(field.Key))
		}
	}

	return fmt.Sprintf("CEF:0|Casbin|Casvisor|1.0|%s|%s|%d|%s",
		cefHeaderEscaper.Replace(event.Type), cefHeaderEscaper.Replace(event.Name), getCefSeverity(event.Severity), strings.Join(ext, " "))
}
