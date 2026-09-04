package gateways

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
)

/*
Razorpay subscription, refund and customer capabilities.

Razorpay's recurring flow is shaped by RBI's e-mandate rules, so it is not
Stripe's: a subscription is created against a pre-registered plan, and the
customer must authorise the mandate through a short_url redirect before the
first charge. CreateSubscription therefore returns an AuthURL and a "pending"
status far more often than Stripe does — the subscription is real but not yet
authorised, and the caller has to send the customer through the redirect.
*/

var (
	_ contracts.SubscriptionGateway = (*RazorpayGateway)(nil)
	_ contracts.RefundGateway       = (*RazorpayGateway)(nil)
	_ contracts.CustomerGateway     = (*RazorpayGateway)(nil)
)

func (r *RazorpayGateway) CreateSubscription(ctx context.Context, p contracts.SubscriptionParams) (*contracts.Subscription, error) {
	if p.PlanID == "" {
		return nil, fmt.Errorf("cashier/razorpay: SubscriptionParams.PlanID (a Razorpay plan id) is required")
	}
	req := map[string]any{
		"plan_id":         p.PlanID,
		"customer_notify": 1,
		// total_count is required by Razorpay; 0 is invalid, so a subscription
		// meant to run until cancelled uses a large finite count.
		"total_count": razorpayTotalCount(p.TotalCycles),
	}
	if p.Quantity > 1 {
		req["quantity"] = p.Quantity
	}
	if p.TrialEnd != nil {
		req["start_at"] = p.TrialEnd.Unix()
	} else if p.TrialDays > 0 {
		req["start_at"] = time.Now().AddDate(0, 0, p.TrialDays).Unix()
	}
	if len(p.Metadata) > 0 {
		req["notes"] = p.Metadata
	}
	if p.CustomerID != "" {
		req["customer_id"] = p.CustomerID
	}

	var out map[string]any
	if err := r.do(ctx, http.MethodPost, "/v1/subscriptions", req, &out); err != nil {
		return nil, err
	}
	sub := r.toSubscription(out)
	sub.Subject = p.Subject
	return sub, nil
}

func (r *RazorpayGateway) GetSubscription(ctx context.Context, id string) (*contracts.Subscription, error) {
	var out map[string]any
	if err := r.do(ctx, http.MethodGet, "/v1/subscriptions/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return r.toSubscription(out), nil
}

func (r *RazorpayGateway) CancelSubscription(ctx context.Context, id string, p contracts.CancelParams) (*contracts.Subscription, error) {
	// cancel_at_cycle_end=1 keeps access until the paid cycle ends.
	req := map[string]any{"cancel_at_cycle_end": boolToInt(p.AtPeriodEnd)}
	var out map[string]any
	if err := r.do(ctx, http.MethodPost, "/v1/subscriptions/"+url.PathEscape(id)+"/cancel", req, &out); err != nil {
		return nil, err
	}
	return r.toSubscription(out), nil
}

func (r *RazorpayGateway) Refund(ctx context.Context, p contracts.RefundParams) (*contracts.Refund, error) {
	if p.PaymentID == "" {
		return nil, fmt.Errorf("cashier/razorpay: RefundParams.PaymentID is required")
	}
	req := map[string]any{}
	if p.Amount > 0 {
		req["amount"] = p.Amount
	}
	if p.Idempotency != "" {
		req["receipt"] = p.Idempotency
	}
	if len(p.Metadata) > 0 {
		req["notes"] = p.Metadata
	}
	var out map[string]any
	if err := r.do(ctx, http.MethodPost, "/v1/payments/"+url.PathEscape(p.PaymentID)+"/refund", req, &out); err != nil {
		return nil, err
	}
	return &contracts.Refund{
		Gateway:   r.Name(),
		ID:        str(out["id"]),
		PaymentID: p.PaymentID,
		Amount:    int64Of(out["amount"]),
		Currency:  str(out["currency"]),
		Status:    razorpayRefundStatus(str(out["status"])),
		Raw:       out,
	}, nil
}

func (r *RazorpayGateway) CreateCustomer(ctx context.Context, p contracts.CustomerParams) (*contracts.Customer, error) {
	req := map[string]any{"fail_existing": "0"}
	if p.Email != "" {
		req["email"] = p.Email
	}
	if p.Name != "" {
		req["name"] = p.Name
	}
	if p.Phone != "" {
		req["contact"] = p.Phone
	}
	if len(p.Metadata) > 0 {
		req["notes"] = p.Metadata
	}
	var out map[string]any
	if err := r.do(ctx, http.MethodPost, "/v1/customers", req, &out); err != nil {
		return nil, err
	}
	return &contracts.Customer{Gateway: r.Name(), ID: str(out["id"]), Email: str(out["email"]), Subject: p.Subject, Raw: out}, nil
}

func (r *RazorpayGateway) GetCustomer(ctx context.Context, id string) (*contracts.Customer, error) {
	var out map[string]any
	if err := r.do(ctx, http.MethodGet, "/v1/customers/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &contracts.Customer{Gateway: r.Name(), ID: str(out["id"]), Email: str(out["email"]), Raw: out}, nil
}

// toSubscription maps a Razorpay subscription onto the canonical shape,
// carrying the mandate authorisation URL when the subscription is not yet live.
func (r *RazorpayGateway) toSubscription(o map[string]any) *contracts.Subscription {
	sub := &contracts.Subscription{
		Gateway:    r.Name(),
		ID:         str(o["id"]),
		Status:     razorpaySubStatus(str(o["status"])),
		PlanID:     str(o["plan_id"]),
		CustomerID: str(o["customer_id"]),
		AuthURL:    str(o["short_url"]),
		Raw:        o,
	}
	if v := int64Of(o["current_end"]); v > 0 {
		t := time.Unix(v, 0)
		sub.CurrentPeriodEnd = &t
	}
	if v := int64Of(o["ended_at"]); v > 0 {
		t := time.Unix(v, 0)
		sub.EndedAt = &t
	}
	return sub
}

// razorpaySubStatus maps Razorpay's status vocabulary onto the canonical set.
func razorpaySubStatus(s string) contracts.SubscriptionStatus {
	switch s {
	case "active":
		return contracts.SubActive
	case "authenticated", "created":
		return contracts.SubPending
	case "pending", "halted":
		return contracts.SubPastDue
	case "paused":
		return contracts.SubPaused
	case "cancelled", "completed", "expired":
		return contracts.SubCanceled
	default:
		return contracts.SubUnknown
	}
}

func razorpayRefundStatus(s string) contracts.RefundStatus {
	switch s {
	case "processed":
		return contracts.RefundSucceeded
	case "failed":
		return contracts.RefundFailed
	default:
		return contracts.RefundPending
	}
}

// razorpayTotalCount turns "run until cancelled" (0) into a finite count
// Razorpay accepts. 1200 monthly cycles is a century — effectively unbounded.
func razorpayTotalCount(cycles int) int {
	if cycles > 0 {
		return cycles
	}
	return 1200
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
