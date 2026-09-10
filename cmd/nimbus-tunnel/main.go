// nimbus-tunnel is the relay behind `nimbus expose`. It publishes each
// connected agent on "<name>.$TUNNEL_DOMAIN" and asks Nimbus Cloud what a
// CLI token may do.
//
// Environment:
//
//	TUNNEL_DOMAIN     suffix for tunnels (default tunnel.nimbusgo.space)
//	TUNNEL_ADDR       listen address (default :8090); TLS is the edge's job
//	NIMBUS_CLOUD_URL  cloud base URL (default https://nimbusgo.space)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/CodeSyncr/nimbus/tunnel"
)

func main() {
	domain := envOr("TUNNEL_DOMAIN", "tunnel.nimbusgo.space")
	addr := envOr("TUNNEL_ADDR", ":8090")
	cloud := strings.TrimRight(envOr("NIMBUS_CLOUD_URL", "https://nimbusgo.space"), "/")

	relay := tunnel.NewRelay(domain, cloudAuthorizer(cloud))
	relay.Logf = log.Printf

	srv := &http.Server{
		Addr:              addr,
		Handler:           relay,
		ReadHeaderTimeout: 10 * time.Second,
		// No write/idle timeouts: tunnels and streamed responses are long-lived.
	}
	go func() {
		log.Printf("nimbus-tunnel listening on %s, publishing *.%s, cloud %s", addr, domain, cloud)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
}

// cloudAuthorizer asks Nimbus Cloud to resolve a CLI token into a Grant.
func cloudAuthorizer(cloud string) tunnel.Authorizer {
	client := &http.Client{Timeout: 10 * time.Second}
	return func(ctx context.Context, token string) (tunnel.Grant, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, cloud+"/api/v1/tunnel/authorize", nil)
		if err != nil {
			return tunnel.Grant{}, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return tunnel.Grant{}, err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		switch {
		case resp.StatusCode == http.StatusOK:
			var g tunnel.Grant
			if err := json.Unmarshal(body, &g); err != nil {
				return tunnel.Grant{}, fmt.Errorf("bad grant from cloud: %w", err)
			}
			return g, nil
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusPaymentRequired:
			var payload struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal(body, &payload)
			if payload.Error == "" {
				payload.Error = tunnel.ErrUnauthorized.Error()
			}
			return tunnel.Grant{}, fmt.Errorf("%w: %s", tunnel.ErrUnauthorized, payload.Error)
		default:
			return tunnel.Grant{}, fmt.Errorf("cloud answered %s", resp.Status)
		}
	}
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
