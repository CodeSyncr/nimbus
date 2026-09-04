package ai

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionUsageAccumulates(t *testing.T) {
	var u SessionUsage
	u.Add(&TokenUsage{InputTokens: 1000, OutputTokens: 250, CostUSD: 0.01})
	u.Add(&TokenUsage{InputTokens: 500, OutputTokens: 100, CostUSD: 0.005})

	if u.Requests != 2 {
		t.Errorf("Requests = %d, want 2", u.Requests)
	}
	if u.Total() != 1850 {
		t.Errorf("Total = %d, want 1850", u.Total())
	}
	if u.CostUSD != 0.015 {
		t.Errorf("CostUSD = %v, want 0.015", u.CostUSD)
	}
	if !strings.Contains(u.Summary(), "1.9k tokens") || !strings.Contains(u.Summary(), "$0.0150") {
		t.Errorf("Summary = %q", u.Summary())
	}
}

// A server that reports no usage must still count requests, and must not
// invent token or cost figures.
func TestSessionUsageWithoutServerReporting(t *testing.T) {
	var u SessionUsage
	u.Add(nil)
	u.Add(nil)

	if u.Requests != 2 {
		t.Errorf("Requests = %d, want 2", u.Requests)
	}
	if u.Reported() {
		t.Error("Reported() is true with no server usage")
	}
	if got := u.Summary(); got != "2 requests" {
		t.Errorf("Summary = %q, want %q", got, "2 requests")
	}
	if u.CostUSD != 0 {
		t.Errorf("cost was invented: %v", u.CostUSD)
	}
}

func TestFormatTokens(t *testing.T) {
	cases := map[int]string{0: "0", 999: "999", 1000: "1.0k", 42_100: "42.1k", 2_500_000: "2.5M"}
	for in, want := range cases {
		if got := FormatTokens(in); got != want {
			t.Errorf("FormatTokens(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestParseUsageFromSSEStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"type\":\"text\",\"text\":\"hello\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"done\",\"stop_reason\":\"end_turn\",\"usage\":{\"input_tokens\":1200,\"output_tokens\":300,\"cost_usd\":0.0075}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	msg, err := parseMessageResponse(resp, nil)
	if err != nil {
		t.Fatalf("parseMessageResponse: %v", err)
	}
	if msg.Usage == nil {
		t.Fatal("usage was dropped from the stream")
	}
	if msg.Usage.InputTokens != 1200 || msg.Usage.OutputTokens != 300 {
		t.Errorf("usage = %+v", msg.Usage)
	}
	if msg.Usage.CostUSD != 0.0075 {
		t.Errorf("cost = %v, want 0.0075", msg.Usage.CostUSD)
	}
	if msg.StopReason != "end_turn" {
		t.Errorf("stop reason lost: %q", msg.StopReason)
	}
}

func TestParseUsageFromJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true,"content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	msg, err := parseMessageResponse(resp, nil)
	if err != nil {
		t.Fatalf("parseMessageResponse: %v", err)
	}
	if msg.Usage == nil || msg.Usage.Total() != 15 {
		t.Fatalf("usage = %+v, want 15 total", msg.Usage)
	}
}

// Responses from a server that does not report usage must parse cleanly with
// no usage attached, rather than zeros that look like a free request.
func TestMissingUsageStaysNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true,"content":[{"type":"text","text":"hi"}]}`)
	}))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	msg, err := parseMessageResponse(resp, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Usage != nil {
		t.Errorf("usage should be nil, got %+v", msg.Usage)
	}
}
