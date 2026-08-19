package commands_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/cli/auth"
	"github.com/CodeSyncr/nimbus/cli/commands"
)

func TestAICommand_CloudGeneration(t *testing.T) {
	tmpDir := t.TempDir()
	var stdout, stderr bytes.Buffer

	// Test with a mock Nimbus Cloud server returning 200 OK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/ai/generate" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"files": []map[string]string{
					{
						"path":    "app/models/product.go",
						"content": "package models\n\ntype Product struct {\n\tTitle string\n\tPrice float64\n}\n",
					},
				},
				"hints": []string{"Run nimbus migrate"},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Seed valid credentials
	t.Setenv("HOME", tmpDir)
	_ = auth.SaveCredentials(&auth.Credentials{
		AccessToken: "mock-token-xyz",
		Email:       "pro@nimbusgo.in",
		Plan:        "pro",
		HasSub:      true,
		ServerURL:   server.URL,
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	})

	cloudCtx := &cli.Context{
		AppRoot: tmpDir,
		Args:    []string{"create", "product", "model"},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}

	testAI := &commands.AICommand{}
	// We set server via flag or default env
	t.Setenv("NIMBUS_CLOUD_URL", server.URL)

	if err := testAI.Run(cloudCtx); err != nil {
		t.Fatalf("AICommand.Run failed: %v", err)
	}

	// Verify file was written
	genFile := filepath.Join(tmpDir, "app", "models", "product.go")
	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("expected generated file to exist at %s: %v", genFile, err)
	}
	if !bytes.Contains(data, []byte("type Product struct")) {
		t.Errorf("unexpected content: %s", string(data))
	}
}

func TestAICommand_SubscriptionRequired(t *testing.T) {
	tmpDir := t.TempDir()
	var stdout, stderr bytes.Buffer

	// Mock server returning 402 Payment Required
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "active subscription required",
			"upgrade": map[string]any{
				"required":    true,
				"pricing_url": "https://nimbusgo.in/pricing",
				"message":     "Please upgrade to Pro",
			},
		})
	}))
	defer server.Close()

	t.Setenv("HOME", tmpDir)
	t.Setenv("NIMBUS_CLOUD_URL", server.URL)
	_ = auth.SaveCredentials(&auth.Credentials{
		AccessToken: "mock-free-user",
		Email:       "free@nimbusgo.in",
		Plan:        "free",
		HasSub:      false,
		ServerURL:   server.URL,
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	})

	ctx := &cli.Context{
		AppRoot: tmpDir,
		Args:    []string{"create", "invoice", "system"},
		Stdout:  &stdout,
		Stderr:  &stderr,
	}

	testAI := &commands.AICommand{}
	if err := testAI.Run(ctx); err != nil {
		t.Fatalf("expected graceful handling of 402 payment required, got error: %v", err)
	}

	output := stdout.String()
	if !bytes.Contains([]byte(output), []byte("Subscription Required")) {
		t.Errorf("expected subscription required prompt in output, got:\n%s", output)
	}
}
