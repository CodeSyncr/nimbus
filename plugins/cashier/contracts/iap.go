package contracts

import (
	"context"
	"time"
)

/*
In-app purchases.

Apple's App Store and Google Play are gateways only in the loosest sense: the
store owns the transaction end to end, takes the money, runs the renewals, and
hands the app a signed receipt. Nimbus never creates a charge and never cancels
a subscription here — it can only verify what the store asserts and mirror the
resulting entitlement. That is a fundamentally different contract from a card
gateway, so it is its own interface rather than a strained fit onto
PaymentGateway.

The security-critical rule for both stores: trust the server-to-server signed
payload, not the client. A receipt string or purchase token from the app is
only a pointer; the entitlement is whatever Apple's signed JWS or Google's
Android Publisher API says it is when the server checks.
*/

// IAPPlatform distinguishes the two stores.
type IAPPlatform string

const (
	PlatformApple  IAPPlatform = "apple"
	PlatformGoogle IAPPlatform = "google"
)

// ReceiptParams is a client-supplied pointer to a purchase, to be verified
// server-side against the store.
type ReceiptParams struct {
	Platform IAPPlatform
	// ProductID is the store product the app reports buying; verification must
	// confirm the signed payload names the same product.
	ProductID string
	Subject   string // your user id

	// Apple: the JWS transaction from StoreKit 2 (or a legacy base64 receipt).
	// Google: the purchase token returned by the Play Billing library.
	Token string

	// Google needs the product kind to pick the right API; Apple does not.
	Subscription bool
}

// Entitlement is the verified result of a receipt check: what the store
// confirms the user owns, and until when for a subscription.
type IAPEntitlement struct {
	Platform      IAPPlatform
	ProductID     string
	Subject       string
	TransactionID string
	// OriginalTransactionID ties every renewal of one subscription together;
	// it is the stable key to mirror against, not the per-renewal id.
	OriginalTransactionID string

	Subscription bool
	Active       bool
	ExpiresAt    *time.Time

	// PriceMicros is the transaction value in millionths of a currency unit
	// (1_000_000 = one dollar), and Currency its ISO code. Metering bills on
	// this, so a store that does not report a price yields zero and is treated
	// as non-billable rather than guessed at.
	PriceMicros int64
	Currency    string
	// AutoRenewing is the store's current intent; a subscription can be active
	// now yet set not to renew.
	AutoRenewing bool

	// Environment is "production" or "sandbox"; a sandbox receipt reaching a
	// production server is the classic test-purchase-as-real bug.
	Environment string

	Raw map[string]any
}

// StoreNotification is a verified server-to-server notification from a store
// (Apple App Store Server Notifications V2, Google Real-time Developer
// Notifications), reduced to a canonical shape.
type StoreNotification struct {
	Platform              IAPPlatform
	Type                  string // canonical: "renewed" | "canceled" | "expired" | "refunded" | "grace_period" | provider-specific
	ProductID             string
	OriginalTransactionID string
	ExpiresAt             *time.Time
	Raw                   []byte
}

// IAPVerifier verifies purchases and store notifications for one platform.
type IAPVerifier interface {
	Platform() IAPPlatform
	// VerifyReceipt checks a client-supplied purchase against the store and
	// returns the entitlement the store actually vouches for.
	VerifyReceipt(ctx context.Context, p ReceiptParams) (*IAPEntitlement, error)
	// ParseNotification verifies and decodes a server-to-server notification.
	ParseNotification(payload []byte) (*StoreNotification, error)
}
