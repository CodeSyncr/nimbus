package cashier

import (
	"context"
	"fmt"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
)

// Re-exported subscription and capability types so callers stay in the cashier
// package.
type (
	Subscription       = contracts.Subscription
	SubscriptionParams = contracts.SubscriptionParams
	SubscriptionStatus = contracts.SubscriptionStatus
	CancelParams       = contracts.CancelParams
	SwapParams         = contracts.SwapParams
	Refund             = contracts.Refund
	RefundParams       = contracts.RefundParams
	Customer           = contracts.Customer
	CustomerParams     = contracts.CustomerParams
)

// ErrUnsupported is returned when a gateway lacks a requested capability.
var ErrUnsupported = contracts.ErrUnsupported

// unsupported builds a clear error naming the gateway and the capability, so a
// caller sees "razorpay does not support pausing" rather than a bare sentinel.
func unsupported(gateway, capability string) error {
	return fmt.Errorf("%w: %s does not support %s", ErrUnsupported, gateway, capability)
}

// Subscribe starts a subscription on the named gateway (empty → default), and
// mirrors the result locally when a store is configured.
//
// The mirror write is best-effort by design: the subscription exists at the
// gateway the moment it returns, and failing the caller's request because a
// local cache write hiccuped would be the wrong trade. A missed mirror is
// reconciled by the next webhook.
func (c *Cashier) Subscribe(ctx context.Context, gateway string, p SubscriptionParams) (*Subscription, error) {
	gw, err := c.Gateways.Use(gateway)
	if err != nil {
		return nil, err
	}
	sg, ok := gw.(contracts.SubscriptionGateway)
	if !ok {
		return nil, unsupported(gw.Name(), "subscriptions")
	}
	sub, err := sg.CreateSubscription(ctx, p)
	if err != nil {
		return nil, err
	}
	c.mirror(sub)
	return sub, nil
}

// Subscription fetches a subscription from its gateway and refreshes the mirror.
func (c *Cashier) Subscription(ctx context.Context, gateway, id string) (*Subscription, error) {
	gw, err := c.Gateways.Use(gateway)
	if err != nil {
		return nil, err
	}
	sg, ok := gw.(contracts.SubscriptionGateway)
	if !ok {
		return nil, unsupported(gw.Name(), "subscriptions")
	}
	sub, err := sg.GetSubscription(ctx, id)
	if err != nil {
		return nil, err
	}
	c.mirror(sub)
	return sub, nil
}

// Cancel ends a subscription. AtPeriodEnd (the humane default) keeps access
// until the paid period runs out.
func (c *Cashier) Cancel(ctx context.Context, gateway, id string, p CancelParams) (*Subscription, error) {
	gw, err := c.Gateways.Use(gateway)
	if err != nil {
		return nil, err
	}
	sg, ok := gw.(contracts.SubscriptionGateway)
	if !ok {
		return nil, unsupported(gw.Name(), "subscriptions")
	}
	sub, err := sg.CancelSubscription(ctx, id, p)
	if err != nil {
		return nil, err
	}
	c.mirror(sub)
	return sub, nil
}

// Swap changes a subscription's plan, where the gateway allows it.
func (c *Cashier) Swap(ctx context.Context, gateway, id string, p SwapParams) (*Subscription, error) {
	gw, err := c.Gateways.Use(gateway)
	if err != nil {
		return nil, err
	}
	sw, ok := gw.(contracts.SubscriptionSwapper)
	if !ok {
		return nil, unsupported(gw.Name(), "plan changes")
	}
	sub, err := sw.SwapSubscription(ctx, id, p)
	if err != nil {
		return nil, err
	}
	c.mirror(sub)
	return sub, nil
}

// Pause suspends a subscription, where the gateway allows it.
func (c *Cashier) Pause(ctx context.Context, gateway, id string) (*Subscription, error) {
	return c.pauseResume(ctx, gateway, id, true)
}

// Resume restarts a paused subscription.
func (c *Cashier) Resume(ctx context.Context, gateway, id string) (*Subscription, error) {
	return c.pauseResume(ctx, gateway, id, false)
}

func (c *Cashier) pauseResume(ctx context.Context, gateway, id string, pause bool) (*Subscription, error) {
	gw, err := c.Gateways.Use(gateway)
	if err != nil {
		return nil, err
	}
	pr, ok := gw.(contracts.SubscriptionPauser)
	if !ok {
		return nil, unsupported(gw.Name(), "pausing")
	}
	var sub *Subscription
	if pause {
		sub, err = pr.PauseSubscription(ctx, id)
	} else {
		sub, err = pr.ResumeSubscription(ctx, id)
	}
	if err != nil {
		return nil, err
	}
	c.mirror(sub)
	return sub, nil
}

// Refund reverses a payment, where the gateway allows it.
func (c *Cashier) Refund(ctx context.Context, gateway string, p RefundParams) (*Refund, error) {
	gw, err := c.Gateways.Use(gateway)
	if err != nil {
		return nil, err
	}
	rg, ok := gw.(contracts.RefundGateway)
	if !ok {
		return nil, unsupported(gw.Name(), "refunds")
	}
	return rg.Refund(ctx, p)
}

// Customer creates a gateway customer, where the gateway has the concept.
func (c *Cashier) Customer(ctx context.Context, gateway string, p CustomerParams) (*Customer, error) {
	gw, err := c.Gateways.Use(gateway)
	if err != nil {
		return nil, err
	}
	cg, ok := gw.(contracts.CustomerGateway)
	if !ok {
		return nil, unsupported(gw.Name(), "customers")
	}
	return cg.CreateCustomer(ctx, p)
}

// Capabilities reports which optional capabilities a gateway implements, for a
// UI that hides what a provider cannot do rather than offering it and failing.
func (c *Cashier) Capabilities(gateway string) (Capabilities, error) {
	gw, err := c.Gateways.Use(gateway)
	if err != nil {
		return Capabilities{}, err
	}
	_, sub := gw.(contracts.SubscriptionGateway)
	_, swap := gw.(contracts.SubscriptionSwapper)
	_, pause := gw.(contracts.SubscriptionPauser)
	_, refund := gw.(contracts.RefundGateway)
	_, cust := gw.(contracts.CustomerGateway)
	return Capabilities{
		Subscriptions: sub,
		PlanChanges:   swap,
		Pausing:       pause,
		Refunds:       refund,
		Customers:     cust,
	}, nil
}

// Capabilities describes what a gateway can do beyond a one-off charge.
type Capabilities struct {
	Subscriptions bool
	PlanChanges   bool
	Pausing       bool
	Refunds       bool
	Customers     bool
}

// mirror writes a subscription into the local store when one is configured.
func (c *Cashier) mirror(sub *Subscription) {
	if c.Subscriptions == nil || sub == nil {
		return
	}
	_ = c.Subscriptions.Upsert(sub)
}
