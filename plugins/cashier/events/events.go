// Package events defines canonical, gateway-agnostic payment events and maps
// each gateway's raw webhook types onto them — so paywall logic (grant/revoke)
// is written once regardless of which gateway delivered the event.
package events

import "github.com/CodeSyncr/nimbus/plugins/cashier/contracts"

// Canonical event names.
const (
	PaymentSucceeded      = "payment.succeeded"
	PaymentFailed         = "payment.failed"
	SubscriptionActivated = "subscription.activated"
	SubscriptionCancelled = "subscription.cancelled"
	Unknown               = "unknown"
)

// Normalize maps a verified gateway webhook onto a canonical event.
//
//	switch events.Normalize(evt) {
//	case events.PaymentSucceeded:      paywall.Grant(subject, plan, expiry)
//	case events.SubscriptionCancelled: paywall.Revoke(subject, plan)
//	}
func Normalize(e contracts.WebhookEvent) string {
	switch e.Gateway {
	case "stripe":
		switch e.Type {
		case "checkout.session.completed", "invoice.payment_succeeded":
			return PaymentSucceeded
		case "invoice.payment_failed":
			return PaymentFailed
		case "customer.subscription.created", "customer.subscription.updated":
			return SubscriptionActivated
		case "customer.subscription.deleted":
			return SubscriptionCancelled
		}
	case "razorpay":
		switch e.Type {
		case "payment.captured", "order.paid":
			return PaymentSucceeded
		case "payment.failed":
			return PaymentFailed
		case "subscription.activated", "subscription.charged", "subscription.authenticated":
			return SubscriptionActivated
		case "subscription.cancelled", "subscription.halted", "subscription.completed":
			return SubscriptionCancelled
		}
	case "payu":
		switch e.Type {
		case "success", "captured":
			return PaymentSucceeded
		case "failure", "failed":
			return PaymentFailed
		}
	}
	return Unknown
}
