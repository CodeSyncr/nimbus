package mail

import (
	"bytes"
	"fmt"
	"net/smtp"
	"strings"
)

// Message represents an email (plan: mail drivers SMTP, etc.).
type Message struct {
	From    string
	To      []string
	Subject string
	Body    string
	HTML    bool
}

// Driver sends emails.
type Driver interface {
	Send(m *Message) error
}

// SMTPDriver sends via SMTP (plan: SMTP driver).
type SMTPDriver struct {
	Addr     string
	Auth     smtp.Auth
	FromAddr string
}

// NewSMTPDriver returns an SMTP driver. auth can be nil for no auth.
func NewSMTPDriver(addr string, auth smtp.Auth, fromAddr string) *SMTPDriver {
	return &SMTPDriver{Addr: addr, Auth: auth, FromAddr: fromAddr}
}

// Send sends the message via SMTP.
func (d *SMTPDriver) Send(m *Message) error {
	contentType := "text/plain; charset=utf-8"
	if m.HTML {
		contentType = "text/html; charset=utf-8"
	}
	headers := []string{
		fmt.Sprintf("From: %s", m.From),
		fmt.Sprintf("To: %s", strings.Join(m.To, ",")),
		fmt.Sprintf("Subject: %s", m.Subject),
		"MIME-version: 1.0",
		fmt.Sprintf("Content-Type: %s", contentType),
		"",
	}
	var buf bytes.Buffer
	for _, h := range headers {
		buf.WriteString(h + "\r\n")
	}
	buf.WriteString(m.Body)
	addr := d.Addr
	if !strings.Contains(addr, ":") {
		addr = addr + ":25"
	}
	return smtp.SendMail(addr, d.Auth, m.From, m.To, buf.Bytes())
}

// Default driver (set by app).
var Default Driver

// Send is a shortcut for Default.Send (if Default is set).
func Send(m *Message) error {
	if Default == nil {
		return fmt.Errorf("mail: no driver set")
	}
	return Default.Send(m)
}
