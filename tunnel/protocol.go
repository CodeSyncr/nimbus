// Package tunnel implements `nimbus expose`: a relay that publishes a
// developer's local port on a public HTTPS URL.
//
// The CLI (Agent) dials the relay over a WebSocket carrying its Nimbus Cloud
// CLI token. Both ends wrap that connection in a yamux session, after which
// everything is ordinary Go HTTP: the relay runs a reverse proxy whose
// transport opens a yamux stream per connection, and the agent serves those
// streams with a reverse proxy to localhost. HTTP/1.1, server-sent events
// and WebSocket upgrades all pass through without a bespoke framing format.
package tunnel

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/hashicorp/yamux"
)

const (
	// ConnectPath is the relay endpoint an agent dials.
	ConnectPath = "/connect"
	// HeaderSubdomain carries the name an agent asks for (agent → relay).
	HeaderSubdomain = "X-Nimbus-Subdomain"
	// HeaderTunnelURL returns the public URL on the upgrade response.
	HeaderTunnelURL = "X-Nimbus-Tunnel-Url"
	// HeaderTunnelExpires returns the session deadline (RFC 3339) or "".
	HeaderTunnelExpires = "X-Nimbus-Tunnel-Expires"
	// HeaderTunnelID marks every public response served through a tunnel,
	// so a page can be traced back to an account when abuse is reported.
	HeaderTunnelID = "X-Nimbus-Tunnel"
)

// Grant is what Nimbus Cloud says a CLI token may do. The relay never sees
// plans directly; it enforces exactly these numbers.
type Grant struct {
	UserID uint   `json:"user_id"`
	Plan   string `json:"plan"`
	// Subdomain, when set, is the name the relay MUST publish under: the
	// authorizer resolved the agent's request against its own records (a
	// reservation the account owns). It is authoritative — the relay does
	// not second-guess it — and empty when no name was requested.
	Subdomain string `json:"subdomain"`
	// MaxTunnels caps concurrent tunnels for the account (0 = one).
	MaxTunnels int `json:"max_tunnels"`
	// SessionTTLSeconds closes the tunnel after this long (0 = unlimited).
	SessionTTLSeconds int `json:"session_ttl_seconds"`
	// CustomSubdomain allows choosing the name instead of a random one.
	CustomSubdomain bool `json:"custom_subdomain"`
}

// ErrUnauthorized is returned by an Authorizer for an unknown token.
var ErrUnauthorized = errors.New("invalid or expired token")

// AuthError is a rejection from an Authorizer that carries the HTTP status
// the relay should report, so the CLI can tell "your token expired" from
// "that name belongs to another account" and stop retrying either way.
type AuthError struct {
	Status  int
	Message string
}

func (e *AuthError) Error() string { return e.Message }

// Unwrap lets errors.Is(err, ErrUnauthorized) keep working for 401s.
func (e *AuthError) Unwrap() error {
	if e.Status == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	return nil
}

// 1-40 characters: a letter or digit at each end, hyphens allowed inside.
var subdomainRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])?$`)

// ValidSubdomain reports whether name is usable as a tunnel label.
func ValidSubdomain(name string) bool { return subdomainRe.MatchString(name) }

var (
	adjectives = []string{"brisk", "calm", "clever", "cosmic", "eager", "fuzzy", "gentle", "golden", "happy", "jolly", "keen", "lucky", "merry", "nimble", "proud", "quick", "rapid", "shiny", "sunny", "swift", "tidy", "vivid", "warm", "witty"}
	nouns      = []string{"otter", "falcon", "maple", "comet", "harbor", "lantern", "meadow", "nebula", "orchid", "pebble", "quartz", "river", "saffron", "thistle", "tundra", "violet", "walnut", "zephyr", "beacon", "cedar", "dune", "ember", "fjord", "glacier"}
)

// RandomSubdomain returns a readable, hard-to-guess label such as
// "brisk-otter-3f9a".
func RandomSubdomain() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	adj := adjectives[int(b[0])%len(adjectives)]
	noun := nouns[int(b[1])%len(nouns)]
	return fmt.Sprintf("%s-%s-%s", adj, noun, hex.EncodeToString(b[2:4]))
}

// yamuxConfig is shared by both ends: keepalives detect a dead link within
// a minute, and yamux's own logging stays quiet.
func yamuxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = 20 * time.Second
	cfg.ConnectionWriteTimeout = 30 * time.Second
	cfg.LogOutput = io.Discard
	return cfg
}
