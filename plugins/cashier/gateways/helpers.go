// Package gateways holds the concrete payment gateways (Stripe, Razorpay, PayU,
// …). Each implements contracts.PaymentGateway. To add a gateway, drop a new
// file here implementing the interface and register it in the manager.
package gateways

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
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

// int64Of coerces a decoded JSON number (float64) or numeric string to int64.
// Gateway responses arrive as map[string]any, so amounts and timestamps come
// back as float64 and need narrowing before they mean anything.
func int64Of(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	}
	return 0
}

// stripeFirstPrice reads the price id of a subscription's first item.
func stripeFirstPrice(sub map[string]any) string {
	item := stripeFirstItem(sub)
	if item == nil {
		return ""
	}
	if price, ok := item["price"].(map[string]any); ok {
		return str(price["id"])
	}
	return str(item["price"])
}

// stripeFirstItemID reads the id of a subscription's first item — the handle
// Stripe needs to swap a plan.
func stripeFirstItemID(sub map[string]any) string {
	item := stripeFirstItem(sub)
	if item == nil {
		return ""
	}
	return str(item["id"])
}

func stripeFirstItem(sub map[string]any) map[string]any {
	items, ok := sub["items"].(map[string]any)
	if !ok {
		return nil
	}
	data, ok := items["data"].([]any)
	if !ok || len(data) == 0 {
		return nil
	}
	item, _ := data[0].(map[string]any)
	return item
}
