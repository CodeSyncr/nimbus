package contracts

import "context"

// RefundParams requests a refund. A zero Amount refunds the whole payment.
type RefundParams struct {
	PaymentID   string
	Amount      int64 // smallest unit; 0 → full refund
	Currency    string
	Reason      string // "requested_by_customer" | "duplicate" | "fraudulent" | provider-specific
	Idempotency string
	Metadata    map[string]string
}

// RefundStatus is the canonical state of a refund.
type RefundStatus string

const (
	RefundPending   RefundStatus = "pending"
	RefundSucceeded RefundStatus = "succeeded"
	RefundFailed    RefundStatus = "failed"
)

// Refund is a processed refund reduced to what Nimbus records.
type Refund struct {
	Gateway   string
	ID        string
	PaymentID string
	Amount    int64
	Currency  string
	Status    RefundStatus
	Raw       map[string]any
}

// RefundGateway is implemented by gateways that can reverse a payment.
type RefundGateway interface {
	Refund(ctx context.Context, p RefundParams) (*Refund, error)
}
