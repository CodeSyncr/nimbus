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

	fmt.Fprintln(ctx.Stdout, headerStyle.Render("🔐 Authenticating with Nimbus Cloud (nimbusgo.space)..."))
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
		fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Terminal Authorized — Nimbus Cloud</title>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Mona+Sans:ital,wdth,wght@0,75..125,200..900;1,75..125,200..900&family=DM+Mono:wght@400;500&display=swap" rel="stylesheet">
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: 'Mona Sans', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      background: #ffffff;
      color: #111827;
      min-height: 100vh;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      padding: 24px;
      position: relative;
    }
    body::before {
      content: '';
      position: absolute;
      top: 0; left: 0; right: 0; height: 3px;
      background: linear-gradient(90deg, #e74c3c 0%%, #f97316 50%%, #fbbf24 100%%);
    }
    .card {
      max-width: 480px;
      width: 100%%;
      background: #ffffff;
      border: 1px solid #e5e7eb;
      border-radius: 16px;
      padding: 40px 36px 32px;
      text-align: center;
      box-shadow: 0 20px 40px rgba(0,0,0,0.06), 0 1px 3px rgba(0,0,0,0.04);
    }
    .logo-badge {
      width: 48px;
      height: 48px;
      background: linear-gradient(135deg, #e74c3c 0%%, #f97316 100%%);
      border-radius: 12px;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      color: #ffffff;
      margin-bottom: 20px;
      box-shadow: 0 8px 16px rgba(231, 76, 60, 0.2);
    }
    h1 {
      font-size: 1.35rem;
      font-weight: 800;
      letter-spacing: -0.03em;
      color: #111827;
      margin-bottom: 8px;
    }
    .desc {
      font-size: 0.9rem;
      color: #6b7280;
      line-height: 1.5;
      margin-bottom: 24px;
    }
    .user-pill {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      background: #fafafa;
      border: 1px solid #e5e7eb;
      padding: 8px 14px;
      border-radius: 99px;
      font-size: 0.82rem;
      color: #374151;
      font-family: 'DM Mono', monospace;
      margin-bottom: 28px;
    }
    .status-dot {
      width: 7px;
      height: 7px;
      background: #22c55e;
      border-radius: 50%%;
      display: inline-block;
    }
    .terminal-tip {
      background: #18181f;
      border-radius: 10px;
      padding: 14px 16px;
      text-align: left;
      font-family: 'DM Mono', monospace;
      font-size: 0.8rem;
      color: #dde1ed;
      margin-bottom: 24px;
      border: 1px solid rgba(255,255,255,0.06);
    }
    .terminal-tip .label {
      color: #64748b;
      font-size: 0.72rem;
      margin-bottom: 6px;
      display: block;
    }
    .terminal-tip .cmd {
      color: #86efac;
    }
    .btn-group {
      display: flex;
      gap: 10px;
      justify-content: center;
    }
    .btn {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 6px;
      padding: 9px 18px;
      border-radius: 8px;
      font-size: 0.84rem;
      font-weight: 600;
      text-decoration: none;
      transition: all 0.15s;
    }
    .btn-primary {
      background: #e74c3c;
      color: #ffffff;
    }
    .btn-primary:hover {
      background: #c0392b;
    }
    .btn-secondary {
      background: #ffffff;
      color: #374151;
      border: 1px solid #e5e7eb;
    }
    .btn-secondary:hover {
      background: #fafafa;
      border-color: #d1d5db;
    }
    .footer-note {
      margin-top: 24px;
      font-size: 0.78rem;
      color: #9ca3af;
    }
  </style>
</head>
<body>
  <div class="card">
    <div class="logo-badge">
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"><path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/></svg>
    </div>
    <h1>Terminal Authorized</h1>
    <p class="desc">Your Nimbus CLI session is now authenticated. You can safely close this browser window and return to your terminal.</p>
    
    <div class="user-pill">
      <span class="status-dot"></span>
      <span>%s</span>
      <span style="color: #9ca3af;">·</span>
      <span style="text-transform: capitalize; font-weight: 600; color: #111827;">%s Plan</span>
    </div>

    <div class="terminal-tip">
      <span class="label">Ready in terminal:</span>
      <span>$ <span class="cmd">nimbus ai "generate blog controller"</span></span>
    </div>

    <div class="btn-group">
      <a href="%s/cloud/dashboard" class="btn btn-primary">Open Dashboard</a>
      <a href="%s/docs" class="btn btn-secondary">Read Docs</a>
    </div>
  </div>
  
  <p class="footer-note">Nimbus Cloud · <a href="%s" style="color: inherit; text-decoration: underline;">nimbusgo.space</a></p>
</body>
</html>`, email, plan, serverURL, serverURL, serverURL)

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
