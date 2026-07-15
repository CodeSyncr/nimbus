package gateways

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
)

// ── Razorpay ──────────────────────────────────────────────────────

func rzpSig(secret, data string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(data))
	return hex.EncodeToString(m.Sum(nil))
}

func TestRazorpay_VerifyPayment(t *testing.T) {
	g := NewRazorpay(RazorpayConfig{KeySecret: "secret"})
	order, pay := "order_1", "pay_1"
	sig := rzpSig("secret", order+"|"+pay)

	ok, err := g.VerifyPayment(context.Background(), contracts.PaymentProof{OrderID: order, PaymentID: pay, Signature: sig})
	if err != nil || !ok {
		t.Fatalf("valid payment rejected: ok=%v err=%v", ok, err)
	}
	// tampered payment id
	ok, _ = g.VerifyPayment(context.Background(), contracts.PaymentProof{OrderID: order, PaymentID: "pay_HACK", Signature: sig})
	if ok {
		t.Fatal("tampered payment accepted")
	}
}

func TestRazorpay_VerifyWebhook(t *testing.T) {
	g := NewRazorpay(RazorpayConfig{WebhookSecret: "whsec"})
	body := []byte(`{"event":"payment.captured"}`)
	h := http.Header{}
	h.Set("X-Razorpay-Signature", rzpSig("whsec", string(body)))

	ev, err := g.VerifyWebhook(body, h)
	if err != nil || ev.Type != "payment.captured" || ev.Gateway != "razorpay" {
		t.Fatalf("valid webhook: ev=%+v err=%v", ev, err)
	}
	h.Set("X-Razorpay-Signature", rzpSig("wrong", string(body)))
	if _, err := g.VerifyWebhook(body, h); err == nil {
		t.Fatal("bad signature accepted")
	}
}

// ── Stripe ────────────────────────────────────────────────────────

func stripeSig(secret string, ts int64, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(fmt.Sprintf("%d.", ts)))
	m.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(m.Sum(nil)))
}

func TestStripe_VerifyWebhook(t *testing.T) {
	g := NewStripe(StripeConfig{WebhookSecret: "whsec"})
	body := []byte(`{"id":"evt_1","type":"checkout.session.completed"}`)
	h := http.Header{}
	h.Set("Stripe-Signature", stripeSig("whsec", time.Now().Unix(), body))

	ev, err := g.VerifyWebhook(body, h)
	if err != nil || ev.Type != "checkout.session.completed" {
		t.Fatalf("valid webhook: ev=%+v err=%v", ev, err)
	}
	h.Set("Stripe-Signature", stripeSig("wrong", time.Now().Unix(), body))
	if _, err := g.VerifyWebhook(body, h); err == nil {
		t.Fatal("bad secret accepted")
	}
	// replay
	h.Set("Stripe-Signature", stripeSig("whsec", time.Now().Add(-time.Hour).Unix(), body))
	if _, err := g.VerifyWebhook(body, h); err == nil {
		t.Fatal("stale timestamp accepted")
	}
}

func TestStripe_CreateCharge(t *testing.T) {
	var form string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		form = string(b)
		io.WriteString(w, `{"id":"cs_1","url":"https://checkout.stripe.com/c/cs_1"}`)
	}))
	defer srv.Close()

	g := NewStripe(StripeConfig{SecretKey: "sk_test"})
	g.baseURL = srv.URL
	ch, err := g.CreateCharge(context.Background(), contracts.ChargeParams{Mode: "subscription", PriceID: "price_x", SuccessURL: "https://ok"})
	if err != nil {
		t.Fatal(err)
	}
	if ch.RedirectURL == "" || ch.ID != "cs_1" || ch.Gateway != "stripe" {
		t.Fatalf("charge: %+v", ch)
	}
	if !strings.Contains(form, "mode=subscription") || !strings.Contains(form, "price_x") {
		t.Fatalf("form: %s", form)
	}
}

// ── PayU ──────────────────────────────────────────────────────────

func TestPayU_HashRoundTrip(t *testing.T) {
	g := NewPayU(PayUConfig{MerchantKey: "mkey", MerchantSalt: "msalt"})

	// A charge produces a signed request form.
	ch, err := g.CreateCharge(context.Background(), contracts.ChargeParams{
		Amount:        49900, // ₹499.00
		CustomerEmail: "a@b.com",
		Reference:     "txn_1",
		Metadata:      map[string]string{"firstname": "Alice", "productinfo": "Pro"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ch.RedirectURL == "" || ch.Raw["hash"] == "" {
		t.Fatalf("charge missing form/hash: %+v", ch)
	}

	// Simulate a valid PayU callback: build the reverse hash the same way.
	var udf [10]string
	valid := url.Values{
		"status": {"success"}, "txnid": {"txn_1"}, "amount": {"499.00"},
		"productinfo": {"Pro"}, "firstname": {"Alice"}, "email": {"a@b.com"},
	}
	valid.Set("hash", g.responseHash("success", "txn_1", "499.00", "Pro", "Alice", "a@b.com", udf))

	ev, err := g.VerifyWebhook([]byte(valid.Encode()), http.Header{})
	if err != nil || ev.Type != "success" || ev.ID != "txn_1" {
		t.Fatalf("valid callback rejected: ev=%+v err=%v", ev, err)
	}

	// Tampered amount → hash mismatch.
	bad := url.Values{}
	for k, v := range valid {
		bad[k] = v
	}
	bad.Set("amount", "1.00")
	if _, err := g.VerifyWebhook([]byte(bad.Encode()), http.Header{}); err == nil {
		t.Fatal("tampered PayU callback accepted")
	}
}
