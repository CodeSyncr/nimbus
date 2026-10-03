package middleware

import (
	"context"
	"net"
	"strings"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// TrustedProxies restricts which IP addresses are trusted to set forwarding
// headers (X-Forwarded-For, X-Real-Ip, X-Forwarded-Proto). When the request
// comes from an untrusted proxy, those headers are stripped to prevent spoofing.
//
// Usage:
//
//	r.Use(middleware.TrustedProxies("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"))
func TrustedProxies(cidrs ...string) router.Middleware {
	networks := parseCIDRs(cidrs)
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			remoteIP := extractIP(c.Request.RemoteAddr)
			clientIP := remoteIP
			if isTrusted(remoteIP, cidrs, networks) {
				if xff := c.Request.Header.Get("X-Forwarded-For"); xff != "" {
					chain := strings.Split(xff, ",")
					for i := len(chain) - 1; i >= 0 && isTrusted(clientIP, cidrs, networks); i-- {
						parsed := net.ParseIP(strings.TrimSpace(chain[i]))
						if parsed == nil {
							clientIP = remoteIP
							break
						}
						clientIP = parsed.String()
					}
				} else if ip := net.ParseIP(c.Request.Header.Get("X-Real-Ip")); ip != nil {
					clientIP = ip.String()
				}
			}
			if !isTrusted(remoteIP, cidrs, networks) {
				c.Request.Header.Del("X-Forwarded-For")
				c.Request.Header.Del("X-Real-Ip")
				c.Request.Header.Del("X-Forwarded-Proto")
				c.Request.Header.Del("X-Forwarded-Host")
			}
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), clientIPKey{}, clientIP))
			return next(c)
		}
	}
}

// parseCIDRs pre-parses CIDR strings at middleware init so we pay the cost once.
func parseCIDRs(cidrs []string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		if !strings.Contains(cidr, "/") {
			continue
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		networks = append(networks, network)
	}
	return networks
}

// extractIP strips the port from addr (e.g. "10.0.0.1:1234" -> "10.0.0.1").
func extractIP(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	if ip := net.ParseIP(addr); ip != nil {
		return ip.String()
	}
	return addr
}

type clientIPKey struct{}

// ClientIP uses forwarding information only after TrustedProxies has validated
// the peer and walked the proxy chain. Without it, only RemoteAddr is used.
func ClientIP(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPKey{}).(string); ok {
		return ip
	}
	return extractIP(r.RemoteAddr)
}

// isTrusted checks if the IP falls within any of the given CIDR ranges or matches exactly.
func isTrusted(ip string, cidrs []string, networks []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed != nil {
		for _, network := range networks {
			if network.Contains(parsed) {
				return true
			}
		}
	}
	for _, cidr := range cidrs {
		if !strings.Contains(cidr, "/") && ip == cidr {
			return true
		}
	}
	return false
}
