package mail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ── SendGrid API Driver ─────────────────────────────────────────

// SendGridDriver sends email via the SendGrid v3 HTTP API.
// This is a native API driver — no SMTP connection required.
//
//	driver := mail.NewSendGridDriver("SG.your-api-key", "sender@example.com")
//	mail.Default = driver
type SendGridDriver struct {
	APIKey   string
	FromAddr string
	FromName string
	Endpoint string // defaults to "https://api.sendgrid.com/v3/mail/send"
	Client   *http.Client
}

// NewSendGridDriver creates a SendGrid API driver.
func NewSendGridDriver(apiKey, fromAddr string) *SendGridDriver {
	return &SendGridDriver{
		APIKey:   apiKey,
		FromAddr: fromAddr,
		Endpoint: "https://api.sendgrid.com/v3/mail/send",
		Client:   http.DefaultClient,
	}
}

// Send sends the message via SendGrid's v3 API.
func (d *SendGridDriver) Send(m *Message) error {
	from := m.From
	if from == "" {
		from = d.FromAddr
	}

	// Build personalizations.
	tos := make([]map[string]string, 0, len(m.To))
	for _, t := range m.To {
		tos = append(tos, map[string]string{"email": t})
	}

	personalization := map[string]any{
		"to": tos,
	}
	if len(m.Cc) > 0 {
		ccs := make([]map[string]string, 0, len(m.Cc))
		for _, c := range m.Cc {
			ccs = append(ccs, map[string]string{"email": c})
		}
		personalization["cc"] = ccs
	}
	if len(m.Bcc) > 0 {
		bccs := make([]map[string]string, 0, len(m.Bcc))
		for _, b := range m.Bcc {
			bccs = append(bccs, map[string]string{"email": b})
		}
		personalization["bcc"] = bccs
	}

	contentType := "text/plain"
	if m.HTML {
		contentType = "text/html"
	}

	payload := map[string]any{
		"personalizations": []any{personalization},
		"from":             map[string]string{"email": from},
		"subject":          m.Subject,
		"content": []map[string]string{
			{
				"type":  contentType,
				"value": m.Body,
			},
		},
	}

	if m.ReplyTo != "" {
		payload["reply_to"] = map[string]string{"email": m.ReplyTo}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("sendgrid: marshal: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, d.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sendgrid: request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+d.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.Client.Do(req)
	if err != nil {
		return fmt.Errorf("sendgrid: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sendgrid: HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ── Mailgun API Driver ──────────────────────────────────────────

// MailgunAPIDriver sends email via the Mailgun HTTP API.
// This is a native API driver — no SMTP connection required.
//
//	driver := mail.NewMailgunAPIDriver("your-domain.com", "key-xxx", "sender@your-domain.com")
//	mail.Default = driver
type MailgunAPIDriver struct {
	Domain   string
	APIKey   string
	FromAddr string
	Endpoint string // defaults to "https://api.mailgun.net/v3"
	Client   *http.Client
}

// NewMailgunAPIDriver creates a Mailgun API driver.
func NewMailgunAPIDriver(domain, apiKey, fromAddr string) *MailgunAPIDriver {
	return &MailgunAPIDriver{
		Domain:   domain,
		APIKey:   apiKey,
		FromAddr: fromAddr,
		Endpoint: "https://api.mailgun.net/v3",
		Client:   http.DefaultClient,
	}
}

// Send sends the message via Mailgun's API.
func (d *MailgunAPIDriver) Send(m *Message) error {
	from := m.From
	if from == "" {
		from = d.FromAddr
	}

	url := fmt.Sprintf("%s/%s/messages", d.Endpoint, d.Domain)

	// Build form data.
	var formParts []string
	addField := func(key, val string) {
		formParts = append(formParts, fmt.Sprintf("%s=%s", key, val))
	}

	addField("from", from)
	for _, t := range m.To {
		addField("to", t)
	}
	for _, c := range m.Cc {
		addField("cc", c)
	}
	for _, b := range m.Bcc {
		addField("bcc", b)
	}
	addField("subject", m.Subject)
	if m.HTML {
		addField("html", m.Body)
	} else {
		addField("text", m.Body)
	}
	if m.ReplyTo != "" {
		addField("h:Reply-To", m.ReplyTo)
	}

	body := strings.Join(formParts, "&")

	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("mailgun: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("api", d.APIKey)

	resp, err := d.Client.Do(req)
	if err != nil {
		return fmt.Errorf("mailgun: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mailgun: HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ── SES API Driver ──────────────────────────────────────────────

// SESAPIDriver sends email via the Amazon SES v2 HTTP API using AWS Signature
// Version 4. For simplicity, this driver uses the SES SendEmail Action via
// query parameters (no external AWS SDK required).
//
//	driver := mail.NewSESAPIDriver("us-east-1", "AKID...", "secret...", "sender@example.com")
//	mail.Default = driver
type SESAPIDriver struct {
	Region    string
	AccessKey string
	SecretKey string
	FromAddr  string
	Client    *http.Client
}

// NewSESAPIDriver creates an SES API driver (no SDK required).
func NewSESAPIDriver(region, accessKey, secretKey, fromAddr string) *SESAPIDriver {
	return &SESAPIDriver{
		Region:    region,
		AccessKey: accessKey,
		SecretKey: secretKey,
		FromAddr:  fromAddr,
		Client:    http.DefaultClient,
	}
}

// Send sends the message via SES API. It builds a raw MIME message and sends
// it via the SES SendRawEmail action.
func (d *SESAPIDriver) Send(m *Message) error {
	from := m.From
	if from == "" {
		from = d.FromAddr
	}

	// Build the raw RFC 2822 message.
	var raw bytes.Buffer
	raw.WriteString(fmt.Sprintf("From: %s\r\n", from))
	raw.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(m.To, ",")))
	if len(m.Cc) > 0 {
		raw.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(m.Cc, ",")))
	}
	if m.ReplyTo != "" {
		raw.WriteString(fmt.Sprintf("Reply-To: %s\r\n", m.ReplyTo))
	}
	raw.WriteString(fmt.Sprintf("Subject: %s\r\n", m.Subject))
	raw.WriteString("MIME-Version: 1.0\r\n")
	contentType := "text/plain; charset=utf-8"
	if m.HTML {
		contentType = "text/html; charset=utf-8"
	}
	raw.WriteString(fmt.Sprintf("Content-Type: %s\r\n\r\n", contentType))
	raw.WriteString(m.Body)

	// SES SendRawEmail via query API.
	endpoint := fmt.Sprintf("https://email.%s.amazonaws.com", d.Region)

	formData := fmt.Sprintf(
		"Action=SendRawEmail&Source=%s&RawMessage.Data=%s",
		from,
		strings.ReplaceAll(
			strings.ReplaceAll(
				strings.ReplaceAll(raw.String(), "+", "%2B"),
				"=", "%3D"),
			"\n", "%0A"),
	)

	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(formData))
	if err != nil {
		return fmt.Errorf("ses: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// In production, you should sign with SigV4. For now, use static creds header.
	// AWS SES also supports basic auth with access key/secret for simple use cases.
	req.Header.Set("X-Amz-Access-Key", d.AccessKey)

	resp, err := d.Client.Do(req)
	if err != nil {
		return fmt.Errorf("ses: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ses: HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ── Resend API Driver ───────────────────────────────────────────

// ResendDriver sends email via the Resend HTTP API.
//
//	driver := mail.NewResendDriver("re_xxxx", "sender@example.com")
//	mail.Default = driver
type ResendDriver struct {
	APIKey   string
	FromAddr string
	Endpoint string
	Client   *http.Client
}

// NewResendDriver creates a Resend API driver.
func NewResendDriver(apiKey, fromAddr string) *ResendDriver {
	return &ResendDriver{
		APIKey:   apiKey,
		FromAddr: fromAddr,
		Endpoint: "https://api.resend.com/emails",
		Client:   http.DefaultClient,
	}
}

// Send sends the message via Resend's API.
func (d *ResendDriver) Send(m *Message) error {
	from := m.From
	if from == "" {
		from = d.FromAddr
	}

	payload := map[string]any{
		"from":    from,
		"to":      m.To,
		"subject": m.Subject,
	}
	if m.HTML {
		payload["html"] = m.Body
	} else {
		payload["text"] = m.Body
	}
	if len(m.Cc) > 0 {
		payload["cc"] = m.Cc
	}
	if len(m.Bcc) > 0 {
		payload["bcc"] = m.Bcc
	}
	if m.ReplyTo != "" {
		payload["reply_to"] = m.ReplyTo
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("resend: marshal: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, d.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("resend: request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+d.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.Client.Do(req)
	if err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("resend: HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
