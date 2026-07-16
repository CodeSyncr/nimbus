package serverless

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

func invoke(t *testing.T, h Handler, event string) Response {
	t.Helper()
	resp, err := h(context.Background(), json.RawMessage(event))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return resp
}

// echoHandler reflects request details back so tests can assert the adapter
// reconstructed the *http.Request faithfully.
func echoHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Method", r.Method)
		w.Header().Set("X-Query", r.URL.RawQuery)
		w.Header().Set("X-Custom", r.Header.Get("X-Custom"))
		w.Header().Set("X-Cookie", r.Header.Get("Cookie"))
		w.WriteHeader(200)
		body, _ := io.ReadAll(r.Body)
		w.Write([]byte("hello:" + string(body)))
	})
	mux.HandleFunc("/set-cookies", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "a", Value: "1"})
		http.SetCookie(w, &http.Cookie{Name: "b", Value: "2"})
		w.WriteHeader(204)
	})
	mux.HandleFunc("/binary", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(200)
		w.Write([]byte{0x00, 0xff, 0x01, 0xfe}) // not valid UTF-8
	})
	return mux
}

// ── v2.0 (HTTP API / Function URL) ─────────────────────────────────

func TestLambda_V2_MethodPathQueryHeaders(t *testing.T) {
	h := Lambda(echoHandler())
	event := `{
		"version": "2.0",
		"rawPath": "/hello",
		"rawQueryString": "a=1&b=2",
		"cookies": ["session=xyz", "theme=dark"],
		"headers": {"x-custom": "hi", "host": "api.example.com"},
		"requestContext": {"http": {"method": "POST", "sourceIp": "1.2.3.4"}},
		"body": "world",
		"isBase64Encoded": false
	}`
	resp := invoke(t, h, event)

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.Body != "hello:world" {
		t.Errorf("body = %q", resp.Body)
	}
	if resp.Headers["X-Method"] != "POST" {
		t.Errorf("method = %q", resp.Headers["X-Method"])
	}
	if resp.Headers["X-Query"] != "a=1&b=2" {
		t.Errorf("query = %q", resp.Headers["X-Query"])
	}
	if resp.Headers["X-Custom"] != "hi" {
		t.Errorf("custom header = %q", resp.Headers["X-Custom"])
	}
	if resp.Headers["X-Cookie"] != "session=xyz; theme=dark" {
		t.Errorf("cookie header = %q", resp.Headers["X-Cookie"])
	}
}

func TestLambda_V2_Base64RequestBody(t *testing.T) {
	h := Lambda(echoHandler())
	encoded := base64.StdEncoding.EncodeToString([]byte("binary-in"))
	event := `{
		"version": "2.0",
		"rawPath": "/hello",
		"requestContext": {"http": {"method": "POST"}},
		"body": "` + encoded + `",
		"isBase64Encoded": true
	}`
	resp := invoke(t, h, event)
	if resp.Body != "hello:binary-in" {
		t.Errorf("decoded body = %q", resp.Body)
	}
}

func TestLambda_V2_SetCookiesUseCookiesField(t *testing.T) {
	h := Lambda(echoHandler())
	event := `{"version":"2.0","rawPath":"/set-cookies","requestContext":{"http":{"method":"GET"}}}`
	resp := invoke(t, h, event)

	if resp.StatusCode != 204 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if len(resp.Cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %v", resp.Cookies)
	}
	if !strings.HasPrefix(resp.Cookies[0], "a=1") || !strings.HasPrefix(resp.Cookies[1], "b=2") {
		t.Errorf("cookies = %v", resp.Cookies)
	}
	// Set-Cookie must NOT be folded into the headers map.
	if _, ok := resp.Headers["Set-Cookie"]; ok {
		t.Error("Set-Cookie leaked into headers map")
	}
}

func TestLambda_V2_BinaryResponseIsBase64(t *testing.T) {
	h := Lambda(echoHandler())
	event := `{"version":"2.0","rawPath":"/binary","requestContext":{"http":{"method":"GET"}}}`
	resp := invoke(t, h, event)

	if !resp.IsBase64Encoded {
		t.Fatal("binary response should be base64-encoded")
	}
	raw, err := base64.StdEncoding.DecodeString(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string([]byte{0x00, 0xff, 0x01, 0xfe}) {
		t.Errorf("decoded bytes = %x", raw)
	}
}

// ── v1.0 (REST API / ALB) ──────────────────────────────────────────

func TestLambda_V1_MethodPathQuery(t *testing.T) {
	h := Lambda(echoHandler())
	event := `{
		"httpMethod": "POST",
		"path": "/hello",
		"multiValueQueryStringParameters": {"tag": ["go", "web"]},
		"queryStringParameters": {"tag": "go"},
		"headers": {"X-Custom": "v1"},
		"body": "b",
		"isBase64Encoded": false
	}`
	resp := invoke(t, h, event)

	if resp.StatusCode != 200 || resp.Body != "hello:b" {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Headers["X-Method"] != "POST" {
		// v1 responses use multiValueHeaders
		if got := resp.MultiValueHeaders["X-Method"]; len(got) != 1 || got[0] != "POST" {
			t.Errorf("method header = %v / %q", got, resp.Headers["X-Method"])
		}
	}
	// array query serialized as repeated params
	q := resp.MultiValueHeaders["X-Query"]
	if len(q) != 1 || (q[0] != "tag=go&tag=web" && q[0] != "tag=web&tag=go") {
		t.Errorf("v1 query = %v", q)
	}
}

func TestLambda_V1_UsesMultiValueHeaders(t *testing.T) {
	h := Lambda(echoHandler())
	event := `{"httpMethod":"GET","path":"/set-cookies"}`
	resp := invoke(t, h, event)

	if resp.Headers != nil {
		t.Error("v1 should not populate the single-value headers map")
	}
	cookies := resp.MultiValueHeaders["Set-Cookie"]
	if len(cookies) != 2 {
		t.Fatalf("v1 Set-Cookie multi-value = %v", cookies)
	}
}

// ── Through the real Nimbus router ─────────────────────────────────

func TestLambda_ThroughNimbusRouter(t *testing.T) {
	r := router.New()
	r.Get("/users/:id", func(c *nhttp.Context) error {
		return c.JSON(200, map[string]string{"id": c.Param("id")})
	})
	r.Post("/users", func(c *nhttp.Context) error {
		return c.JSON(201, map[string]string{"created": "true"})
	})

	h := Lambda(r)

	// GET with a path param.
	get := invoke(t, h, `{"version":"2.0","rawPath":"/users/42","requestContext":{"http":{"method":"GET"}}}`)
	if get.StatusCode != 200 || !strings.Contains(get.Body, `"id":"42"`) {
		t.Fatalf("GET resp = %+v", get)
	}

	// POST returns 201.
	post := invoke(t, h, `{"version":"2.0","rawPath":"/users","requestContext":{"http":{"method":"POST"}}}`)
	if post.StatusCode != 201 {
		t.Fatalf("POST status = %d", post.StatusCode)
	}

	// Unknown route → 404 from the router.
	nf := invoke(t, h, `{"version":"2.0","rawPath":"/nope","requestContext":{"http":{"method":"GET"}}}`)
	if nf.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", nf.StatusCode)
	}
}

func TestLambda_InvalidEvent(t *testing.T) {
	h := Lambda(echoHandler())
	resp, err := h(context.Background(), json.RawMessage(`not json`))
	if err != nil {
		t.Fatalf("adapter should not surface an error to the runtime: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
