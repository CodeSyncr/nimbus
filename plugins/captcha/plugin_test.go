package captcha_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/plugins/captcha"
	"github.com/CodeSyncr/nimbus/plugins/captcha/server"
)

func TestNewPlugin(t *testing.T) {
	p := captcha.New()
	if p.Name() != "captcha" {
		t.Fatalf("expected plugin name 'captcha', got %s", p.Name())
	}
	if p.Version() != "1.0.0" {
		t.Fatalf("expected plugin version '1.0.0', got %s", p.Version())
	}
}

func TestPluginRegisterAndMockSolve(t *testing.T) {
	app := nimbus.New()

	p := captcha.New(&captcha.Config{
		APIKey:   "test-key",
		MockMode: true,
	})

	app.Use(p)
	if err := p.Register(app); err != nil {
		t.Fatalf("p.Register failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test Solve
	sol, err := captcha.Solve(ctx, captcha.TaskPayload{
		Type:       captcha.TaskTypeTurnstileProxyless,
		WebsiteURL: "https://example.com",
		WebsiteKey: "0x4AAAAAAA",
	})

	if err != nil {
		t.Fatalf("captcha.Solve failed in mock mode: %v", err)
	}

	if sol.Token == "" {
		t.Fatal("expected non-empty solved token")
	}

	// Test GetBalance
	bal, err := captcha.GetBalance(ctx)
	if err != nil {
		t.Fatalf("captcha.GetBalance failed: %v", err)
	}
	if bal <= 0 {
		t.Fatalf("expected positive balance, got %f", bal)
	}
}

func TestRealServerIntegration(t *testing.T) {
	// Start actual Backend Captcha Server on random local port
	srv := server.NewServer(&server.ServerConfig{
		Addr: "127.0.0.1:0",
		APIKeys: map[string]float64{
			"live_test_api_key": 250.0,
		},
	})

	err := srv.Start()
	if err != nil {
		t.Fatalf("Failed to start backend server: %v", err)
	}
	defer func() {
		_ = srv.Shutdown(context.Background())
	}()

	serverURL := "http://" + srv.Addr()

	// Register Plugin pointed to our local backend server (MockMode = false!)
	app := nimbus.New()
	p := captcha.New(&captcha.Config{
		APIKey:          "live_test_api_key",
		Endpoint:        serverURL,
		MockMode:        false,
		PollingInterval: 50 * time.Millisecond,
	})

	app.Use(p)
	if err := p.Register(app); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Solve Turnstile Challenge on real backend server
	sol, err := captcha.Solve(ctx, captcha.TaskPayload{
		Type:       captcha.TaskTypeTurnstileProxyless,
		WebsiteURL: "https://example.com/login",
		WebsiteKey: "0x4AAAAAAAJn_...",
	})

	if err != nil {
		t.Fatalf("Real server Turnstile solve failed: %v", err)
	}

	if !strings.Contains(sol.Token, "NMB_TS_") {
		t.Fatalf("unexpected token output format: %s", sol.Token)
	}

	// 2. Solve ImageToText OCR Challenge
	ocrSol, err := captcha.Solve(ctx, captcha.TaskPayload{
		Type: captcha.TaskTypeImageToText,
		Body: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
	})
	if err != nil {
		t.Fatalf("Real server OCR solve failed: %v", err)
	}
	if !strings.HasPrefix(ocrSol.Text, "NMB_") {
		t.Fatalf("unexpected OCR text output: %s", ocrSol.Text)
	}

	// 3. Query Balance from real backend server
	bal, err := captcha.GetBalance(ctx)
	if err != nil {
		t.Fatalf("GetBalance failed: %v", err)
	}
	if bal != 250.0 {
		t.Fatalf("expected balance 250.0, got %f", bal)
	}
}

func TestVerifierMock(t *testing.T) {
	app := nimbus.New()
	p := captcha.New(&captcha.Config{
		MockMode: true,
	})
	app.Use(p)
	_ = p.Register(app)

	ctx := context.Background()
	res, err := captcha.Verify(ctx, "turnstile", "mock-captcha-token-approved", "127.0.0.1")
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if !res.Success {
		t.Fatal("expected verification success for mock token")
	}
}

func TestMiddleware(t *testing.T) {
	app := nimbus.New()
	p := captcha.New(&captcha.Config{
		MockMode: true,
	})
	app.Use(p)
	_ = p.Register(app)

	// Setup protected route handler
	handler := captcha.Protect()(func(c *nhttp.Context) error {
		c.String(http.StatusOK, "OK")
		return nil
	})

	// 1. Request WITHOUT token -> 422 Unprocessable Entity
	req1 := httptest.NewRequest(http.MethodPost, "/submit", nil)
	rec1 := httptest.NewRecorder()
	c1 := nhttp.New(rec1, req1, nil)

	_ = handler(c1)
	if rec1.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing token, got %d", rec1.Code)
	}

	// 2. Request WITH mock token in form -> 200 OK
	formData := url.Values{}
	formData.Set("cf-turnstile-response", "mock-captcha-token-approved")

	req2 := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(formData.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec2 := httptest.NewRecorder()
	c2 := nhttp.New(rec2, req2, nil)

	_ = handler(c2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid mock token, got %d", rec2.Code)
	}
}
