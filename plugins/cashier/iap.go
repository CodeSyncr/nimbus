package cashier

import (
	"context"
	"fmt"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
	"github.com/CodeSyncr/nimbus/plugins/cashier/iap"
)

// Re-exported IAP types.
type (
	IAPPlatform       = contracts.IAPPlatform
	ReceiptParams     = contracts.ReceiptParams
	IAPEntitlement    = contracts.IAPEntitlement
	StoreNotification = contracts.StoreNotification
	IAPVerifier       = contracts.IAPVerifier
)

const (
	PlatformApple  = contracts.PlatformApple
	PlatformGoogle = contracts.PlatformGoogle
)

// IAPManager holds the registered store verifiers and routes a request to the
// right one. Apple and Google are verified through entirely different APIs, so
// each platform registers its own verifier and this picks by platform.
//
// Verification is a paid Nimbus Cloud feature, so a manager carries a meter and
// every registered verifier is wrapped by it (see iap.Metered). Constructing a
// manager with NewIAPManager, which has no meter, is for development and tests
// only; production uses NewMeteredIAPManager with a Nimbus Cloud meter.
type IAPManager struct {
	verifiers map[contracts.IAPPlatform]contracts.IAPVerifier
	meter     Meter
}

// Meter and its transaction/decision types, re-exported from the iap package.
type (
	Meter              = iap.Meter
	MeteredTransaction = iap.MeteredTransaction
	Decision           = iap.Decision
)

// NewIAPManager builds an unmetered manager. Verifiers registered on it are NOT
// gated — use it only for development and tests. Production must use
// NewMeteredIAPManager so transactions are metered and billed.
func NewIAPManager() *IAPManager {
	return &IAPManager{verifiers: map[contracts.IAPPlatform]contracts.IAPVerifier{}}
}

// NewMeteredIAPManager builds a manager whose verifiers are all gated by the
// meter. This is the supported production path: without a meter, in-app
// purchases are not a paid feature.
func NewMeteredIAPManager(m Meter) *IAPManager {
	return &IAPManager{verifiers: map[contracts.IAPPlatform]contracts.IAPVerifier{}, meter: m}
}

// Register adds a platform verifier, wrapping it in the manager's meter when
// one is configured so its entitlements are gated.
func (m *IAPManager) Register(v contracts.IAPVerifier) *IAPManager {
	if v == nil {
		return m
	}
	if m.meter != nil {
		v = iap.Metered(v, m.meter)
	}
	m.verifiers[v.Platform()] = v
	return m
}

// MeteredFromCloud builds a production IAP manager gated by a Nimbus Cloud
// meter keyed on the given API key. This is the one-liner a developer uses to
// turn Apple/Google verification into the paid feature: register Apple/Google
// verifiers on the returned manager and every entitlement is metered.
func MeteredFromCloud(apiKey string) (*IAPManager, error) {
	m, err := iap.NewCloudMeter(iap.CloudMeterConfig{APIKey: apiKey})
	if err != nil {
		return nil, err
	}
	return NewMeteredIAPManager(m), nil
}

// Verifier returns the verifier for a platform.
func (m *IAPManager) Verifier(p contracts.IAPPlatform) (contracts.IAPVerifier, error) {
	v, ok := m.verifiers[p]
	if !ok {
		return nil, fmt.Errorf("cashier/iap: no verifier registered for %q", p)
	}
	return v, nil
}

// Verify checks a client-supplied purchase against its store and returns the
// entitlement the store actually vouches for.
//
// The whole point of server-side verification is that the client is not
// trusted: the app hands over a token, and only the store's signed answer
// decides what the user owns. A caller must gate access on the returned
// entitlement, never on the app's own claim.
func (m *IAPManager) Verify(ctx context.Context, p ReceiptParams) (*IAPEntitlement, error) {
	v, err := m.Verifier(p.Platform)
	if err != nil {
		return nil, err
	}
	return v.VerifyReceipt(ctx, p)
}

// Notification verifies and decodes a server-to-server store notification.
func (m *IAPManager) Notification(platform IAPPlatform, payload []byte) (*StoreNotification, error) {
	v, err := m.Verifier(platform)
	if err != nil {
		return nil, err
	}
	return v.ParseNotification(payload)
}

// VerifyPurchase verifies an in-app purchase through the metered manager and,
// when a subscription store is configured, mirrors the entitlement so an access
// check treats an IAP subscription exactly like a card one.
//
// This is the method an app's receipt-validation endpoint calls: hand it the
// platform, product and token the client reported, and it returns only what
// Nimbus Cloud both cryptographically verified and metered.
func (c *Cashier) VerifyPurchase(ctx context.Context, p ReceiptParams) (*IAPEntitlement, error) {
	if c.IAP == nil {
		return nil, fmt.Errorf("cashier: in-app purchases are not configured (set Config.IAP)")
	}
	ent, err := c.IAP.Verify(ctx, p)
	if err != nil {
		return nil, err
	}
	c.mirrorPurchase(ent)
	return ent, nil
}

// mirrorPurchase records a verified IAP subscription in the subscription store,
// keyed by its original transaction id so every renewal updates one row.
func (c *Cashier) mirrorPurchase(ent *IAPEntitlement) {
	if c.Subscriptions == nil || ent == nil || !ent.Subscription {
		return
	}
	status := contracts.SubActive
	if !ent.Active {
		status = contracts.SubExpired
	}
	_ = c.Subscriptions.Upsert(&contracts.Subscription{
		Gateway:          string(ent.Platform),
		ID:               ent.OriginalTransactionID,
		Status:           status,
		PlanID:           ent.ProductID,
		Subject:          ent.Subject,
		CurrentPeriodEnd: ent.ExpiresAt,
	})
}
