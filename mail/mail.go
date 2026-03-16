package mail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime"
	"net/smtp"
	"path/filepath"
	"strings"
)

// Attachment represents a file attachment on an email.
type Attachment struct {
	Filename string
	Data     []byte
	MIMEType string // e.g. "application/pdf". Auto-detected if empty.
}

// Message represents an email (plan: mail drivers SMTP, etc.).
type Message struct {
	From        string
	To          []string
	Cc          []string
	Bcc         []string
	ReplyTo     string
	Subject     string
	Body        string
	HTML        bool
	Attachments []Attachment
}

// NewMessage creates a new Message with the given subject.
func NewMessage(subject string) *Message {
	return &Message{Subject: subject}
}

// SetFrom sets the sender address.
func (m *Message) SetFrom(from string) *Message { m.From = from; return m }

// SetTo sets the primary recipients.
func (m *Message) SetTo(to ...string) *Message { m.To = to; return m }

// AddCc adds CC recipients.
func (m *Message) AddCc(cc ...string) *Message { m.Cc = append(m.Cc, cc...); return m }

// AddBcc adds BCC recipients.
func (m *Message) AddBcc(bcc ...string) *Message { m.Bcc = append(m.Bcc, bcc...); return m }

// SetReplyTo sets the reply-to address.
func (m *Message) SetReplyTo(replyTo string) *Message { m.ReplyTo = replyTo; return m }

// SetBody sets the email body. Set html=true for HTML content.
func (m *Message) SetBody(body string, html bool) *Message { m.Body = body; m.HTML = html; return m }

// Attach adds a file attachment.
func (m *Message) Attach(filename string, data []byte, mimeType ...string) *Message {
	a := Attachment{Filename: filename, Data: data}
	if len(mimeType) > 0 {
		a.MIMEType = mimeType[0]
	}
	m.Attachments = append(m.Attachments, a)
	return m
}

// AllRecipients returns To + Cc + Bcc for envelope delivery.
func (m *Message) AllRecipients() []string {
	all := make([]string, 0, len(m.To)+len(m.Cc)+len(m.Bcc))
	all = append(all, m.To...)
	all = append(all, m.Cc...)
	all = append(all, m.Bcc...)
	return all
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
	var buf bytes.Buffer
	boundary := "NimbusBoundary7e3a1c"

	hasAttachments := len(m.Attachments) > 0

	// Headers.
	buf.WriteString(fmt.Sprintf("From: %s\r\n", m.From))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(m.To, ",")))
	if len(m.Cc) > 0 {
		buf.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(m.Cc, ",")))
	}
	if m.ReplyTo != "" {
		buf.WriteString(fmt.Sprintf("Reply-To: %s\r\n", m.ReplyTo))
	}
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", m.Subject))
	buf.WriteString("MIME-Version: 1.0\r\n")

	if hasAttachments {
		buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%s\r\n", boundary))
		buf.WriteString("\r\n")

		// Body part.
		buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		contentType := "text/plain; charset=utf-8"
		if m.HTML {
			contentType = "text/html; charset=utf-8"
		}
		buf.WriteString(fmt.Sprintf("Content-Type: %s\r\n\r\n", contentType))
		buf.WriteString(m.Body)
		buf.WriteString("\r\n")

		// Attachments.
		for _, a := range m.Attachments {
			mimeType := a.MIMEType
			if mimeType == "" {
				mimeType = mime.TypeByExtension(filepath.Ext(a.Filename))
				if mimeType == "" {
					mimeType = "application/octet-stream"
				}
			}
			buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
			buf.WriteString(fmt.Sprintf("Content-Type: %s; name=\"%s\"\r\n", mimeType, a.Filename))
			buf.WriteString("Content-Transfer-Encoding: base64\r\n")
			buf.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n\r\n", a.Filename))
			buf.WriteString(base64.StdEncoding.EncodeToString(a.Data))
			buf.WriteString("\r\n")
		}
		buf.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else {
		contentType := "text/plain; charset=utf-8"
		if m.HTML {
			contentType = "text/html; charset=utf-8"
		}
		buf.WriteString(fmt.Sprintf("Content-Type: %s\r\n", contentType))
		buf.WriteString("\r\n")
		buf.WriteString(m.Body)
	}

	addr := d.Addr
	if !strings.Contains(addr, ":") {
		addr = addr + ":25"
	}
	recipients := m.AllRecipients()
	return smtp.SendMail(addr, d.Auth, m.From, recipients, buf.Bytes())
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

// MailgunSMTPDriver wraps SMTPDriver for Mailgun SMTP transport.
// For the native API driver, use MailgunAPIDriver instead.
// Example addr: "smtp.mailgun.org:587".
type MailgunSMTPDriver struct {
	smtp *SMTPDriver
}

func NewMailgunSMTPDriver(addr string, auth smtp.Auth, fromAddr string) *MailgunSMTPDriver {
	return &MailgunSMTPDriver{smtp: NewSMTPDriver(addr, auth, fromAddr)}
}

func (d *MailgunSMTPDriver) Send(m *Message) error {
	return d.smtp.Send(m)
}

// SendGridSMTPDriver wraps SMTPDriver for SendGrid SMTP transport.
// For the native API driver, use SendGridDriver instead.
// Example addr: "smtp.sendgrid.net:587".
type SendGridSMTPDriver struct {
	smtp *SMTPDriver
}

func NewSendGridSMTPDriver(addr string, auth smtp.Auth, fromAddr string) *SendGridSMTPDriver {
	return &SendGridSMTPDriver{smtp: NewSMTPDriver(addr, auth, fromAddr)}
}

func (d *SendGridSMTPDriver) Send(m *Message) error {
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
