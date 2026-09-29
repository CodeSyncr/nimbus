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

	// RenewalInfo is Apple's optional signed renewal info JWS (StoreKit 2's
	// Product.SubscriptionInfo.RenewalInfo). With it the verifier knows
	// whether the subscription will renew and whether it is in grace.
	RenewalInfo string
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

	// PeriodType is "trial", "intro" or "normal" where the store says which
	// kind of period this transaction pays for ("" when it does not say).
	PeriodType string
	// PurchasedAt is when this transaction (this renewal) was bought.
	PurchasedAt *time.Time
	// Revoked is set when the store took the purchase back (a refund, a
	// family-sharing removal). A revoked purchase never grants access.
	Revoked bool
	// BillingIssue is set while the store is retrying a failed renewal. With
	// GraceExpiresAt in the future the user keeps access through the store's
	// own grace period; without it, access has already lapsed (billing retry
	// or Google's account hold).
	BillingIssue   bool
	GraceExpiresAt *time.Time

	// Token is Google's purchase token: the handle every later API call and
	// notification uses. LinkedToken is the token this purchase replaced
	// (an upgrade, a downgrade, a resubscribe), so the replaced row can be
	// retired instead of expiring as if the user had left. Apple leaves both
	// empty.
	Token       string
	LinkedToken string
	// Acknowledged is Google's acknowledgement state. Google refunds any
	// purchase that is not acknowledged within three days.
	Acknowledged bool
	// AppAccountToken is the UUID an iOS app attached to the purchase
	// (StoreKit 2's appAccountToken); it names the buyer's account.
	AppAccountToken string

	Raw map[string]any
}

// StoreNotification is a verified server-to-server notification from a store
// (Apple App Store Server Notifications V2, Google Real-time Developer
// Notifications), reduced to a canonical shape.
type StoreNotification struct {
	Platform IAPPlatform
	// Type is canonical: "purchased" | "renewed" | "canceled" | "uncanceled" |
	// "expired" | "refunded" | "grace_period" | "billing_issue" |
	// "product_change" | "paused" | "recovered" | "test", or provider-specific.
	Type string
	// Subtype is the store's own type and subtype, for logs
	// ("DID_CHANGE_RENEWAL_STATUS/AUTO_RENEW_DISABLED", "google_3").
	Subtype               string
	ProductID             string
	OriginalTransactionID string
	TransactionID         string
	ExpiresAt             *time.Time
	// ID is the store's delivery id (Apple's notificationUUID, Pub/Sub's
	// messageId); a store retries deliveries, so handlers dedupe on it.
	ID string
	// Environment is "production" or "sandbox".
	Environment string
	// Token is Google's purchase token (empty for Apple).
	Token string
	// Entitlement is the purchase state the notification itself vouches for.
	// Apple signs the latest transaction and renewal info into every
	// notification, so it is set there; Google's notifications carry only a
	// token, so it is nil and the handler re-verifies against the API.
	Entitlement *IAPEntitlement
	Raw         []byte
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

// IAPAcknowledger is implemented by verifiers whose store needs the server to
// confirm a purchase (Google refunds unacknowledged purchases after three
// days).
type IAPAcknowledger interface {
	Acknowledge(ctx context.Context, productID, token string, subscription bool) error
}
