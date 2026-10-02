package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const tinyPNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestLoadAttachmentSources(t *testing.T) {
	ctx := context.Background()
	a, err := LoadAttachment(ctx, tinyPNG)
	if err != nil || a.MediaType != "image/png" || !a.IsImage() || len(a.Data) == 0 {
		t.Fatalf("data URI = %+v, %v", a, err)
	}
	pdf, err := LoadAttachment(ctx, "testdata/invoice.pdf")
	if err != nil || !pdf.IsPDF() || pdf.Name != "invoice.pdf" {
		t.Fatalf("path = %+v, %v", pdf.MediaType, err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		http.ServeFile(w, r, "testdata/invoice.pdf")
	}))
	defer srv.Close()
	remote, err := LoadAttachment(ctx, srv.URL+"/files/invoice.pdf?sig=x")
	if err != nil || !remote.IsPDF() || remote.Name != "invoice.pdf" {
		t.Fatalf("url = %+v, %v", remote.MediaType, err)
	}
	if _, err := LoadAttachment(ctx, "testdata/missing.pdf"); err == nil {
		t.Fatal("missing file should fail")
	}
}

func TestDocumentTextExtractsPDF(t *testing.T) {
	a, _ := LoadAttachment(context.Background(), "testdata/invoice.pdf")
	text, err := DocumentText(a)
	if err != nil || !strings.Contains(text, "Invoice total 42") {
		t.Fatalf("text = %q, %v", text, err)
	}
}

func TestAnthropicSendsPDFsAsDocuments(t *testing.T) {
	p := &anthropicProvider{model: "claude-x", maxTokens: 100}
	ar := p.buildRequest(&GenerateRequest{Messages: []Message{{
		Role: RoleUser, Content: "What is the total?",
		Files: []string{"testdata/invoice.pdf", "testdata/notes.txt", tinyPNG},
	}}}, false)
	blocks := ar.Messages[0].Content
	types := []string{}
	for _, b := range blocks {
		types = append(types, b.Type+":"+func() string {
			if b.Source != nil {
				return b.Source.Type + "/" + b.Source.MediaType
			}
			return ""
		}())
	}
	got := strings.Join(types, " ")
	for _, want := range []string{"text:", "document:base64/application/pdf", "document:text/text/plain", "image:base64/image/png"} {
		if !strings.Contains(got, want) {
			t.Fatalf("blocks = %s, missing %s", got, want)
		}
	}
}

func TestGeminiSendsPDFsInline(t *testing.T) {
	p := &geminiProvider{model: "gemini-2.0-flash"}
	gr := p.buildRequest(&GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "total?", Files: []string{"testdata/invoice.pdf"}}}})
	raw, _ := json.Marshal(gr)
	if !strings.Contains(string(raw), `"mimeType":"application/pdf"`) {
		t.Fatalf("request = %.300s", raw)
	}
}

func TestOpenAIGetsDocumentText(t *testing.T) {
	msgs := (&openAIProvider{}).toOpenAIMessages(&GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "total?", Files: []string{"testdata/invoice.pdf", tinyPNG}}}})
	m := msgs[len(msgs)-1]
	if len(m.MultiContent) != 2 || !strings.Contains(m.MultiContent[0].Text, "Invoice total 42") || m.MultiContent[1].ImageURL == nil {
		t.Fatalf("message = %+v", m)
	}
}

func TestWithFilesReachesTheUserTurn(t *testing.T) {
	fake := NewFake(FakeToolCall("echo", `{"q":"x"}`), FakeText("42 EUR"))
	_, err := NewAgent("x").WithClient(fake.Client()).WithToolObjects(echoTool(t)).
		Prompt(context.Background(), "total?", WithFiles("testdata/invoice.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range fake.Requests() {
		if f := r.Messages[0].Files; len(f) != 1 {
			t.Fatalf("step %d: files = %v", i, f)
		}
	}
	fake2 := NewFake(FakeText("ok"))
	if _, err := fake2.Client().Generate(context.Background(), "total?", WithFiles("testdata/invoice.pdf")); err != nil {
		t.Fatal(err)
	}
	if f := fake2.Requests()[0].Messages[0].Files; len(f) != 1 {
		t.Fatalf("client Generate: files = %v", f)
	}
}
