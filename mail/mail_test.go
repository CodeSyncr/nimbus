package mail

import (
	"testing"
)

func TestNewMessage(t *testing.T) {
	m := NewMessage("Welcome")
	if m.Subject != "Welcome" {
		t.Errorf("expected 'Welcome', got %q", m.Subject)
	}
}

func TestMessage_FluentAPI(t *testing.T) {
	m := NewMessage("Test").
		SetFrom("noreply@example.com").
		SetTo("alice@example.com", "bob@example.com").
		AddCc("cc@example.com").
		AddBcc("bcc@example.com").
		SetReplyTo("reply@example.com").
		SetBody("<h1>Hello</h1>", true)

	if m.From != "noreply@example.com" {
		t.Errorf("From: got %q", m.From)
	}
	if len(m.To) != 2 {
		t.Errorf("To: expected 2, got %d", len(m.To))
	}
	if len(m.Cc) != 1 || m.Cc[0] != "cc@example.com" {
		t.Error("Cc mismatch")
	}
	if len(m.Bcc) != 1 || m.Bcc[0] != "bcc@example.com" {
		t.Error("Bcc mismatch")
	}
	if m.ReplyTo != "reply@example.com" {
		t.Error("ReplyTo mismatch")
	}
	if !m.HTML {
		t.Error("expected HTML=true")
	}
}

func TestMessage_AllRecipients(t *testing.T) {
	m := NewMessage("Test").
		SetTo("to@example.com").
		AddCc("cc@example.com").
		AddBcc("bcc@example.com")

	all := m.AllRecipients()
	if len(all) != 3 {
		t.Errorf("expected 3 recipients, got %d", len(all))
	}
}

func TestMessage_Attach(t *testing.T) {
	m := NewMessage("Invoice").
		Attach("report.pdf", []byte("pdf-data"), "application/pdf")

	if len(m.Attachments) != 1 {
		t.Fatal("expected 1 attachment")
	}
	a := m.Attachments[0]
	if a.Filename != "report.pdf" {
		t.Errorf("filename: got %q", a.Filename)
	}
	if a.MIMEType != "application/pdf" {
		t.Errorf("MIME: got %q", a.MIMEType)
	}
}

func TestMessage_AttachAutoMIME(t *testing.T) {
	m := NewMessage("Test").
		Attach("image.png", []byte("data"))

	if m.Attachments[0].MIMEType != "" {
		t.Error("expected empty MIME for auto-detection")
	}
}

// mockDriver records sent messages for testing
type mockDriver struct {
	sent []*Message
}

func (d *mockDriver) Send(m *Message) error {
	d.sent = append(d.sent, m)
	return nil
}

func TestSend_WithMockDriver(t *testing.T) {
	mock := &mockDriver{}
	Default = mock

	m := NewMessage("Hello").
		SetFrom("test@example.com").
		SetTo("user@example.com").
		SetBody("Hi there", false)

	err := Send(m)
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(mock.sent) != 1 {
		t.Fatal("expected 1 sent message")
	}
	if mock.sent[0].Subject != "Hello" {
		t.Error("wrong subject")
	}
}

func TestSend_NilDriver(t *testing.T) {
	Default = nil
	err := Send(NewMessage("Test"))
	if err == nil {
		t.Error("expected error with nil driver")
	}
}
