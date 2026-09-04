package cashier

import (
	"sort"
	"time"
)

/*
CustomerInfo is RevenueCat's central read model: one aggregate answering
"what does this subscriber have right now?" — every entitlement with its
state (active, renewing, period type, expiry), the products behind them, and
any gateway subscriptions mirrored locally. The client (or a dashboard) reads
this one object instead of stitching paywall checks together.
*/

// EntitlementInfo is one entitlement's state inside CustomerInfo.
type EntitlementInfo struct {
	ID         string     `json:"id"`
	Active     bool       `json:"active"`
	WillRenew  bool       `json:"will_renew"`
	PeriodType string     `json:"period_type,omitempty"` // trial|intro|normal|promotional|grace
	ProductID  string     `json:"product_id,omitempty"`
	Source     string     `json:"source,omitempty"` // purchase|promotional
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// CustomerInfo aggregates a subject's subscription state.
type CustomerInfo struct {
	Subject     string    `json:"subject"`
	RequestedAt time.Time `json:"requested_at"`

	// Entitlements is every known entitlement, keyed by id — active or not.
	Entitlements map[string]EntitlementInfo `json:"entitlements"`
	// ActiveEntitlementIDs lists the ids currently granting access, sorted.
	ActiveEntitlementIDs []string `json:"active_entitlement_ids"`
	// ActiveProductIDs lists the products behind active entitlements, sorted.
	ActiveProductIDs []string `json:"active_product_ids"`
	// ActiveSubscriptions are gateway subscriptions currently granting access,
	// from the local mirror (empty when no mirror is configured).
	ActiveSubscriptions []*Subscription `json:"active_subscriptions,omitempty"`
	// LatestExpiresAt is the furthest-out expiry across active entitlements
	// (nil when an active entitlement never expires or none are active).
	LatestExpiresAt *time.Time `json:"latest_expires_at,omitempty"`
}

// HasEntitlement reports whether the aggregate holds an active entitlement.
func (ci CustomerInfo) HasEntitlement(id string) bool {
	e, ok := ci.Entitlements[id]
	return ok && e.Active
}

// CustomerInfo builds the aggregate for a subject from the paywall store and
// the subscription mirror. Customer management is part of the Cashier Cloud
// suite: on a payments-only facade (no cloud key, so no lifecycle) the
// aggregate comes back empty.
func (c *Cashier) CustomerInfo(subject string) CustomerInfo {
	info := CustomerInfo{
		Subject:      subject,
		RequestedAt:  time.Now(),
		Entitlements: map[string]EntitlementInfo{},
	}
	if !c.CloudEnabled() {
		return info
	}

	if c.Paywall != nil && c.Paywall.Store() != nil {
		list, err := c.Paywall.Store().List(subject)
		if err == nil {
			now := time.Now()
			neverExpires := false
			products := map[string]bool{}
			for _, e := range list {
				active := e.ExpiresAt.IsZero() || e.ExpiresAt.After(now)
				ei := EntitlementInfo{
					ID: e.Plan, Active: active, WillRenew: e.WillRenew,
					PeriodType: e.PeriodType, ProductID: e.ProductID, Source: e.Source,
				}
				if !e.ExpiresAt.IsZero() {
					exp := e.ExpiresAt
					ei.ExpiresAt = &exp
				}
				info.Entitlements[e.Plan] = ei
				if !active {
					continue
				}
				info.ActiveEntitlementIDs = append(info.ActiveEntitlementIDs, e.Plan)
				if e.ProductID != "" && !products[e.ProductID] {
					products[e.ProductID] = true
					info.ActiveProductIDs = append(info.ActiveProductIDs, e.ProductID)
				}
				if e.ExpiresAt.IsZero() {
					neverExpires = true
				} else if info.LatestExpiresAt == nil || e.ExpiresAt.After(*info.LatestExpiresAt) {
					exp := e.ExpiresAt
					info.LatestExpiresAt = &exp
				}
			}
			if neverExpires {
				info.LatestExpiresAt = nil
			}
			sort.Strings(info.ActiveEntitlementIDs)
			sort.Strings(info.ActiveProductIDs)
		}
	}

	if c.Subscriptions != nil {
		info.ActiveSubscriptions = c.Subscriptions.ActiveForSubject(subject)
	}

	return info
}
