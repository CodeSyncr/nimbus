package middleware

import (
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
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			remoteIP := extractIP(c.Request.RemoteAddr)
			if !isTrusted(remoteIP, cidrs) {
				// Strip forwarding headers from untrusted proxies
				c.Request.Header.Del("X-Forwarded-For")
				c.Request.Header.Del("X-Real-Ip")
				c.Request.Header.Del("X-Forwarded-Proto")
				c.Request.Header.Del("X-Forwarded-Host")
			}
			return next(c)
		}
	}
}

// extractIP strips the port from addr (e.g. "10.0.0.1:1234" → "10.0.0.1").
func extractIP(addr string) string {
	if i := strings.LastIndex(addr, ":"); i > 0 {
		// Check if it's IPv6 [::1]:port
		if addr[0] == '[' {
			if j := strings.LastIndex(addr, "]"); j > 0 {
				return addr[1:j]
			}
		}
		return addr[:i]
	}
	return addr
}

// isTrusted checks if the IP falls within any of the given CIDR ranges.
// Also supports exact IP matching.
func isTrusted(ip string, cidrs []string) bool {
	for _, cidr := range cidrs {
		if strings.Contains(cidr, "/") {
			// Simple prefix match for common private ranges
			if matchCIDR(ip, cidr) {
				return true
			}
		} else if ip == cidr {
			return true
		}
	}
	return false
}

// matchCIDR does a simple string-based CIDR prefix match for the most common
// private network ranges. For full CIDR parsing, use net.ParseCIDR.
func matchCIDR(ip, cidr string) bool {
	parts := strings.SplitN(cidr, "/", 2)
	if len(parts) != 2 {
		return false
	}
	prefix := parts[0]
	// Common ranges: match by octet prefix
	switch cidr {
	case "10.0.0.0/8":
		return strings.HasPrefix(ip, "10.")
	case "172.16.0.0/12":
		return strings.HasPrefix(ip, "172.16.") || strings.HasPrefix(ip, "172.17.") ||
			strings.HasPrefix(ip, "172.18.") || strings.HasPrefix(ip, "172.19.") ||
			strings.HasPrefix(ip, "172.20.") || strings.HasPrefix(ip, "172.21.") ||
			strings.HasPrefix(ip, "172.22.") || strings.HasPrefix(ip, "172.23.") ||
			strings.HasPrefix(ip, "172.24.") || strings.HasPrefix(ip, "172.25.") ||
			strings.HasPrefix(ip, "172.26.") || strings.HasPrefix(ip, "172.27.") ||
			strings.HasPrefix(ip, "172.28.") || strings.HasPrefix(ip, "172.29.") ||
			strings.HasPrefix(ip, "172.30.") || strings.HasPrefix(ip, "172.31.")
	case "192.168.0.0/16":
		return strings.HasPrefix(ip, "192.168.")
	case "127.0.0.0/8":
		return strings.HasPrefix(ip, "127.")
	default:
		// Fallback: exact prefix match on the network address
		return strings.HasPrefix(ip, prefix)
	}
}
