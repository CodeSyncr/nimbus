package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// DefaultServerURL is the default Nimbus Cloud API & OAuth server.
const DefaultServerURL = "https://nimbusgo.space"

// Credentials represents authenticated developer credentials for Nimbus Cloud.
type Credentials struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Plan         string    `json:"plan"` // e.g. "free", "pro", "team", "enterprise"
	HasSub       bool      `json:"has_subscription"`
	ExpiresAt    time.Time `json:"expires_at"`
	ServerURL    string    `json:"server_url"`
}

// IsExpired reports whether the access token has expired.
func (c *Credentials) IsExpired() bool {
	if c == nil || c.AccessToken == "" {
		return true
	}
	if c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(c.ExpiresAt)
}

// ConfigDirEnv overrides the config directory (default ~/.nimbus). Tests use
// it so they never touch the real login; on Windows os.UserHomeDir reads
// USERPROFILE, so overriding HOME alone is not enough.
const ConfigDirEnv = "NIMBUS_CONFIG_DIR"

// ConfigDir returns the path to the ~/.nimbus directory (or $NIMBUS_CONFIG_DIR).
func ConfigDir() (string, error) {
	dir := os.Getenv(ConfigDirEnv)
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".nimbus")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// AuthFilePath returns the absolute path to ~/.nimbus/auth.json.
func AuthFilePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "auth.json"), nil
}

// LoadCredentials loads stored credentials from ~/.nimbus/auth.json.
func LoadCredentials() (*Credentials, error) {
	path, err := AuthFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	if creds.ServerURL == "" {
		creds.ServerURL = DefaultServerURL
	}
	return &creds, nil
}

// SaveCredentials writes credentials to ~/.nimbus/auth.json with 0600 permissions.
func SaveCredentials(creds *Credentials) error {
	path, err := AuthFilePath()
	if err != nil {
		return err
	}
	if creds.ServerURL == "" {
		creds.ServerURL = DefaultServerURL
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// ClearCredentials removes ~/.nimbus/auth.json.
func ClearCredentials() error {
	path, err := AuthFilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// GetServerURL returns the configured Nimbus Cloud URL (checking NIMBUS_CLOUD_URL env var).
func GetServerURL() string {
	if u := os.Getenv("NIMBUS_CLOUD_URL"); u != "" {
		return u
	}
	return DefaultServerURL
}
