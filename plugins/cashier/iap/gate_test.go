package iap

import (
	"context"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
)

// fakeVerifier returns a fixed entitlement, so the metering gate can be tested
// without real store crypto.
type fakeVerifier struct {
	platform contracts.IAPPlatform
	ent      *contracts.IAPEntitlement
}

func (f fakeVerifier) Platform() contracts.IAPPlatform { return f.platform }
func (f fakeVerifier) VerifyReceipt(context.Context, contracts.ReceiptParams) (*contracts.IAPEntitlement, error) {
	return f.ent, nil
}
func (f fakeVerifier) ParseNotification([]byte) (*contracts.StoreNotification, error) {
	return nil, nil
}

// denyMeter always denies, standing in for an unpaid Nimbus Cloud account.
type denyMeter struct{}

func (denyMeter) Authorize(context.Context, MeteredTransaction) (Decision, error) {
	return Decision{Allowed: false, Reason: "account not entitled"}, nil
}

func activeEnt() *contracts.IAPEntitlement {
	exp := time.Now().Add(time.Hour)
	return &contracts.IAPEntitlement{
		Platform: contracts.PlatformApple, ProductID: "pro", Subject: "u1",
		TransactionID: "t1", Subscription: true, Active: true,
		ExpiresAt: &exp, PriceMicros: dollars(10), Currency: "USD", Environment: "production",
	}
}

// The gate's reason for existing: when Nimbus Cloud denies the account, the
// entitlement is NOT returned, however valid the receipt. That is what makes it
// a paid feature rather than a free library.
func TestMeteredVerifierBlocksWhenDenied(t *testing.T) {
	v := Metered(fakeVerifier{platform: contracts.PlatformApple, ent: activeEnt()}, denyMeter{})
	_, err := v.VerifyReceipt(context.Background(), contracts.ReceiptParams{Platform: contracts.PlatformApple})
	if err == nil {
		t.Fatal("a denied account still received an entitlement")
	}
}

// When the meter allows, the entitlement passes through untouched.
func TestMeteredVerifierPassesWhenAllowed(t *testing.T) {
	v := Metered(fakeVerifier{platform: contracts.PlatformApple, ent: activeEnt()}, NewLocalMeter())
	ent, err := v.VerifyReceipt(context.Background(), contracts.ReceiptParams{Platform: contracts.PlatformApple})
	if err != nil {
		t.Fatalf("VerifyReceipt: %v", err)
	}
	if ent == nil || !ent.Active {
		t.Errorf("entitlement not returned: %+v", ent)
	}
}

// An inactive (lapsed/refunded) receipt is verified but never metered — there
// is no revenue to bill, and it must not consume the free tier.
func TestInactiveEntitlementIsNotMetered(t *testing.T) {
	ent := activeEnt()
	ent.Active = false
	meter := NewLocalMeter()
	v := Metered(fakeVerifier{platform: contracts.PlatformApple, ent: ent}, meter)

	if _, err := v.VerifyReceipt(context.Background(), contracts.ReceiptParams{Platform: contracts.PlatformApple}); err != nil {
		t.Fatalf("VerifyReceipt: %v", err)
	}
	if meter.TrackedVolume() != 0 {
		t.Errorf("an inactive receipt was metered: %d", meter.TrackedVolume())
	}
}
