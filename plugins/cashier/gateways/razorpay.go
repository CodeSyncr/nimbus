package gateways

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
)

// RazorpayConfig configures the Razorpay gateway (India).
type RazorpayConfig struct {
	KeyID         string
	KeySecret     string
	WebhookSecret string
}

// RazorpayGateway implements contracts.PaymentGateway via Razorpay Orders +
// client-side Checkout. Flow: CreateCharge → an order id → the frontend opens
// Razorpay Checkout → on success it returns {order_id, payment_id, signature}
// which VerifyPayment validates. Webhooks are verified with VerifyWebhook.
type RazorpayGateway struct {
	cfg     RazorpayConfig
	baseURL string
	http    *http.Client
}

// NewRazorpay builds the Razorpay gateway.
func NewRazorpay(cfg RazorpayConfig) *RazorpayGateway {
	return &RazorpayGateway{cfg: cfg, baseURL: "https://api.razorpay.com", http: &http.Client{Timeout: 30 * time.Second}}
}

func (r *RazorpayGateway) Name() string { return "razorpay" }

// CreateCharge creates a Razorpay order (Charge.ID is the order id; no redirect).
func (r *RazorpayGateway) CreateCharge(ctx context.Context, p contracts.ChargeParams) (*contracts.Charge, error) {
	if p.Amount <= 0 {
		return nil, fmt.Errorf("cashier/razorpay: Amount (in paise) is required")
	}
	currency := firstNonEmpty(p.Currency, "INR")
	req := map[string]any{"amount": p.Amount, "currency": currency}
	if p.Reference != "" {
		req["receipt"] = p.Reference
	}
	if len(p.Metadata) > 0 {
		req["notes"] = p.Metadata
	}
	var out map[string]any
	if err := r.do(ctx, http.MethodPost, "/v1/orders", req, &out); err != nil {
		return nil, err
	}
	return &contracts.Charge{Gateway: r.Name(), ID: str(out["id"]), Amount: p.Amount, Currency: currency, Raw: out}, nil
}

// VerifyPayment validates a Checkout callback: HMAC-SHA256("<order>|<payment>",
// key_secret) must equal the returned signature (constant-time).
func (r *RazorpayGateway) VerifyPayment(ctx context.Context, proof contracts.PaymentProof) (bool, error) {
	if proof.OrderID == "" || proof.PaymentID == "" || proof.Signature == "" {
		return false, fmt.Errorf("cashier/razorpay: OrderID, PaymentID and Signature are required")
	}
	mac := hmac.New(sha256.New, []byte(r.cfg.KeySecret))
	mac.Write([]byte(proof.OrderID + "|" + proof.PaymentID))
	got, err := hex.DecodeString(proof.Signature)
	if err != nil {
		return false, nil
	}
	return hmac.Equal(got, mac.Sum(nil)), nil
}

// VerifyWebhook validates X-Razorpay-Signature (HMAC-SHA256 of the raw body).
func (r *RazorpayGateway) VerifyWebhook(payload []byte, headers http.Header) (*contracts.WebhookEvent, error) {
	if r.cfg.WebhookSecret == "" {
		return nil, fmt.Errorf("cashier/razorpay: webhook secret not set")
	}
	sig := headers.Get("X-Razorpay-Signature")
	if sig == "" {
		return nil, fmt.Errorf("cashier/razorpay: missing X-Razorpay-Signature")
	}
	mac := hmac.New(sha256.New, []byte(r.cfg.WebhookSecret))
	mac.Write(payload)
	got, err := hex.DecodeString(sig)
	if err != nil || !hmac.Equal(got, mac.Sum(nil)) {
		return nil, fmt.Errorf("cashier/razorpay: webhook signature verification failed")
	}
	var env struct {
		Event string `json:"event"`
	}
	_ = json.Unmarshal(payload, &env)
	return &contracts.WebhookEvent{Gateway: r.Name(), Type: env.Event, Raw: payload}, nil
}

func (r *RazorpayGateway) do(ctx context.Context, method, path string, in, out any) error {
	if r.cfg.KeyID == "" || r.cfg.KeySecret == "" {
		return fmt.Errorf("cashier/razorpay: KeyID/KeySecret not set")
	}
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.baseURL+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(r.cfg.KeyID, r.cfg.KeySecret)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cashier/razorpay: error %d: %s", resp.StatusCode, string(raw))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
