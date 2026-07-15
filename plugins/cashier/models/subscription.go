package models

import (
	"time"

	"github.com/CodeSyncr/nimbus/database"
)

// Subscription mirrors a gateway subscription locally for access checks.
type Subscription struct {
	database.Model
	Gateway    string     `json:"gateway" gorm:"index"`
	GatewayID  string     `json:"gateway_id" gorm:"index"` // subscription id
	Subject    string     `json:"subject" gorm:"index"`    // user id
	Plan       string     `json:"plan"`
	PriceID    string     `json:"price_id"`
	Status     string     `json:"status" gorm:"index"` // active|trialing|canceled|past_due
	EndsAt     *time.Time `json:"ends_at"`
	CanceledAt *time.Time `json:"canceled_at"`
}

// TableName is the DB table for subscriptions.
func (Subscription) TableName() string { return "cashier_subscriptions" }

// Active reports whether the subscription currently grants access.
func (s Subscription) Active() bool {
	if s.Status != "active" && s.Status != "trialing" {
		return false
	}
	if s.EndsAt != nil && time.Now().After(*s.EndsAt) {
		return false
	}
	return true
}
