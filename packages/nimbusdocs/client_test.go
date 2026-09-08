package nimbusdocs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewValidates(t *testing.T) {
	if _, err := New("chat", "k"); err == nil {
		t.Error("unknown product accepted")
	}
	if _, err := New(Sigma, ""); err == nil {
		t.Error("empty key accepted")
	}
}

func TestUploadSendsKeyPathAndOptions(t *testing.T) {
	var got *http.Request
	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"id":42,"product":"sigma","url":"https://x/v/sigma/42?sig=y","created_at":"2026-09-09T00:00:00Z"}`))
	}))
	defer srv.Close()

	c, _ := New(Sigma, "sgm_live_abc", WithBaseURL(srv.URL+"/"))
	doc, err := c.Upload(context.Background(), Upload{
		Content: []byte("a,b\n1,2"), Filename: "q3.csv",
		TTL: time.Hour, Mode: "view", RetainDays: 30, IdempotencyKey: "order-9",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.URL.Path != "/api/v1/sigma/documents" {
		t.Errorf("path %s", got.URL.Path)
	}
	if got.Header.Get("Authorization") != "Bearer sgm_live_abc" {
		t.Error("bearer key missing")
	}
	if got.Header.Get("Idempotency-Key") != "order-9" {
		t.Error("idempotency header missing")
	}
	q := got.URL.Query()
	if q.Get("ttl") != "3600" || q.Get("mode") != "view" || q.Get("retain_days") != "30" {
		t.Errorf("query %v", q)
	}
	if body["title"] != "q3.csv" {
		t.Error("title should default to the filename")
	}
	if doc.ID != 42 || doc.URL == "" {
		t.Errorf("document not decoded: %+v", doc)
	}
}

func TestQuotaRefusalCarriesTheQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(402)
		_, _ = w.Write([]byte(`{"error":"allowance used up","quota":{"limit":50,"used":51,"remaining":0,"overage":1,"allowed":false}}`))
	}))
	defer srv.Close()
	c, _ := New(Carbon, "cbn_live_x", WithBaseURL(srv.URL))
	_, err := c.Upload(context.Background(), Upload{Content: []byte("x"), Filename: "a.html"})
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want *Error, got %T", err)
	}
	if e.Code != CodeQuotaExceeded || e.Status != 402 || e.Quota == nil || e.Quota.Used != 51 {
		t.Errorf("got %+v", e)
	}
}

func TestStatusCodesMapToStableCodes(t *testing.T) {
	for status, code := range map[int]string{401: CodeUnauthorized, 404: CodeNotFound, 413: CodeTooLarge, 429: CodeRateLimited, 500: CodeServer} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"nope"}`))
		}))
		c, _ := New(Sigma, "k", WithBaseURL(srv.URL))
		_, err := c.Get(context.Background(), 1)
		srv.Close()
		var e *Error
		if !errors.As(err, &e) || e.Code != code {
			t.Errorf("status %d: got %v, want %s", status, err, code)
		}
	}
}

func TestLinkAndListPaths(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.URL.Path == "/api/v1/carbon/documents/7/link":
			_, _ = w.Write([]byte(`{"url":"https://x/v/carbon/7?sig=z","mode":"edit","expires_at":"2026-09-09T01:00:00Z"}`))
		default:
			_, _ = w.Write([]byte(`{"documents":[{"id":1},{"id":2}]}`))
		}
	}))
	defer srv.Close()
	c, _ := New(Carbon, "k", WithBaseURL(srv.URL))
	l, err := c.Link(context.Background(), 7, LinkOptions{TTL: time.Minute, Mode: "edit"})
	if err != nil || l.Mode != "edit" {
		t.Fatalf("link: %v %+v", err, l)
	}
	docs, err := c.List(context.Background(), 0)
	if err != nil || len(docs) != 2 {
		t.Fatalf("list: %v %d", err, len(docs))
	}
	if paths[0] != "POST /api/v1/carbon/documents/7/link?mode=edit&ttl=60" || paths[1] != "GET /api/v1/carbon/documents?limit=50" {
		t.Errorf("paths %v", paths)
	}
}

func TestUnreachableIsAnErrorNotAPanic(t *testing.T) {
	c, _ := New(Sigma, "k", WithBaseURL("http://127.0.0.1:1"), WithTimeout(500*time.Millisecond))
	if _, err := c.Usage(context.Background()); err == nil {
		t.Error("expected an error")
	}
}
