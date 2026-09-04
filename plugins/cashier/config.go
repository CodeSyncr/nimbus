package cashier

import "time"

// Config configures the Cashier plugin.
type Config struct {
	// Manager is the gateway registry. Nil → a new empty one is created.
	Manager *GatewayManager
	// Default gateway name. Falls back to PAYMENTS_DEFAULT_GATEWAY, then the
	// first registered gateway.
	Default string
	// FromEnv auto-registers gateways whose credentials are present in the
	// environment (Stripe: STRIPE_KEY[/STRIPE_WEBHOOK_SECRET]; Razorpay:
	// RAZORPAY_KEY_ID/RAZORPAY_KEY_SECRET[/RAZORPAY_WEBHOOK_SECRET]; PayU:
	// PAYU_MERCHANT_KEY/PAYU_MERCHANT_SALT).
	FromEnv bool
	// Paywall backs entitlement checks (nil → in-memory store).
	Paywall *Paywall
	// WebhookPrefix mounts per-gateway webhooks at "<prefix>/<gateway>/webhook".
	// Default "/payments".
	WebhookPrefix string
	// OnWebhook runs for every verified webhook (any gateway). Use
	// events.Normalize(evt) to branch on canonical events and grant/revoke
	// paywall access.
	OnWebhook func(WebhookEvent) error
	// Subscriptions mirrors gateway subscriptions locally for fast access
	// checks. Nil → no mirror (the gateway stays authoritative).
	Subscriptions SubscriptionStore
	// IAP verifies Apple/Google in-app purchases. Nil → in-app purchases are
	// not accepted.
	IAP *IAPManager
	// CloudKey is the Cashier Cloud secret key ("cshr_live_…"). It activates
	// the subscription suite — the product→entitlement catalogue, offerings,
	// the subscriber lifecycle, and CustomerInfo — and is required for
	// Apple/Google in-app purchase verification. Falls back to the
	// CASHIER_CLOUD_KEY environment variable. Without it the plugin is
	// payments-only: gateways, charges, webhooks, refunds, and the basic
	// paywall keep working; the fields below are ignored.
	CloudKey string
	// Products seeds the catalogue mapping purchasable products onto the
	// entitlements they unlock (RevenueCat's Products → Entitlements).
	// Cashier Cloud only.
	Products []Product
	// Offerings seeds the paywall offerings; CurrentOffering picks the one
	// paywalls present (default: the first registered). Cashier Cloud only.
	Offerings       []Offering
	CurrentOffering string
	// GracePeriod is how long a billing issue keeps entitlements alive while
	// the gateway retries the charge. 0 → DefaultGracePeriod (72h).
	// Cashier Cloud only.
	GracePeriod time.Duration
	// OnSubscriberEvent receives every canonical lifecycle event
	// (initial_purchase, renewal, cancellation, billing_issue, …) for
	// analytics, email, or your own event bus. Cashier Cloud only.
	OnSubscriberEvent func(SubscriberEvent)
}
