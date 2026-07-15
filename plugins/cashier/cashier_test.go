package cashier

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
)

// stubGateway is a no-network gateway for manager tests.
type stubGateway struct{ name string }

func (g stubGateway) Name() string { return g.name }
func (g stubGateway) CreateCharge(context.Context, contracts.ChargeParams) (*contracts.Charge, error) {
	return &contracts.Charge{Gateway: g.name, ID: "ch_" + g.name}, nil
}
func (g stubGateway) VerifyPayment(context.Context, contracts.PaymentProof) (bool, error) {
	return true, nil
}
func (g stubGateway) VerifyWebhook([]byte, http.Header) (*contracts.WebhookEvent, error) {
	return nil, nil
}

func TestGatewayManager(t *testing.T) {
	m := NewGatewayManager()
	m.Register(stubGateway{"razorpay"})
	m.Register(stubGateway{"stripe"})

	// First registered is the default until SetDefault.
	if m.DefaultName() != "razorpay" {
		t.Fatalf("default: %s", m.DefaultName())
	}
	m.SetDefault("stripe")
	if m.DefaultName() != "stripe" {
		t.Fatalf("SetDefault: %s", m.DefaultName())
	}
	// Use("") → default; Use(name) → named.
	if g, _ := m.Use(""); g.Name() != "stripe" {
		t.Fatalf("Use(\"\") = %s", g.Name())
	}
	if g, err := m.Use("razorpay"); err != nil || g.Name() != "razorpay" {
		t.Fatalf("Use(razorpay): %v", err)
	}
	if _, err := m.Use("paypal"); err == nil {
		t.Fatal("unknown gateway accepted")
	}
	if got := m.Names(); len(got) != 2 || got[0] != "razorpay" || got[1] != "stripe" {
		t.Fatalf("Names: %v", got)
	}
}

func TestCashier_Charge(t *testing.T) {
	m := NewGatewayManager()
	m.Register(stubGateway{"razorpay"})
	c := &Cashier{Gateways: m, Paywall: NewPaywall(nil)}
	ch, err := c.Charge(context.Background(), "", ChargeParams{Amount: 100})
	if err != nil || ch.ID != "ch_razorpay" {
		t.Fatalf("charge via default: %+v err=%v", ch, err)
	}
}

func TestPaywall(t *testing.T) {
	p := NewPaywall(nil)
	if p.HasAccess("u1", "pro") {
		t.Fatal("access before grant")
	}
	_ = p.Grant("u1", "pro", time.Time{}) // forever
	if !p.HasAccess("u1", "pro") {
		t.Fatal("no access after grant")
	}
	_ = p.Revoke("u1", "pro")
	if p.HasAccess("u1", "pro") {
		t.Fatal("access after revoke")
	}
	// expiry
	_ = p.Grant("u2", "pro", time.Now().Add(-time.Minute))
	if p.HasAccess("u2", "pro") {
		t.Fatal("expired entitlement granted access")
	}
	_ = p.Grant("u3", "pro", time.Now().Add(time.Hour))
	if !p.HasAccess("u3", "pro") {
		t.Fatal("future-expiry entitlement denied")
	}
}
