// Package contracts defines the payment-gateway contract and the shared value
// types every gateway and the manager operate on. It has no dependencies on the
// concrete gateways, so both the gateways and the root cashier package depend on
// it without cycles.
package contracts

import (
	"context"
	"errors"
	"net/http"
)

// ErrUnsupported is returned by a gateway for an operation it doesn't implement
// (e.g. VerifyPayment on a redirect-only gateway).
var ErrUnsupported = errors.New("cashier: operation not supported by this gateway")

// ChargeParams is a gateway-agnostic request to start a payment. Gateways use
// different subsets:
//   - Stripe (hosted checkout): Mode + PriceID (or Amount/Currency) + URLs → a redirect URL.
//   - Razorpay (widget): Amount + Currency + Reference → an order id the frontend opens.
type ChargeParams struct {
	Amount   int64  // smallest currency unit (paise for INR, cents for USD)
	Currency string // "INR", "USD", …

	Mode    string // "subscription" | "payment" (Stripe). Default "payment".
	PriceID string // Stripe Price (price_…) for recurring billing.

	CustomerID    string
	CustomerEmail string

	SuccessURL string // redirect (hosted-checkout) gateways
	CancelURL  string

	Reference string // your order id (Razorpay receipt / correlation)
	Metadata  map[string]string
}

// Charge is the result of creating a payment.
type Charge struct {
	Gateway     string // "stripe" | "razorpay" | …
	ID          string // Checkout session (Stripe) or order (Razorpay)
	RedirectURL string // set for hosted-checkout gateways; empty for widgets
	Amount      int64
	Currency    string
	Raw         map[string]any // decoded gateway response
}

// PaymentProof is a client-returned proof of a completed payment, verified
// server-side (primarily Razorpay: order_id + payment_id + signature).
type PaymentProof struct {
	OrderID   string
	PaymentID string
	Signature string
}

// WebhookEvent is a verified, parsed webhook.
type WebhookEvent struct {
	Gateway string
	Type    string // gateway event type, e.g. "payment.captured"
	ID      string
	Raw     []byte // raw verified body
}

// PaymentGateway is the contract every payment provider implements.
type PaymentGateway interface {
	Name() string
	CreateCharge(ctx context.Context, p ChargeParams) (*Charge, error)
	VerifyPayment(ctx context.Context, proof PaymentProof) (bool, error)
	VerifyWebhook(payload []byte, headers http.Header) (*WebhookEvent, error)
}
