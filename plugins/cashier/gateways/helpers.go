// Package gateways holds the concrete payment gateways (Stripe, Razorpay, PayU,
// …). Each implements contracts.PaymentGateway. To add a gateway, drop a new
// file here implementing the interface and register it in the manager.
package gateways

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// randomHex returns n random bytes hex-encoded (2n chars).
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
