package gateways

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
)

/*
Stripe subscription, refund and customer capabilities.

These are separate from stripe.go's core charge/webhook methods on purpose: the
core is what every gateway has, and this is the optional slice Stripe happens to
implement fully. The compile-time assertions below are what make the capability
model honest — if a method drifts out of the interface, the build breaks here
rather than silently at a type assertion in the facade.
*/

var (
	_ contracts.SubscriptionGateway = (*StripeGateway)(nil)
	_ contracts.SubscriptionSwapper = (*StripeGateway)(nil)
	_ contracts.SubscriptionPauser  = (*StripeGateway)(nil)
	_ contracts.RefundGateway       = (*StripeGateway)(nil)
	_ contracts.CustomerGateway     = (*StripeGateway)(nil)
)

// doIdem is do() with an optional idempotency key. Stripe dedupes any POST
// carrying the same Idempotency-Key for 24 hours, which is what makes a
// retried CreateSubscription safe rather than double-charging.
func (s *StripeGateway) doIdem(ctx context.Context, method, path, idem string, form url.Values, out any) error {
	if idem == "" {
		return s.do(ctx, method, path, form, out)
	}
	// The core do() sets no extra headers, so wrap a request that does.
	return s.doWithHeaders(ctx, method, path, map[string]string{"Idempotency-Key": idem}, form, out)
}

func (s *StripeGateway) CreateSubscription(ctx context.Context, p contracts.SubscriptionParams) (*contracts.Subscription, error) {
	if p.PlanID == "" {
		return nil, fmt.Errorf("cashier/stripe: SubscriptionParams.PlanID (a Stripe price id) is required")
	}
	if p.CustomerID == "" {
		return nil, fmt.Errorf("cashier/stripe: SubscriptionParams.CustomerID is required; create a customer first")
	}
	form := url.Values{}
	form.Set("customer", p.CustomerID)
	form.Set("items[0][price]", p.PlanID)
	if p.Quantity > 1 {
		form.Set("items[0][quantity]", strconv.FormatInt(p.Quantity, 10))
	}
	if p.TrialDays > 0 {
		form.Set("trial_period_days", strconv.Itoa(p.TrialDays))
	} else if p.TrialEnd != nil {
		form.Set("trial_end", strconv.FormatInt(p.TrialEnd.Unix(), 10))
	}
	if p.CouponCode != "" {
		form.Set("discounts[0][coupon]", p.CouponCode)
	}
	// Let Stripe raise a payment intent the client can confirm (SCA), rather
	// than failing the subscription outright when authentication is needed.
	form.Set("payment_behavior", "default_incomplete")
	form.Set("expand[]", "latest_invoice.payment_intent")
	for k, v := range p.Metadata {
		form.Set("metadata["+k+"]", v)
	}

	var out map[string]any
	if err := s.doIdem(ctx, http.MethodPost, "/v1/subscriptions", p.Idempotency, form, &out); err != nil {
		return nil, err
	}
	sub := s.toSubscription(out)
	sub.Subject = p.Subject
	return sub, nil
}

