package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/charmbracelet/lipgloss"
)

// Login performs the browser-based OAuth authorization flow with Nimbus Cloud.
func Login(ctx *cli.Context, serverURL string) (*Credentials, error) {
	if serverURL == "" {
		serverURL = GetServerURL()
	}

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, err
	}
	state := hex.EncodeToString(stateBytes)

	// Start local ephemeral loopback listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start local auth listener: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	authURL := fmt.Sprintf("%s/auth/cli?port=%d&state=%s", strings.TrimRight(serverURL, "/"), port, state)

	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#818cf8")).Bold(true)
	linkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#38bdf8")).Underline(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b"))
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Bold(true)

	fmt.Fprintln(ctx.Stdout, headerStyle.Render("🔐 Authenticating with Nimbus Cloud (nimbusgo.in)..."))
	fmt.Fprintln(ctx.Stdout, dimStyle.Render("Opening your browser to authorize your CLI session:"))
	fmt.Fprintf(ctx.Stdout, "  %s\n\n", linkStyle.Render(authURL))
	fmt.Fprintln(ctx.Stdout, dimStyle.Render("Waiting for authorization (press Ctrl+C to cancel)..."))

	// Open user's default browser
	_ = openBrowser(authURL)

	tokenChan := make(chan *Credentials, 1)
	errChan := make(chan error, 1)

	mux := http.NewServeMux()
	server := &http.Server{Handler: mux}

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		callbackState := q.Get("state")
		if callbackState != state {
			http.Error(w, "Invalid OAuth state parameter", http.StatusBadRequest)
			errChan <- errors.New("invalid OAuth state mismatch")
			return
		}

		token := q.Get("token")
		if token == "" {
			token = q.Get("access_token")
		}
		if token == "" {
			http.Error(w, "Missing access token in callback", http.StatusBadRequest)
			errChan <- errors.New("missing access token from server")
			return
		}

		email := q.Get("email")
		name := q.Get("name")
		plan := q.Get("plan")
		if plan == "" {
			plan = "pro"
		}
		hasSub := q.Get("has_subscription") == "true" || plan != "free"

		creds := &Credentials{
			AccessToken: token,
			Email:       email,
			Name:        name,
			Plan:        plan,
			HasSub:      hasSub,
			ServerURL:   serverURL,
			ExpiresAt:   time.Now().Add(30 * 24 * time.Hour),
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Nimbus CLI Authenticated</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; background: #0f172a; color: #f8fafc; text-align: center; }
    .card { background: #1e293b; padding: 2.5rem 3.5rem; border-radius: 1rem; border: 1px solid #334155; box-shadow: 0 20px 25px -5px rgba(0,0,0,0.5); }
    h1 { color: #818cf8; margin-bottom: 0.5rem; font-size: 1.75rem; }
    p { color: #94a3b8; font-size: 1rem; margin-top: 0; }
  </style>
</head>
<body>
  <div class="card">
    <h1>✓ Authenticated with Nimbus</h1>
    <p>You can now close this tab and return to your terminal.</p>
  </div>
</body>
</html>`)

		tokenChan <- creds
	})

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	select {
	case creds := <-tokenChan:
		_ = server.Shutdown(context.Background())
		if err := SaveCredentials(creds); err != nil {
			return nil, fmt.Errorf("failed to save credentials: %w", err)
		}
		displayName := creds.Email
		if displayName == "" {
			displayName = creds.Name
		}
		if displayName == "" {
			displayName = "developer"
		}
		fmt.Fprintf(ctx.Stdout, "%s Successfully logged in as %s (%s plan)\n\n", successStyle.Render("✓"), displayName, creds.Plan)
		return creds, nil

	case err := <-errChan:
		_ = server.Shutdown(context.Background())
		return nil, err

	case <-time.After(3 * time.Minute):
		_ = server.Shutdown(context.Background())
		return nil, errors.New("authentication timed out after 3 minutes")
	}
}

// FetchProfile queries the Nimbus Cloud API to verify the token and get the latest profile.
func FetchProfile(creds *Credentials) (*Credentials, error) {
	if creds == nil || creds.AccessToken == "" {
		return nil, errors.New("not authenticated")
	}

	serverURL := creds.ServerURL
	if serverURL == "" {
		serverURL = GetServerURL()
	}

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/auth/user", strings.TrimRight(serverURL, "/")), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("unauthorized: token expired or invalid")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server error (%d): %s", resp.StatusCode, string(body))
	}

	var data struct {
		User struct {
			Email           string `json:"email"`
			Name            string `json:"name"`
			Plan            string `json:"plan"`
			HasSubscription bool   `json:"has_subscription"`
		} `json:"user"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	creds.Email = data.User.Email
	creds.Name = data.User.Name
	creds.Plan = data.User.Plan
	creds.HasSub = data.User.HasSubscription
	_ = SaveCredentials(creds)
	return creds, nil
}

// openBrowser opens the specified URL in the default web browser on the OS.
func openBrowser(targetURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", targetURL)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", targetURL)
	default:
		cmd = exec.Command("xdg-open", targetURL)
	}
	return cmd.Start()
}
