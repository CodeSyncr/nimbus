package contracts

import (
	"context"
	"time"
)

/*
Capability interfaces.

The core PaymentGateway is deliberately small — four methods every provider can
honour. Everything beyond a one-off charge is a capability a gateway may or may
not have: Razorpay and Stripe run full subscription lifecycles, a redirect-only
gateway like CCAvenue barely does refunds, and an in-app-purchase "gateway"
issues no charges at all and only verifies receipts.

Modelling those as separate interfaces rather than as more methods on
PaymentGateway is what keeps the set open. A gateway implements the capabilities
it has; the manager checks for them at the call site and returns ErrUnsupported
otherwise. Adding Stripe subscriptions does not force PayU to grow a
CreateSubscription that can only error, and adding a new gateway never has to
stub methods it cannot mean.
*/

// SubscriptionStatus is the canonical local view of a subscription, mapped from
// each gateway's own vocabulary so access checks read the same everywhere.
type SubscriptionStatus string

const (
	SubActive   SubscriptionStatus = "active"
	SubTrialing SubscriptionStatus = "trialing"
	SubPastDue  SubscriptionStatus = "past_due"
	SubPaused   SubscriptionStatus = "paused"
	SubCanceled SubscriptionStatus = "canceled"
	SubExpired  SubscriptionStatus = "expired"
	SubPending  SubscriptionStatus = "pending" // created, awaiting first payment/authentication
	SubUnknown  SubscriptionStatus = "unknown"
)

// Grants reports whether a status currently entitles the subject to access.
func (s SubscriptionStatus) Grants() bool {
	return s == SubActive || s == SubTrialing
}

// SubscriptionParams is a gateway-agnostic request to start a subscription.
//
// Gateways use different subsets, the same way ChargeParams does: Stripe needs
// a PriceID, Razorpay a PlanID and often a pre-created customer, and both accept
// a trial and metadata.
type SubscriptionParams struct {
	PlanID     string // Stripe price_… / Razorpay plan_… / provider plan ref
	CustomerID string // gateway customer id, when the gateway needs one
	Subject    string // your user id, mirrored onto the local record

	Quantity    int64
	TrialDays   int
	TrialEnd    *time.Time
	CouponCode  string
	Metadata    map[string]string
	Idempotency string // caller-supplied key; gateways that support it dedupe on it

	// TotalCycles bounds a fixed-length subscription (0 = until cancelled).
	TotalCycles int
	// NotifyURL / ReturnURL for gateways that authenticate the mandate via a
	// redirect (Indian e-mandate flows).
	NotifyURL string
	ReturnURL string
}

// Subscription is the gateway's subscription reduced to what Nimbus mirrors.
type Subscription struct {
	Gateway    string
	ID         string             // gateway subscription id
	Status     SubscriptionStatus // canonical
	PlanID     string
	CustomerID string
	Subject    string

	CurrentPeriodEnd *time.Time
	TrialEnd         *time.Time
	CancelAt         *time.Time // set when scheduled to cancel at period end
	CanceledAt       *time.Time
	EndedAt          *time.Time

	// AuthURL is a redirect the customer must complete to authorise the mandate
	// (e-mandate / SCA). Empty when the subscription is active immediately.
	AuthURL string

	Raw map[string]any
}

// CancelParams tunes how a subscription ends.
type CancelParams struct {
	// AtPeriodEnd keeps access until the paid period runs out rather than
	// revoking immediately — the usual, humane default for a cancellation.
	AtPeriodEnd bool
	// Comment is recorded with the gateway where supported.
	Comment string
}

// SwapParams changes a subscription's plan.
type SwapParams struct {
	NewPlanID string
	Quantity  int64
	// Prorate bills or credits the difference immediately. Off defers the new
	// price to the next cycle.
	Prorate bool
}

// SubscriptionGateway is implemented by gateways that run recurring billing.
type SubscriptionGateway interface {
	CreateSubscription(ctx context.Context, p SubscriptionParams) (*Subscription, error)
	GetSubscription(ctx context.Context, id string) (*Subscription, error)
	CancelSubscription(ctx context.Context, id string, p CancelParams) (*Subscription, error)
}

// SubscriptionSwapper is the optional slice of subscription support for
// changing plans mid-term; not every gateway allows it.
type SubscriptionSwapper interface {
	SwapSubscription(ctx context.Context, id string, p SwapParams) (*Subscription, error)
}

// SubscriptionPauser is optional pause/resume support (Stripe, Razorpay).
type SubscriptionPauser interface {
	PauseSubscription(ctx context.Context, id string) (*Subscription, error)
	ResumeSubscription(ctx context.Context, id string) (*Subscription, error)
}
