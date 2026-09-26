package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func fakeChat(t *testing.T, hits *atomic.Int32, reply string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		var body struct{ Model string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "x", "object": "chat.completion", "model": body.Model,
			"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": reply + ":" + body.Model + ":" + r.Header.Get("Authorization")}}},
		})
	}))
}

func TestFallbackModelRoutesToItsOwnAccount(t *testing.T) {
	var mainHits, fbHits atomic.Int32
	mainSrv, fbSrv := fakeChat(t, &mainHits, "main"), fakeChat(t, &fbHits, "fb")
	defer mainSrv.Close()
	defer fbSrv.Close()

	c, err := NewClient(&Config{Provider: "openai", Model: "main-model", MaxTokens: 64, Timeout: 10,
		OpenAIKey: "main-key", OpenAIBaseURL: mainSrv.URL,
		FallbackModel: "fb-model", FallbackAPIKey: "fb-key", FallbackBaseURL: fbSrv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	res, err := c.Generate(ctx, "hi")
	if err != nil || res.Text != "main:main-model:Bearer main-key" {
		t.Fatalf("main: %v %+v", err, res)
	}
	res, err = c.Generate(ctx, "hi", WithModel("fb-model"))
	if err != nil || res.Text != "fb:fb-model:Bearer fb-key" {
		t.Fatalf("fallback: %v %+v", err, res)
	}
	// Any other model stays on the main account.
	res, err = c.Generate(ctx, "hi", WithModel("other"))
	if err != nil || res.Text != "main:other:Bearer main-key" {
		t.Fatalf("other: %v %+v", err, res)
	}
	if mainHits.Load() != 2 || fbHits.Load() != 1 {
		t.Fatalf("hits main=%d fb=%d", mainHits.Load(), fbHits.Load())
	}
}

func TestFallbackWithoutOwnAccountStaysOnMain(t *testing.T) {
	var hits atomic.Int32
	srv := fakeChat(t, &hits, "main")
	defer srv.Close()
	c, err := NewClient(&Config{Provider: "openai", Model: "m", MaxTokens: 64, Timeout: 10, OpenAIKey: "k", OpenAIBaseURL: srv.URL, FallbackModel: "fb"})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := c.Generate(context.Background(), "hi", WithModel("fb")); err != nil || res.Text != "main:fb:Bearer k" {
		t.Fatalf("%v %+v", err, res)
	}
}
