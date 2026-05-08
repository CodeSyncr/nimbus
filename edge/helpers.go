package edge

import (
	"fmt"
	"net/http"
	"strings"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func extractIP(r *http.Request) string {
	// Check standard proxy headers.
	for _, h := range []string{"CF-Connecting-IP", "X-Real-IP", "X-Forwarded-For"} {
		if v := r.Header.Get(h); v != "" {
			// X-Forwarded-For may contain multiple IPs.
			if idx := strings.IndexByte(v, ','); idx > 0 {
				return strings.TrimSpace(v[:idx])
			}
			return v
		}
	}

	// Fall back to RemoteAddr.
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx > 0 {
		return addr[:idx]
	}
	return addr
}

func parseFloat(s string) float64 {
	if s == "" {
		return 0
	}
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

func fnvHash(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func basicEncode(user, pass string) string {
	// Simple base64 encoding.
	const base64Chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	input := []byte(user + ":" + pass)
	var result strings.Builder
	for i := 0; i < len(input); i += 3 {
		var b0, b1, b2 byte
		b0 = input[i]
		if i+1 < len(input) {
			b1 = input[i+1]
		}
		if i+2 < len(input) {
			b2 = input[i+2]
		}
		result.WriteByte(base64Chars[b0>>2])
		result.WriteByte(base64Chars[((b0&3)<<4)|(b1>>4)])
		if i+1 < len(input) {
			result.WriteByte(base64Chars[((b1&0x0F)<<2)|(b2>>6)])
		} else {
			result.WriteByte('=')
		}
		if i+2 < len(input) {
			result.WriteByte(base64Chars[b2&0x3F])
		} else {
			result.WriteByte('=')
		}
	}
	return result.String()
}
