package middleware

import (
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
			if !isTrusted(remoteIP, cidrs, networks) {
				c.Request.Header.Del("X-Forwarded-For")
				c.Request.Header.Del("X-Real-Ip")
				c.Request.Header.Del("X-Forwarded-Proto")
				c.Request.Header.Del("X-Forwarded-Host")
			}
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
	if i := strings.LastIndex(addr, ":"); i > 0 {
		if addr[0] == '[' {
			if j := strings.LastIndex(addr, "]"); j > 0 {
				return addr[1:j]
			}
		}
		return addr[:i]
	}
	return addr
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
