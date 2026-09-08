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
	// Products seeds the catalogue mapping purchasable products onto the
	// entitlements they unlock (RevenueCat's Products → Entitlements).
	Products []Product
	// Offerings seeds the paywall offerings; CurrentOffering picks the one
	// paywalls present (default: the first registered).
	Offerings       []Offering
	CurrentOffering string
	// GracePeriod is how long a billing issue keeps entitlements alive while
	// the gateway retries the charge. 0 → DefaultGracePeriod (72h).
	GracePeriod time.Duration
	// OnSubscriberEvent receives every canonical lifecycle event
	// (initial_purchase, renewal, cancellation, billing_issue, …) for
	// analytics, email, or your own event bus.
	OnSubscriberEvent func(SubscriberEvent)
}
