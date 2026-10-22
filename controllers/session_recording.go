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

package controllers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/recording"
	"github.com/gorilla/websocket"
)

// GetSessionRecording
// @Title GetSessionRecording
// @Tag Session API
// @Description download a recording of a session, as a Casvisor recording or, for SSH and Telnet, as an asciicast
// @Param   id     query    string  true        "The id of the session"
// @Param   index     query    int  false        "Which recording of the session, from 0"
// @Param   format     query    string  false        "cast for an asciicast v2 file"
// @router /get-session-recording [get]
func (c *ApiController) GetSessionRecording() {
	sessionId := c.Input().Get("id")
	index, _ := strconv.Atoi(c.Input().Get("index"))

	session, err := object.GetConnSession(sessionId)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if session == nil {
		c.ResponseError(fmt.Sprintf("the session: %s does not exist", sessionId))
		return
	}
	if !c.checkOwner(session.Owner) {
		return
	}
	if index < 0 || index >= len(session.Recordings) {
		c.ResponseError("the session has no such recording")
		return
	}

	path, err := recording.GetPath(session.Recordings[index])
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			c.ResponseError("the recording has been deleted")
		} else {
			c.ResponseError(err.Error())
		}
		return
	}
	defer file.Close()

	user := c.GetSessionUser()
	object.AddAuditRecord(&object.AuditEvent{
		Organization: user.Owner,
		User:         user.Name,
		ClientIp:     c.getClientIp(),
		UserAgent:    c.getUserAgent(),
		Method:       "GET",
		Action:       "session-replay",
		Asset:        session.Asset,
		Detail:       session.Recordings[index],
	})

	c.EnableRender = false
	fileName := session.Name
	if index > 0 {
		fileName = fmt.Sprintf("%s-%d", fileName, index+1)
	}

	if c.Input().Get("format") == "cast" {
		reader := bufio.NewReaderSize(file, 64*1024)
		header, err := recording.ReadHeader(reader)
		if err != nil {
			c.ResponseError(err.Error())
			return
		}
		if header.Protocol != "SSH" && header.Protocol != "Telnet" {
			c.ResponseError("only SSH and Telnet recordings can be converted to asciicast")
			return
		}

		c.Ctx.Output.Header("Content-Type", "application/x-asciicast")
		c.Ctx.Output.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName+".cast"))
		writeAsciicast(c.Ctx.ResponseWriter, header, reader)
		return
	}

	info, err := file.Stat()
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.Ctx.Output.Header("Content-Type", "application/octet-stream")
	c.Ctx.Output.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName+".cvr"))
	c.Ctx.Output.Header("Cache-Control", "no-store")
	http.ServeContent(c.Ctx.ResponseWriter, c.Ctx.Request, fileName+".cvr", info.ModTime(), file)
}

// writeAsciicast converts a terminal recording to asciicast v2, which asciinema plays.
func writeAsciicast(w io.Writer, header *recording.Header, reader *bufio.Reader) {
	start, _ := time.Parse(time.RFC3339Nano, header.StartTime)
	out := bufio.NewWriter(w)
	defer out.Flush()

	line, _ := json.Marshal(map[string]interface{}{
		"version":   2,
		"width":     header.Cols,
		"height":    header.Rows,
		"timestamp": start.Unix(),
		"title":     header.Asset,
	})
	_, _ = out.Write(append(line, '\n'))

	// a frame can end in the middle of a UTF-8 character, which is kept for the next one
	var pending []byte
	for {
		frame, err := recording.ReadFrame(reader)
		if err != nil {
			return
		}

		seconds := float64(frame.Time) / 1000
		var event []interface{}
		switch frame.Kind {
		case recording.KindData:
			data := append(pending, frame.Data...)
			cut := len(data)
			for i := 1; i <= 3 && i <= len(data); i++ {
				if utf8.RuneStart(data[len(data)-i]) {
					if !utf8.FullRune(data[len(data)-i:]) {
						cut = len(data) - i
					}
					break
				}
			}
			pending = append([]byte{}, data[cut:]...)
			event = []interface{}{seconds, "o", string(data[:cut])}
		case recording.KindResize:
			size := map[string]int{}
			if json.Unmarshal(frame.Data, &size) != nil {
				continue
			}
			event = []interface{}{seconds, "r", fmt.Sprintf("%dx%d", size["cols"], size["rows"])}
		default:
			continue
		}

		line, err := json.Marshal(event)
		if err != nil {
			continue
		}
		_, err = out.Write(append(line, '\n'))
		if err != nil {
			return
		}
	}
}

// monitorRecording streams a VNC or RDP session to an administrator: the
// recording so far, so that the screen can be drawn from its start, then each
// new frame. Frames are sent as their kind byte followed by their data.
func (c *ApiController) monitorRecording(ws *websocket.Conn, stream *wsStream, rec *recording.Recording) {
	header, readSoFar, viewer, err := rec.Watch()
	if err != nil {
		stream.Close(SessionNotFound, err.Error())
		return
	}
	defer rec.Unwatch(viewer)

	go func() {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				rec.Unwatch(viewer)
				return
			}
		}
	}()

	send := func(frame *recording.Frame) error {
		return stream.writeBinary(append([]byte{frame.Kind}, frame.Data...))
	}

	err = stream.WriteJSONUnrecorded(map[string]interface{}{"type": "header", "header": header})
	if err == nil {
		err = readSoFar(send)
	}
	if err == nil {
		err = stream.WriteJSONUnrecorded(terminalMessage{Type: "connected"})
	}
	if err != nil {
		return
	}

	for frame := range viewer.Frames {
		if err = send(&frame); err != nil {
			return
		}
	}

	stream.Close(Normal, "The session has ended")
}
