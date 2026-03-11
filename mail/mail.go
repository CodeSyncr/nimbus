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

// SMTPDriver sends via SMTP.
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

// ── Provider-specific drivers (SMTP-backed) ─────────────────────

// SESDriver is a thin wrapper around SMTPDriver configured for Amazon SES SMTP.
// Use the SMTP credentials from your SES console.
type SESDriver struct {
	smtp *SMTPDriver
}

// NewSESDriver creates an SES driver. Example addr: "email-smtp.us-east-1.amazonaws.com:587".
func NewSESDriver(addr string, auth smtp.Auth, fromAddr string) *SESDriver {
	return &SESDriver{smtp: NewSMTPDriver(addr, auth, fromAddr)}
}

func (d *SESDriver) Send(m *Message) error {
	return d.smtp.Send(m)
}

// MailgunDriver wraps SMTPDriver for Mailgun.
// Example addr: "smtp.mailgun.org:587".
type MailgunDriver struct {
	smtp *SMTPDriver
}

func NewMailgunDriver(addr string, auth smtp.Auth, fromAddr string) *MailgunDriver {
	return &MailgunDriver{smtp: NewSMTPDriver(addr, auth, fromAddr)}
}

func (d *MailgunDriver) Send(m *Message) error {
	return d.smtp.Send(m)
}

// SendGridDriver wraps SMTPDriver for SendGrid.
// Example addr: "smtp.sendgrid.net:587".
type SendGridDriver struct {
	smtp *SMTPDriver
}

func NewSendGridDriver(addr string, auth smtp.Auth, fromAddr string) *SendGridDriver {
	return &SendGridDriver{smtp: NewSMTPDriver(addr, auth, fromAddr)}
}

func (d *SendGridDriver) Send(m *Message) error {
	return d.smtp.Send(m)
}

// PostmarkDriver wraps SMTPDriver for Postmark.
// Example addr: "smtp.postmarkapp.com:587".
type PostmarkDriver struct {
	smtp *SMTPDriver
}

func NewPostmarkDriver(addr string, auth smtp.Auth, fromAddr string) *PostmarkDriver {
	return &PostmarkDriver{smtp: NewSMTPDriver(addr, auth, fromAddr)}
}

func (d *PostmarkDriver) Send(m *Message) error {
	return d.smtp.Send(m)
}

