package auth_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/cli/auth"
)

func TestCredentials_Expiry(t *testing.T) {
	var nilCreds *auth.Credentials
	if !nilCreds.IsExpired() {
		t.Errorf("expected nil creds to be expired")
	}

	emptyCreds := &auth.Credentials{}
	if !emptyCreds.IsExpired() {
		t.Errorf("expected empty token creds to be expired")
	}

	validCreds := &auth.Credentials{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	if validCreds.IsExpired() {
		t.Errorf("expected valid token to not be expired")
	}

	expiredCreds := &auth.Credentials{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
	}
	if !expiredCreds.IsExpired() {
		t.Errorf("expected past token to be expired")
	}
}

func TestCredentials_SaveLoadClear(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv(auth.ConfigDirEnv, filepath.Join(tmpHome, ".nimbus"))

	// Verify initially nil
	creds, err := auth.LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials failed: %v", err)
	}
	if creds != nil {
		t.Fatalf("expected nil credentials in empty dir")
	}

	toSave := &auth.Credentials{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		Email:        "dev@example.com",
		Name:         "Dev User",
		Plan:         "pro",
		HasSub:       true,
		ServerURL:    "https://nimbusgo.in",
		ExpiresAt:    time.Now().Add(24 * time.Hour),
	}

	if err := auth.SaveCredentials(toSave); err != nil {
		t.Fatalf("SaveCredentials failed: %v", err)
	}

	loaded, err := auth.LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials after save failed: %v", err)
	}
	if loaded == nil {
		t.Fatalf("expected non-nil loaded credentials")
	}
	if loaded.Email != "dev@example.com" || loaded.Plan != "pro" || !loaded.HasSub {
		t.Errorf("loaded credentials mismatch: %+v", loaded)
	}

	// Verify file permissions (0600)
	authPath, _ := auth.AuthFilePath()
	info, err := os.Stat(authPath)
	if err != nil {
		t.Fatalf("failed to stat auth file: %v", err)
	}
	// NTFS does not carry Unix permission bits; Go reports 0666 there.
	if perm := info.Mode().Perm(); perm != 0600 && runtime.GOOS != "windows" {
		t.Errorf("expected 0600 permissions, got %o", perm)
	}

	// Verify clear
	if err := auth.ClearCredentials(); err != nil {
		t.Fatalf("ClearCredentials failed: %v", err)
	}
	cleared, err := auth.LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials after clear failed: %v", err)
	}
	if cleared != nil {
		t.Errorf("expected nil credentials after clear")
	}
}

func TestGetServerURL(t *testing.T) {
	t.Setenv("NIMBUS_CLOUD_URL", "")
	if u := auth.GetServerURL(); u != auth.DefaultServerURL {
		t.Errorf("expected default server url %q, got %q", auth.DefaultServerURL, u)
	}

	t.Setenv("NIMBUS_CLOUD_URL", "https://custom.nimbusgo.in")
	if u := auth.GetServerURL(); u != "https://custom.nimbusgo.in" {
		t.Errorf("expected custom server url, got %q", u)
	}
}
