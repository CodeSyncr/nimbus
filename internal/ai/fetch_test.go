package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const samplePage = `<!doctype html>
<html><head><title>Stripe — Payments</title>
<style>body{color:red}</style>
<script>console.log("tracking pixel")</script>
</head>
<body>
<h1>Payments infrastructure</h1>
<p>Millions of companies use Stripe.</p>
<p>Build a &quot;better&quot; checkout &amp; get paid.</p>
<script>window.analytics.load()</script>
</body></html>`

func TestFetchReturnsReadableText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, samplePage)
	}))
	defer srv.Close()

	exec := &ToolExecutor{AppRoot: t.TempDir()}
	out, err := exec.FetchURL(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("FetchURL: %v", err)
	}

	for _, want := range []string{"Payments infrastructure", "Millions of companies", "Stripe — Payments"} {
		if !strings.Contains(out, want) {
			t.Errorf("readable text is missing %q:\n%s", want, out)
		}
	}
	// Script and style content is noise that would crowd out the page.
	for _, unwanted := range []string{"tracking pixel", "analytics.load", "color:red"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("%q should have been stripped:\n%s", unwanted, out)
		}
	}
	// Entities are decoded so the text reads normally.
	if !strings.Contains(out, `"better" checkout & get paid`) {
		t.Errorf("entities were not decoded:\n%s", out)
	}
}

// Studying how a page is built needs the markup, not the prose.
func TestFetchCanReturnRawHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, samplePage)
	}))
	defer srv.Close()

	exec := &ToolExecutor{AppRoot: t.TempDir()}
	out, err := exec.FetchURL(context.Background(), srv.URL, "html")
	if err != nil {
		t.Fatalf("FetchURL: %v", err)
	}
	if !strings.Contains(out, "<h1>") || !strings.Contains(out, "<style>") {
		t.Errorf("html format should preserve markup:\n%s", out)
	}
}

func TestFetchReportsFailuresClearly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	exec := &ToolExecutor{AppRoot: t.TempDir()}

	_, err := exec.FetchURL(context.Background(), srv.URL, "")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("a 404 should be reported with its status: %v", err)
	}
	if _, err := exec.FetchURL(context.Background(), "", ""); err == nil {
		t.Error("an empty url should be rejected")
	}
	if _, err := exec.FetchURL(context.Background(), "file:///etc/passwd", ""); err == nil {
		t.Error("non-http schemes should be refused")
	}
	if _, err := exec.FetchURL(context.Background(), "ftp://example.com", ""); err == nil {
		t.Error("ftp should be refused")
	}
}

// A bare host is a normal way to name a site; it should not be a syntax error.
func TestFetchAcceptsAHostWithoutAScheme(t *testing.T) {
	exec := &ToolExecutor{AppRoot: t.TempDir()}
	// Unroutable by design: the point is that it parses and tries https,
	// rather than failing as a malformed URL.
	_, err := exec.FetchURL(context.Background(), "example.invalid", "")
	if err == nil {
		t.Skip("host unexpectedly resolved")
	}
	if strings.Contains(err.Error(), "not a valid URL") {
		t.Errorf("a bare host should be treated as https, got: %v", err)
	}
}

// Binary responses are described rather than dumped into the conversation.
func TestFetchDoesNotDumpBinary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte{0x89, 'P', 'N', 'G', 0x00, 0x01, 0x02})
	}))
	defer srv.Close()

	exec := &ToolExecutor{AppRoot: t.TempDir()}
	out, err := exec.FetchURL(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("FetchURL: %v", err)
	}
	if !strings.Contains(out, "binary content is not shown") {
		t.Errorf("binary should be described, not dumped: %q", out)
	}
}

// The tool is offered to the model and reachable through the dispatcher.
func TestFetchToolIsAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><p>hello</p></body></html>")
	}))
	defer srv.Close()

	exec := &ToolExecutor{AppRoot: t.TempDir()}

	var offered bool
	for _, def := range exec.GetToolDefinitions() {
		if def.Name == "fetch_url" {
			offered = true
		}
	}
	if !offered {
		t.Fatal("fetch_url is not offered to the model")
	}

	out, _, err := exec.ExecuteTool(context.Background(), "fetch_url", map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("dispatch did not return the page: %q", out)
	}
}