func (s *StripeGateway) GetSubscription(ctx context.Context, id string) (*contracts.Subscription, error) {
	var out map[string]any
	if err := s.do(ctx, http.MethodGet, "/v1/subscriptions/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return s.toSubscription(out), nil
}

func (s *StripeGateway) CancelSubscription(ctx context.Context, id string, p contracts.CancelParams) (*contracts.Subscription, error) {
	var out map[string]any
	if p.AtPeriodEnd {
		// Schedule the cancellation; access continues until the period ends.
		form := url.Values{}
		form.Set("cancel_at_period_end", "true")
		if err := s.do(ctx, http.MethodPost, "/v1/subscriptions/"+url.PathEscape(id), form, &out); err != nil {
			return nil, err
		}
	} else {
		if err := s.do(ctx, http.MethodDelete, "/v1/subscriptions/"+url.PathEscape(id), nil, &out); err != nil {
			return nil, err
		}
	}
	return s.toSubscription(out), nil
}

func (s *StripeGateway) SwapSubscription(ctx context.Context, id string, p contracts.SwapParams) (*contracts.Subscription, error) {
	if p.NewPlanID == "" {
		return nil, fmt.Errorf("cashier/stripe: SwapParams.NewPlanID is required")
	}
	// The subscription item id is what Stripe swaps, so fetch the current one.
	current, err := s.GetSubscription(ctx, id)
	if err != nil {
		return nil, err
	}
	itemID := stripeFirstItemID(current.Raw)
	if itemID == "" {
		return nil, fmt.Errorf("cashier/stripe: could not find the subscription item to swap")
	}
	form := url.Values{}
	form.Set("items[0][id]", itemID)
	form.Set("items[0][price]", p.NewPlanID)
	if p.Quantity > 0 {
		form.Set("items[0][quantity]", strconv.FormatInt(p.Quantity, 10))
	}
	if p.Prorate {
		form.Set("proration_behavior", "create_prorations")
	} else {
		form.Set("proration_behavior", "none")
	}
	var out map[string]any
	if err := s.do(ctx, http.MethodPost, "/v1/subscriptions/"+url.PathEscape(id), form, &out); err != nil {
		return nil, err
	}
	return s.toSubscription(out), nil
}

func (s *StripeGateway) PauseSubscription(ctx context.Context, id string) (*contracts.Subscription, error) {
	form := url.Values{}
	form.Set("pause_collection[behavior]", "void")
	var out map[string]any
	if err := s.do(ctx, http.MethodPost, "/v1/subscriptions/"+url.PathEscape(id), form, &out); err != nil {
		return nil, err
	}
	return s.toSubscription(out), nil
}

func (s *StripeGateway) ResumeSubscription(ctx context.Context, id string) (*contracts.Subscription, error) {
	// Clearing pause_collection resumes billing.
	form := url.Values{}
	form.Set("pause_collection", "")
	var out map[string]any
	if err := s.do(ctx, http.MethodPost, "/v1/subscriptions/"+url.PathEscape(id), form, &out); err != nil {
		return nil, err
	}
	return s.toSubscription(out), nil
}

func (s *StripeGateway) Refund(ctx context.Context, p contracts.RefundParams) (*contracts.Refund, error) {
	if p.PaymentID == "" {
		return nil, fmt.Errorf("cashier/stripe: RefundParams.PaymentID (a payment intent id) is required")
	}
	form := url.Values{}
	form.Set("payment_intent", p.PaymentID)
	if p.Amount > 0 {
		form.Set("amount", strconv.FormatInt(p.Amount, 10))
	}
	if p.Reason != "" {
		form.Set("reason", p.Reason)
	}
	var out map[string]any
	if err := s.doIdem(ctx, http.MethodPost, "/v1/refunds", p.Idempotency, form, &out); err != nil {
		return nil, err
	}
	return &contracts.Refund{
		Gateway:   s.Name(),
		ID:        str(out["id"]),
		PaymentID: p.PaymentID,
		Amount:    int64Of(out["amount"]),
		Currency:  str(out["currency"]),
		Status:    stripeRefundStatus(str(out["status"])),
		Raw:       out,
	}, nil
}

func (s *StripeGateway) CreateCustomer(ctx context.Context, p contracts.CustomerParams) (*contracts.Customer, error) {
	form := url.Values{}
	if p.Email != "" {
		form.Set("email", p.Email)
	}
	if p.Name != "" {
		form.Set("name", p.Name)
	}
	if p.Phone != "" {
		form.Set("phone", p.Phone)
	}
	if p.Subject != "" {
		form.Set("metadata[subject]", p.Subject)
	}
	for k, v := range p.Metadata {
		form.Set("metadata["+k+"]", v)
	}
	var out map[string]any
	if err := s.do(ctx, http.MethodPost, "/v1/customers", form, &out); err != nil {
		return nil, err
	}
	return &contracts.Customer{Gateway: s.Name(), ID: str(out["id"]), Email: str(out["email"]), Subject: p.Subject, Raw: out}, nil
}

func (s *StripeGateway) GetCustomer(ctx context.Context, id string) (*contracts.Customer, error) {
	var out map[string]any
	if err := s.do(ctx, http.MethodGet, "/v1/customers/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &contracts.Customer{Gateway: s.Name(), ID: str(out["id"]), Email: str(out["email"]), Raw: out}, nil
}

// toSubscription maps a Stripe subscription object onto the canonical shape.
func (s *StripeGateway) toSubscription(o map[string]any) *contracts.Subscription {
	sub := &contracts.Subscription{
		Gateway:    s.Name(),
		ID:         str(o["id"]),
		Status:     stripeSubStatus(str(o["status"])),
		CustomerID: str(o["customer"]),
		PlanID:     stripeFirstPrice(o),
		Raw:        o,
	}
	if v := int64Of(o["current_period_end"]); v > 0 {
		t := time.Unix(v, 0)
		sub.CurrentPeriodEnd = &t
	}
	if v := int64Of(o["trial_end"]); v > 0 {
		t := time.Unix(v, 0)
		sub.TrialEnd = &t
	}
	if v := int64Of(o["canceled_at"]); v > 0 {
		t := time.Unix(v, 0)
		sub.CanceledAt = &t
	}
	if b, ok := o["cancel_at_period_end"].(bool); ok && b {
		sub.CancelAt = sub.CurrentPeriodEnd
	}
	return sub
}

// stripeSubStatus maps Stripe's status vocabulary onto the canonical set.
func stripeSubStatus(s string) contracts.SubscriptionStatus {
	switch s {
	case "active":
		return contracts.SubActive
	case "trialing":
		return contracts.SubTrialing
	case "past_due", "unpaid":
		return contracts.SubPastDue
	case "paused":
		return contracts.SubPaused
	case "canceled":
		return contracts.SubCanceled
	case "incomplete", "incomplete_expired":
		return contracts.SubPending
	default:
		return contracts.SubUnknown
	}
}

func stripeRefundStatus(s string) contracts.RefundStatus {
	switch s {
	case "succeeded":
		return contracts.RefundSucceeded
	case "failed", "canceled":
		return contracts.RefundFailed
	default:
		return contracts.RefundPending
	}
}
