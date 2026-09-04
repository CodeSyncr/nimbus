package models

import (
	"time"

	"github.com/CodeSyncr/nimbus/database"
)

// Purchase mirrors a verified in-app purchase from Apple or Google.
//
// Like Subscription it is a mirror: the store owns the transaction, and this
// row is what an access check reads. The stable key is
// OriginalTransactionID — every renewal of one store subscription shares it,
// so upserting on it keeps one row per entitlement rather than one per renewal.
type Purchase struct {
	database.Model
	Platform              string `json:"platform" gorm:"index"` // apple|google
	Subject               string `json:"subject" gorm:"index"`  // user id
	ProductID             string `json:"product_id" gorm:"index"`
	TransactionID         string `json:"transaction_id"`
	OriginalTransactionID string `json:"original_transaction_id" gorm:"uniqueIndex:idx_purchase_orig"`

	Subscription bool       `json:"subscription"`
	Status       string     `json:"status" gorm:"index"` // active|expired|canceled|refunded
	ExpiresAt    *time.Time `json:"expires_at"`
	AutoRenewing bool       `json:"auto_renewing"`
	Environment  string     `json:"environment"` // production|sandbox
}

// TableName is the DB table for in-app purchases.
func (Purchase) TableName() string { return "cashier_purchases" }

// Active reports whether the purchase currently grants access.
func (p Purchase) Active() bool {
	if p.Status != "active" {
		return false
	}
	if p.Subscription && p.ExpiresAt != nil && time.Now().After(*p.ExpiresAt) {
		return false
	}
	return true
}
