// Package models holds the persisted Cashier tables (transactions,
// subscriptions) and their migrations.
package models

import "github.com/CodeSyncr/nimbus/database"

// Transaction records a single charge/payment across any gateway.
type Transaction struct {
	database.Model
	Gateway   string `json:"gateway" gorm:"index"`    // stripe|razorpay|payu
	GatewayID string `json:"gateway_id" gorm:"index"` // order/session/payment id
	Reference string `json:"reference" gorm:"index"`  // app order id
	Subject   string `json:"subject" gorm:"index"`    // user id
	Plan      string `json:"plan"`
	Amount    int64  `json:"amount"` // smallest currency unit (paise/cents)
	Currency  string `json:"currency"`
	Status    string `json:"status" gorm:"index"` // created|paid|failed|refunded
	Meta      string `json:"meta"`                // optional JSON blob
}

// TableName is the DB table for transactions.
func (Transaction) TableName() string { return "cashier_transactions" }
