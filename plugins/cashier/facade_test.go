package cashier

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
)

// localFakeVerifier is a no-crypto IAP verifier for facade tests.
type localFakeVerifier struct{}

func (localFakeVerifier) Platform() contracts.IAPPlatform { return contracts.PlatformApple }
func (localFakeVerifier) VerifyReceipt(_ context.Context, p contracts.ReceiptParams) (*contracts.IAPEntitlement, error) {
	exp := time.Now().Add(time.Hour)
	return &contracts.IAPEntitlement{
		Platform: contracts.PlatformApple, ProductID: p.ProductID, Subject: p.Subject,
		OriginalTransactionID: "otx-" + p.Subject, Subscription: true, Active: true,
		ExpiresAt: &exp, Environment: "production",
	}, nil
}
func (localFakeVerifier) ParseNotification([]byte) (*contracts.StoreNotification, error) {
	return nil, nil
}

func iapManagerForTest() *IAPManager {
	return NewIAPManager().Register(localFakeVerifier{})
}

// fullGateway implements every capability, for testing dispatch and mirroring.
type fullGateway struct {
	stubGateway
	created *contracts.Subscription
}

func (g *fullGateway) CreateSubscription(_ context.Context, p contracts.SubscriptionParams) (*contracts.Subscription, error) {
	return &contracts.Subscription{Gateway: g.name, ID: "sub_1", Status: contracts.SubActive, PlanID: p.PlanID, Subject: p.Subject}, nil
}
func (g *fullGateway) GetSubscription(_ context.Context, id string) (*contracts.Subscription, error) {
	return &contracts.Subscription{Gateway: g.name, ID: id, Status: contracts.SubActive}, nil
}
func (g *fullGateway) CancelSubscription(_ context.Context, id string, _ contracts.CancelParams) (*contracts.Subscription, error) {
	return &contracts.Subscription{Gateway: g.name, ID: id, Status: contracts.SubCanceled}, nil
}
func (g *fullGateway) Refund(_ context.Context, p contracts.RefundParams) (*contracts.Refund, error) {
	return &contracts.Refund{Gateway: g.name, ID: "re_1", PaymentID: p.PaymentID, Status: contracts.RefundSucceeded}, nil
}

// A gateway without a capability must produce a clear, named error — not a
// panic from a bad type assertion, and not a silent no-op.
func TestFacadeReportsUnsupportedCapabilities(t *testing.T) {
	c := &Cashier{Gateways: NewGatewayManager()}
	c.Gateways.Register(stubGateway{"payu"}) // charge-only

	_, err := c.Subscribe(context.Background(), "payu", SubscriptionParams{PlanID: "p"})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("expected ErrUnsupported, got %v", err)
	}
	if err == nil || !contains(err.Error(), "payu") || !contains(err.Error(), "subscriptions") {
		t.Errorf("error should name the gateway and capability: %v", err)
	}

	if _, err := c.Refund(context.Background(), "payu", RefundParams{PaymentID: "x"}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("refund on a charge-only gateway should be unsupported, got %v", err)
	}
}

// A supported capability dispatches to the gateway and mirrors the result.
func TestSubscribeMirrorsLocally(t *testing.T) {
	store := NewMemorySubscriptionStore()
	c := &Cashier{Gateways: NewGatewayManager(), Subscriptions: store}
	c.Gateways.Register(&fullGateway{stubGateway: stubGateway{"stripe"}})

	sub, err := c.Subscribe(context.Background(), "stripe", SubscriptionParams{PlanID: "price_1", Subject: "user-7"})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if sub.ID != "sub_1" {
		t.Fatalf("unexpected subscription: %+v", sub)
	}

	// The mirror must now answer an access check without touching the gateway.
	got, ok := store.Get("stripe", "sub_1")
	if !ok {
		t.Fatal("subscription was not mirrored locally")
	}
	if got.Subject != "user-7" {
		t.Errorf("mirror lost the subject: %+v", got)
	}
	if !c.SubscribedTo("user-7", "price_1") {
		t.Error("SubscribedTo did not see the mirrored subscription")
	}
	if c.SubscribedTo("user-7", "some-other-plan") {
		t.Error("SubscribedTo matched a plan the user does not have")
	}
}

// Capabilities is what a UI reads to avoid offering what a gateway cannot do.
func TestCapabilitiesReport(t *testing.T) {
	c := &Cashier{Gateways: NewGatewayManager()}
	c.Gateways.Register(&fullGateway{stubGateway: stubGateway{"stripe"}})
	c.Gateways.Register(stubGateway{"payu"})

	full, _ := c.Capabilities("stripe")
	if !full.Subscriptions || !full.Refunds {
		t.Errorf("full gateway under-reported: %+v", full)
	}
	bare, _ := c.Capabilities("payu")
	if bare.Subscriptions || bare.Refunds || bare.Customers {
		t.Errorf("charge-only gateway over-reported: %+v", bare)
	}
}

// A cancelled subscription still inside its paid period keeps access; revoking
// on the cancel event rather than the period end short-changes the customer.
func TestCancelledButPaidThroughStillGrantsAccess(t *testing.T) {
	store := NewMemorySubscriptionStore()
	future := time.Now().Add(48 * time.Hour)
	_ = store.Upsert(&contracts.Subscription{
		Gateway: "stripe", ID: "sub_2", Subject: "u",
		Status: contracts.SubCanceled, PlanID: "pro", CurrentPeriodEnd: &future,
	})
	c := &Cashier{Gateways: NewGatewayManager(), Subscriptions: store}
	if !c.SubscribedTo("u", "pro") {
		t.Error("a cancelled subscription still within its paid period should grant access")
	}

	past := time.Now().Add(-1 * time.Hour)
	_ = store.Upsert(&contracts.Subscription{
		Gateway: "stripe", ID: "sub_2", Subject: "u",
		Status: contracts.SubCanceled, PlanID: "pro", CurrentPeriodEnd: &past,
	})
	if c.SubscribedTo("u", "pro") {
		t.Error("a cancelled subscription past its period must not grant access")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// An active IAP subscription, once verified, must grant access through the same
// SubscribedTo check a card subscription uses — the app should not care which
// rail paid.
func TestVerifiedPurchaseGrantsAccess(t *testing.T) {
	store := NewMemorySubscriptionStore()
	// Unmetered manager is fine for a unit test: the metering gate has its own
	// coverage in the iap package.
	mgr := iapManagerForTest()
	c := &Cashier{Gateways: NewGatewayManager(), Subscriptions: store, IAP: mgr}

	ent, err := c.VerifyPurchase(context.Background(), ReceiptParams{
		Platform: PlatformApple, ProductID: "pro.monthly", Subject: "u-42", Token: "tok",
	})
	if err != nil {
		t.Fatalf("VerifyPurchase: %v", err)
	}
	if !ent.Active {
		t.Fatalf("entitlement not active: %+v", ent)
	}
	if !c.SubscribedTo("u-42", "pro.monthly") {
		t.Error("a verified IAP subscription did not grant access")
	}
}
