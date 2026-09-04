package gateways

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
)

// The capability interfaces are the whole design; a gateway that claims a
// capability must actually satisfy the interface at compile time. These assert
// it at run time too, so a refactor that breaks the claim is caught.
func TestStripeAdvertisesItsCapabilities(t *testing.T) {
	var g contracts.PaymentGateway = NewStripe(StripeConfig{SecretKey: "sk_test"})
	if _, ok := g.(contracts.SubscriptionGateway); !ok {
		t.Error("Stripe should support subscriptions")
	}
	if _, ok := g.(contracts.SubscriptionSwapper); !ok {
		t.Error("Stripe should support plan swaps")
	}
	if _, ok := g.(contracts.SubscriptionPauser); !ok {
		t.Error("Stripe should support pausing")
	}
	if _, ok := g.(contracts.RefundGateway); !ok {
		t.Error("Stripe should support refunds")
	}
	if _, ok := g.(contracts.CustomerGateway); !ok {
		t.Error("Stripe should support customers")
	}
}

// Stripe expects form-encoded requests; a subscription must send the customer,
// the price, and the SCA-friendly payment behaviour, and map the reply onto
// the canonical shape.
func TestStripe_CreateSubscription(t *testing.T) {
	var gotBody string
	var gotIdem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := readBody(r)
		gotBody = b
		gotIdem = r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"sub_1","status":"active","customer":"cus_1",
			"current_period_end":1893456000,
			"items":{"data":[{"id":"si_1","price":{"id":"price_1"}}]}
		}`))
	}))
	defer srv.Close()

	g := NewStripe(StripeConfig{SecretKey: "sk_test"})
	g.baseURL = srv.URL

	sub, err := g.CreateSubscription(context.Background(), contracts.SubscriptionParams{
		PlanID: "price_1", CustomerID: "cus_1", Subject: "user-9", Idempotency: "idem-1",
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if sub.ID != "sub_1" || sub.Status != contracts.SubActive {
		t.Errorf("mapped subscription wrong: %+v", sub)
	}
	if sub.PlanID != "price_1" {
		t.Errorf("plan id = %q", sub.PlanID)
	}
	if sub.Subject != "user-9" {
		t.Errorf("subject not carried through: %q", sub.Subject)
	}
	if sub.CurrentPeriodEnd == nil {
		t.Error("current period end not parsed")
	}
	if !strings.Contains(gotBody, "customer=cus_1") || !strings.Contains(gotBody, "items%5B0%5D%5Bprice%5D=price_1") {
		t.Errorf("request body missing fields: %s", gotBody)
	}
	if !strings.Contains(gotBody, "payment_behavior=default_incomplete") {
		t.Errorf("SCA payment behaviour not requested: %s", gotBody)
	}
	if gotIdem != "idem-1" {
		t.Errorf("idempotency key not sent: %q", gotIdem)
	}
}

// A retried create must carry the same idempotency key so the provider dedupes
// rather than creating a second subscription.
func TestStripe_RefundSendsIdempotencyKey(t *testing.T) {
	var gotIdem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIdem = r.Header.Get("Idempotency-Key")
		_, _ = w.Write([]byte(`{"id":"re_1","amount":500,"currency":"usd","status":"succeeded"}`))
	}))
	defer srv.Close()

	g := NewStripe(StripeConfig{SecretKey: "sk_test"})
	g.baseURL = srv.URL

	ref, err := g.Refund(context.Background(), contracts.RefundParams{PaymentID: "pi_1", Amount: 500, Idempotency: "r-idem"})
	if err != nil {
		t.Fatalf("Refund: %v", err)
	}
	if ref.Status != contracts.RefundSucceeded || ref.ID != "re_1" {
		t.Errorf("refund mapped wrong: %+v", ref)
	}
	if gotIdem != "r-idem" {
		t.Errorf("refund idempotency key not sent: %q", gotIdem)
	}
}

// Cancelling at period end must schedule, not delete: a customer who paid keeps
// access until the period runs out.
func TestStripe_CancelAtPeriodEndSchedules(t *testing.T) {
	var method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		b, _ := readBody(r)
		if !strings.Contains(b, "cancel_at_period_end=true") {
			t.Errorf("did not schedule the cancel: %s", b)
		}
		_, _ = w.Write([]byte(`{"id":"sub_1","status":"active","cancel_at_period_end":true,"current_period_end":1893456000,"items":{"data":[{"id":"si_1","price":{"id":"price_1"}}]}}`))
	}))
	defer srv.Close()

	g := NewStripe(StripeConfig{SecretKey: "sk_test"})
	g.baseURL = srv.URL
	sub, err := g.CancelSubscription(context.Background(), "sub_1", contracts.CancelParams{AtPeriodEnd: true})
	if err != nil {
		t.Fatalf("CancelSubscription: %v", err)
	}
	if method != http.MethodPost {
		t.Errorf("period-end cancel used %s, want POST (a delete would revoke immediately)", method)
	}
	if sub.CancelAt == nil {
		t.Error("scheduled cancel not reflected on the subscription")
	}
}

// Razorpay's e-mandate flow returns an authorisation URL and a pending status;
// that URL is how the customer authorises the mandate, so it must survive the
// mapping.
func TestRazorpay_CreateSubscriptionCarriesAuthURL(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"id":"sub_rzp","status":"created","plan_id":"plan_1","short_url":"https://rzp.io/i/abc","customer_id":"cust_1"}`))
	}))
	defer srv.Close()

	g := NewRazorpay(RazorpayConfig{KeyID: "k", KeySecret: "s"})
	g.baseURL = srv.URL

	sub, err := g.CreateSubscription(context.Background(), contracts.SubscriptionParams{PlanID: "plan_1", Subject: "u1"})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if sub.AuthURL != "https://rzp.io/i/abc" {
		t.Errorf("mandate auth URL lost: %q", sub.AuthURL)
	}
	if sub.Status != contracts.SubPending {
		t.Errorf("a created-but-unauthorised subscription should be pending, got %q", sub.Status)
	}
	if body["total_count"] == nil {
		t.Error("Razorpay requires total_count; it was not sent")
	}
}

// Razorpay has no pause/resume or plan swap, and must not claim them.
func TestRazorpayCapabilitiesAreHonest(t *testing.T) {
	var g contracts.PaymentGateway = NewRazorpay(RazorpayConfig{KeyID: "k", KeySecret: "s"})
	if _, ok := g.(contracts.SubscriptionGateway); !ok {
		t.Error("Razorpay should support subscriptions")
	}
	if _, ok := g.(contracts.SubscriptionPauser); ok {
		t.Error("Razorpay must not claim pausing it does not implement")
	}
	if _, ok := g.(contracts.RefundGateway); !ok {
		t.Error("Razorpay should support refunds")
	}
}

func readBody(r *http.Request) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			if err.Error() == "EOF" {
				return sb.String(), nil
			}
			return sb.String(), nil
		}
	}
}
