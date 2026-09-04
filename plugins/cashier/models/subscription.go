package models

import (
	"time"

	"github.com/CodeSyncr/nimbus/database"
)

// Subscription mirrors a gateway subscription locally for access checks.
//
// It is a mirror, not the source of truth: the gateway owns the billing, and
// this row is updated from webhooks and verification so an access check is one
// indexed local read rather than a call out to the provider on every request.
type Subscription struct {
	database.Model
	Gateway   string `json:"gateway" gorm:"index"`
	GatewayID string `json:"gateway_id" gorm:"uniqueIndex:idx_sub_gateway_id"` // subscription id
	Subject   string `json:"subject" gorm:"index"`                             // user id
	Plan      string `json:"plan"`
	PriceID   string `json:"price_id"`
	Status    string `json:"status" gorm:"index"` // canonical SubscriptionStatus

	CustomerID       string     `json:"customer_id" gorm:"index"`
	CurrentPeriodEnd *time.Time `json:"current_period_end"`
	TrialEnd         *time.Time `json:"trial_end"`
	CancelAt         *time.Time `json:"cancel_at"` // scheduled cancel at period end
	EndsAt           *time.Time `json:"ends_at"`   // access boundary
	CanceledAt       *time.Time `json:"canceled_at"`

	// RevenueCat-style entitlement context (see cashier.Entitlement).
	PeriodType string `json:"period_type"` // trial|intro|normal|promotional|grace
	Source     string `json:"source"`      // purchase|promotional
	WillRenew  bool   `json:"will_renew"`
}

// TableName is the DB table for subscriptions.
func (Subscription) TableName() string { return "cashier_subscriptions" }

// Active reports whether the subscription currently grants access.
//
// A subscription cancelled but paid through the end of the period still grants
// access until EndsAt: cancelling is not the same as losing access, and
// revoking on the cancel event rather than the period boundary is a common way
// to short-change a paying customer.
func (s Subscription) Active() bool {
	switch s.Status {
	case "active", "trialing":
	case "canceled":
		// Cancelled but still within the paid period.
		return s.EndsAt != nil && time.Now().Before(*s.EndsAt)
	default:
		return false
	}
	if s.EndsAt != nil && time.Now().After(*s.EndsAt) {
		return false
	}
	return true
}
