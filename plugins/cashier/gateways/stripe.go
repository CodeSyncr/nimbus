package gateways

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
)

// StripeConfig configures the Stripe gateway (international hosted checkout).
type StripeConfig struct {
	SecretKey     string        // sk_...
	WebhookSecret string        // whsec_...
	Tolerance     time.Duration // webhook timestamp tolerance (default 5m)
}

// StripeGateway implements contracts.PaymentGateway via Stripe hosted Checkout.
type StripeGateway struct {
	cfg     StripeConfig
	baseURL string
	http    *http.Client
}

// NewStripe builds the Stripe gateway.
func NewStripe(cfg StripeConfig) *StripeGateway {
	if cfg.Tolerance == 0 {
		cfg.Tolerance = 5 * time.Minute
	}
	return &StripeGateway{cfg: cfg, baseURL: "https://api.stripe.com", http: &http.Client{Timeout: 30 * time.Second}}
}

func (s *StripeGateway) Name() string { return "stripe" }

// CreateCharge creates a hosted Checkout session; redirect to Charge.RedirectURL.
func (s *StripeGateway) CreateCharge(ctx context.Context, p contracts.ChargeParams) (*contracts.Charge, error) {
	mode := firstNonEmpty(p.Mode, "payment")
	form := url.Values{}
	form.Set("mode", mode)
	if p.PriceID != "" {
		form.Set("line_items[0][price]", p.PriceID)
		form.Set("line_items[0][quantity]", "1")
	} else if p.Amount > 0 {
		form.Set("line_items[0][quantity]", "1")
		form.Set("line_items[0][price_data][currency]", strings.ToLower(p.Currency))
		form.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(p.Amount, 10))
		form.Set("line_items[0][price_data][product_data][name]", firstNonEmpty(p.Reference, "Payment"))
	} else {
		return nil, fmt.Errorf("cashier/stripe: ChargeParams needs PriceID or Amount")
	}
	if p.SuccessURL != "" {
		form.Set("success_url", p.SuccessURL)
	}
	if p.CancelURL != "" {
		form.Set("cancel_url", p.CancelURL)
	}
	if p.CustomerID != "" {
		form.Set("customer", p.CustomerID)
	} else if p.CustomerEmail != "" {
		form.Set("customer_email", p.CustomerEmail)
	}
	if p.Reference != "" {
		form.Set("client_reference_id", p.Reference)
	}
	for k, v := range p.Metadata {
		form.Set("metadata["+k+"]", v)
	}

	var out map[string]any
	if err := s.do(ctx, http.MethodPost, "/v1/checkout/sessions", form, &out); err != nil {
		return nil, err
	}
	return &contracts.Charge{
		Gateway:     s.Name(),
		ID:          str(out["id"]),
		RedirectURL: str(out["url"]),
		Amount:      p.Amount,
		Currency:    p.Currency,
		Raw:         out,
	}, nil
}

// VerifyPayment retrieves the Checkout session (id in proof.OrderID) and reports
// whether it is paid.
func (s *StripeGateway) VerifyPayment(ctx context.Context, proof contracts.PaymentProof) (bool, error) {
	if proof.OrderID == "" {
		return false, fmt.Errorf("cashier/stripe: VerifyPayment needs the checkout session id in OrderID")
	}
	var out map[string]any
	if err := s.do(ctx, http.MethodGet, "/v1/checkout/sessions/"+url.PathEscape(proof.OrderID), nil, &out); err != nil {
		return false, err
	}
	return str(out["payment_status"]) == "paid", nil
}

// VerifyWebhook validates the Stripe-Signature header (HMAC-SHA256 of "t.body").
func (s *StripeGateway) VerifyWebhook(payload []byte, headers http.Header) (*contracts.WebhookEvent, error) {
	if s.cfg.WebhookSecret == "" {
		return nil, fmt.Errorf("cashier/stripe: webhook secret not set")
	}
	ts, sigs := parseStripeSig(headers.Get("Stripe-Signature"))
	if ts == 0 || len(sigs) == 0 {
		return nil, fmt.Errorf("cashier/stripe: malformed Stripe-Signature")
	}
	if s.cfg.Tolerance > 0 {
		if age := time.Since(time.Unix(ts, 0)); age > s.cfg.Tolerance || age < -s.cfg.Tolerance {
			return nil, fmt.Errorf("cashier/stripe: webhook timestamp outside tolerance")
		}
	}
	mac := hmac.New(sha256.New, []byte(s.cfg.WebhookSecret))
	mac.Write([]byte(strconv.FormatInt(ts, 10) + "."))
	mac.Write(payload)
	expected := mac.Sum(nil)
	ok := false
	for _, sig := range sigs {
		if b, err := hex.DecodeString(sig); err == nil && hmac.Equal(b, expected) {
			ok = true
			break
		}
	}
	if !ok {
		return nil, fmt.Errorf("cashier/stripe: signature verification failed")
	}
	var env struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	_ = json.Unmarshal(payload, &env)
	return &contracts.WebhookEvent{Gateway: s.Name(), Type: env.Type, ID: env.ID, Raw: payload}, nil
}

func (s *StripeGateway) do(ctx context.Context, method, path string, form url.Values, out any) error {
	if s.cfg.SecretKey == "" {
		return fmt.Errorf("cashier/stripe: secret key not set")
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.SecretKey)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cashier/stripe: error %d: %s", resp.StatusCode, string(raw))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func parseStripeSig(header string) (ts int64, sigs []string) {
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			ts, _ = strconv.ParseInt(kv[1], 10, 64)
		case "v1":
			sigs = append(sigs, kv[1])
		}
	}
	return
}
