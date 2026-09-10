package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/cli/auth"
	"github.com/CodeSyncr/nimbus/tunnel"
	"github.com/spf13/cobra"
)

// DefaultRelayURL is the hosted relay; NIMBUS_TUNNEL_URL or --relay override it.
const DefaultRelayURL = "wss://tunnel.nimbusgo.space/connect"

func init() {
	cli.RegisterCommand(&ExposeCommand{})
}

// ExposeCommand publishes a local port on a public HTTPS URL through the
// Nimbus tunnel relay: `nimbus expose [port]`.
type ExposeCommand struct {
	subdomain string
	keepHost  bool
	relay     string
	host      string
}

func (c *ExposeCommand) Name() string { return "expose" }
func (c *ExposeCommand) Description() string {
	return "Expose a local port on a public HTTPS URL for testing (webhooks, mobile devices, demos)"
}
func (c *ExposeCommand) Args() int { return -1 }

func (c *ExposeCommand) Flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.subdomain, "subdomain", "", "Reserve a fixed name, e.g. --subdomain myapp (Pro)")
	cmd.Flags().BoolVar(&c.keepHost, "keep-host", false, "Forward the public Host header instead of the local address")
	cmd.Flags().StringVar(&c.relay, "relay", "", "Relay to connect through (default $NIMBUS_TUNNEL_URL or "+DefaultRelayURL+")")
	cmd.Flags().StringVar(&c.host, "host", "127.0.0.1", "Local host the app listens on")
}

func (c *ExposeCommand) Run(ctx *cli.Context) error {
	port, err := c.port(ctx)
	if err != nil {
		return err
	}
	creds, err := auth.LoadCredentials()
	if err != nil || creds == nil || creds.IsExpired() {
		ctx.UI.Errorf("You are not logged in to Nimbus Cloud.")
		ctx.UI.Infof("Run 'nimbus login' first, then 'nimbus expose' again.")
		return errors.New("not logged in")
	}
	relayURL := c.relay
	if relayURL == "" {
		relayURL = os.Getenv("NIMBUS_TUNNEL_URL")
	}
	if relayURL == "" {
		relayURL = DefaultRelayURL
	}
	relayURL, err = tunnel.NormalizeRelayURL(relayURL)
	if err != nil {
		return err
	}

	local := net.JoinHostPort(c.host, strconv.Itoa(port))
	agent := &tunnel.Agent{
		RelayURL:  relayURL,
		Token:     creds.AccessToken,
		LocalAddr: local,
		Subdomain: strings.ToLower(strings.TrimSpace(c.subdomain)),
		KeepHost:  c.keepHost,
		OnRequest: func(r tunnel.RequestLog) {
			fmt.Fprintf(ctx.Stdout, "%s  %-7s %s  %s  %s\n",
				time.Now().Format("15:04:05"), r.Method, colorStatus(r.Status), r.Path, r.Duration.Round(time.Millisecond))
		},
	}

	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if _, err := net.DialTimeout("tcp", local, time.Second); err != nil {
		ctx.UI.Warnf("Nothing is listening on %s yet; requests will fail until the app starts.", local)
	}

	backoff := 2 * time.Second
	first := true
	for {
		sess, err := agent.Connect(runCtx)
		if err != nil {
			if runCtx.Err() != nil {
				return nil
			}
			var ce *tunnel.ConnectError
			if errors.As(err, &ce) && ce.Permanent() {
				ctx.UI.Errorf("Relay refused the tunnel: %s", ce.Message)
				return err
			}
			ctx.UI.Warnf("Could not reach the relay (%v); retrying in %s", err, backoff)
			if !sleep(runCtx, backoff) {
				return nil
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 2 * time.Second
		if first {
			c.banner(ctx, sess, local, creds)
			first = false
		} else {
			ctx.UI.Successf("Reconnected: %s", sess.URL)
		}

		err = sess.Serve(runCtx)
		if runCtx.Err() != nil {
			fmt.Fprintln(ctx.Stdout)
			ctx.UI.Infof("Tunnel closed.")
			return nil
		}
		if !sess.Expires.IsZero() && time.Now().After(sess.Expires) {
			ctx.UI.Warnf("This session reached the time limit of the %s plan. Run 'nimbus expose' again, or upgrade for unlimited sessions.", planName(creds))
			return nil
		}
		if err != nil {
			ctx.UI.Warnf("Tunnel disconnected (%v); reconnecting…", err)
		} else {
			ctx.UI.Warnf("Tunnel disconnected; reconnecting…")
		}
		if !sleep(runCtx, backoff) {
			return nil
		}
	}
}

func (c *ExposeCommand) banner(ctx *cli.Context, sess *tunnel.Session, local string, creds *auth.Credentials) {
	body := fmt.Sprintf("Public URL   %s\nForwarding   → http://%s\nAccount      %s (%s)", sess.URL, local, creds.Email, planName(creds))
	if !sess.Expires.IsZero() {
		body += fmt.Sprintf("\nSession ends %s", sess.Expires.Local().Format("15:04"))
	}
	if c.keepHost {
		body += "\nHost header  kept as public host"
	}
	ctx.UI.Panel("nimbus expose", body)
	ctx.UI.Infof("Press Ctrl+C to stop. Requests appear below.")
	fmt.Fprintln(ctx.Stdout)
}

// port resolves the port to expose: the argument, else APP_PORT/PORT from
// the app's .env, else the framework default 3333.
func (c *ExposeCommand) port(ctx *cli.Context) (int, error) {
	if len(ctx.Args) > 0 {
		p, err := strconv.Atoi(strings.TrimPrefix(ctx.Args[0], ":"))
		if err != nil || p < 1 || p > 65535 {
			return 0, fmt.Errorf("invalid port %q", ctx.Args[0])
		}
		return p, nil
	}
	if ctx.AppRoot != "" {
		if p := envPort(filepath.Join(ctx.AppRoot, ".env")); p > 0 {
			return p, nil
		}
	}
	return 3333, nil
}

// envPort reads APP_PORT or PORT from a dotenv file without loading it.
func envPort(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	found := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		if key != "APP_PORT" && key != "PORT" {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if p, err := strconv.Atoi(val); err == nil && p > 0 {
			if key == "APP_PORT" {
				return p
			}
			found = p
		}
	}
	return found
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func colorStatus(code int) string {
	color := "\033[32m" // 2xx green
	switch {
	case code >= 500:
		color = "\033[31m"
	case code >= 400:
		color = "\033[33m"
	case code >= 300:
		color = "\033[36m"
	}
	return fmt.Sprintf("%s%d\033[0m", color, code)
}

func planName(creds *auth.Credentials) string {
	if creds.Plan == "" {
		return "free"
	}
	return creds.Plan
}
