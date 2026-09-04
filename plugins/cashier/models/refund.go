package models

import "github.com/CodeSyncr/nimbus/database"

// Refund records a reversal against a transaction, across any gateway.
type Refund struct {
	database.Model
	Gateway   string `json:"gateway" gorm:"index"`
	GatewayID string `json:"gateway_id" gorm:"index"` // refund id
	PaymentID string `json:"payment_id" gorm:"index"` // the charge it reverses
	Subject   string `json:"subject" gorm:"index"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Status    string `json:"status" gorm:"index"` // pending|succeeded|failed
	Reason    string `json:"reason"`
}

// TableName is the DB table for refunds.
func (Refund) TableName() string { return "cashier_refunds" }
