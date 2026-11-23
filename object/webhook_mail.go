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

package object

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

const mailTimeout = 30 * time.Second

// MailAttachment is a file sent with an email.
type MailAttachment struct {
	Name        string
	ContentType string
	Data        []byte
}

func (webhook *Webhook) getSmtpPort() int {
	if webhook.SmtpPort != 0 {
		return webhook.SmtpPort
	}
	return 465
}

func (webhook *Webhook) getMailFrom() string {
	if webhook.SmtpFrom != "" {
		return webhook.SmtpFrom
	}
	return webhook.SmtpUsername
}

func (webhook *Webhook) checkMail() error {
	if webhook.SmtpHost == "" || strings.ContainsAny(webhook.SmtpHost, "/:@ \t\r\n") {
		return fmt.Errorf("invalid SMTP server: %s, it should be a host name such as smtp.example.com", webhook.SmtpHost)
	}
	if webhook.SmtpPort < 0 || webhook.SmtpPort > 65535 {
		return fmt.Errorf("invalid SMTP port: %d", webhook.SmtpPort)
	}
	from, err := mail.ParseAddress(webhook.getMailFrom())
	if err != nil || from.Address == "" {
		return fmt.Errorf("invalid sender: %s, it should be an email address, or the SMTP username should be one", webhook.getMailFrom())
	}
	if len(webhook.Recipients) == 0 {
		return fmt.Errorf("the email needs at least one recipient")
	}
	for _, recipient := range webhook.Recipients {
		address, err := mail.ParseAddress(recipient)
		if err != nil || address.Address != recipient {
			return fmt.Errorf("invalid recipient: %s, it should be an email address such as ops@example.com", recipient)
		}
	}
	return nil
}

// loginAuth is the LOGIN authentication, which servers such as Outlook take instead of PLAIN.
type loginAuth struct {
	username, password string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS {
		return "", nil, errors.New("the SMTP server does not use TLS, the password is not sent")
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	prompt := strings.ToLower(string(fromServer))
	if strings.Contains(prompt, "username") {
		return []byte(a.username), nil
	}
	if strings.Contains(prompt, "password") {
		return []byte(a.password), nil
	}
	return nil, fmt.Errorf("unexpected prompt of the SMTP server: %s", fromServer)
}

// encodeHeader keeps a header value on one line and encodes what is not ASCII.
func encodeHeader(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	return mime.QEncoding.Encode("utf-8", value)
}

func writeBase64(buf *bytes.Buffer, data []byte) {
	encoded := base64.StdEncoding.EncodeToString(data)
	for len(encoded) > 76 {
		buf.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	buf.WriteString(encoded + "\r\n")
}

func (webhook *Webhook) buildMail(subject string, body string, attachments []*MailAttachment) []byte {
	from, _ := mail.ParseAddress(webhook.getMailFrom())
	if from.Name == "" {
		from.Name = "Casvisor"
	}
	id := make([]byte, 12)
	_, _ = rand.Read(id)
	domain := from.Address[strings.LastIndex(from.Address, "@")+1:]

	buf := &bytes.Buffer{}
	buf.WriteString("From: " + from.String() + "\r\n")
	buf.WriteString("To: " + strings.Join(webhook.Recipients, ", ") + "\r\n")
	buf.WriteString("Subject: " + encodeHeader(subject) + "\r\n")
	buf.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	buf.WriteString(fmt.Sprintf("Message-ID: <%s@%s>\r\n", hex.EncodeToString(id), domain))
	buf.WriteString("MIME-Version: 1.0\r\n")

	text := strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
	if len(attachments) == 0 {
		buf.WriteString("Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
		writeBase64(buf, []byte(text))
		return buf.Bytes()
	}

	boundary := "casvisor-" + hex.EncodeToString(id)
	buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%q\r\n\r\n", boundary))
	buf.WriteString("--" + boundary + "\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
	writeBase64(buf, []byte(text))
	for _, attachment := range attachments {
		buf.WriteString("--" + boundary + "\r\n")
		buf.WriteString(fmt.Sprintf("Content-Type: %s; name=%q\r\n", attachment.ContentType, attachment.Name))
		buf.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=%q\r\n", attachment.Name))
		buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		writeBase64(buf, attachment.Data)
	}
	buf.WriteString("--" + boundary + "--\r\n")
	return buf.Bytes()
}

// sendMail sends the email with the SMTP server of the webhook: with TLS from the start on port 465,
// and on the other ports with STARTTLS when the server offers it, which it must when there is a password.
func (webhook *Webhook) sendMail(subject string, body string, attachments []*MailAttachment) error {
	err := webhook.checkMail()
	if err != nil {
		return err
	}

	port := webhook.getSmtpPort()
	address := net.JoinHostPort(webhook.SmtpHost, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: webhookTimeout, Control: refuseMetadataAddress}
	tlsConfig := &tls.Config{ServerName: webhook.SmtpHost, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	if port == 465 {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, tlsConfig)
	} else {
		conn, err = dialer.Dial("tcp", address)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to the SMTP server: %s", err.Error())
	}
	_ = conn.SetDeadline(time.Now().Add(mailTimeout))

	client, err := smtp.NewClient(conn, webhook.SmtpHost)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("the SMTP server: %s", err.Error())
	}
	defer func() { _ = client.Close() }()

	if port != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			err = client.StartTLS(tlsConfig)
			if err != nil {
				return fmt.Errorf("STARTTLS with the SMTP server failed: %s", err.Error())
			}
		}
	}

	if webhook.SmtpUsername != "" || webhook.Secret != "" {
		_, isTls := client.TLSConnectionState()
		if !isTls {
			return fmt.Errorf("the SMTP server does not offer STARTTLS on port %d, the password is not sent, use port 465 or 587", port)
		}
		var auth smtp.Auth = smtp.PlainAuth("", webhook.SmtpUsername, webhook.Secret, webhook.SmtpHost)
		if ok, mechanisms := client.Extension("AUTH"); ok && !strings.Contains(strings.ToUpper(mechanisms), "PLAIN") && strings.Contains(strings.ToUpper(mechanisms), "LOGIN") {
			auth = &loginAuth{username: webhook.SmtpUsername, password: webhook.Secret}
		}
		err = client.Auth(auth)
		if err != nil {
			return fmt.Errorf("the SMTP server refused the username or the password: %s", err.Error())
		}
	}

	from, _ := mail.ParseAddress(webhook.getMailFrom())
	err = client.Mail(from.Address)
	if err != nil {
		return fmt.Errorf("the SMTP server refused the sender: %s", err.Error())
	}
	for _, recipient := range webhook.Recipients {
		err = client.Rcpt(recipient)
		if err != nil {
			return fmt.Errorf("the SMTP server refused the recipient: %s: %s", recipient, err.Error())
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("the SMTP server: %s", err.Error())
	}
	_, err = writer.Write(webhook.buildMail(subject, body, attachments))
	if err == nil {
		err = writer.Close()
	}
	if err != nil {
		return fmt.Errorf("the SMTP server did not take the email: %s", err.Error())
	}
	return client.Quit()
}
