package cashier

import (
	"errors"
	"time"
)

/*
Subscriber lifecycle, modeled on RevenueCat's event stream.

RevenueCat's backend turns raw store/gateway noise into one canonical stream of
subscriber events — INITIAL_PURCHASE, RENEWAL, CANCELLATION, UNCANCELLATION,
BILLING_ISSUE, PRODUCT_CHANGE, EXPIRATION, promotional grants, transfers — and
keeps the entitlement state those events imply. This file is that engine for
Cashier: the app reports what happened (a payment, a cancel click, a failed
charge), and the lifecycle updates entitlements through the paywall store and
emits the canonical event for anything downstream (analytics, email, logs).

Two rules mirror RevenueCat's behaviour:

 1. Cancelling is not losing access. A cancellation only turns renewal off;
    entitlements survive until the paid period ends, and expiration is its own
    later event.
 2. A failed renewal opens a grace period instead of cutting access — the
    entitlement is extended by the configured grace window while the gateway
    retries, and only expires if payment never recovers.
*/

// Subscriber event types (RevenueCat parity).
const (
	EventInitialPurchase     = "initial_purchase"
	EventRenewal             = "renewal"
	EventNonRenewingPurchase = "non_renewing_purchase"
	EventCancellation        = "cancellation"
	EventUncancellation      = "uncancellation"
	EventExpiration          = "expiration"
	EventBillingIssue        = "billing_issue"
	EventProductChange       = "product_change"
	EventPaused              = "subscription_paused"
	EventExtended            = "subscription_extended"
	EventPromotionalGrant    = "promotional_grant"
	EventPromotionalRevoke   = "promotional_revoke"
	EventTransfer            = "transfer"
)

// Period types carried on entitlements and events.
const (
	PeriodTrial       = "trial"
	PeriodIntro       = "intro"
	PeriodNormal      = "normal"
	PeriodPromotional = "promotional"
	PeriodGrace       = "grace"
)

// Cancellation / expiration reasons.
const (
	ReasonUnsubscribe        = "unsubscribe"
	ReasonBillingError       = "billing_error"
	ReasonDeveloperInitiated = "developer_initiated"
	ReasonPriceIncrease      = "price_increase"
	ReasonCustomerSupport    = "customer_support"
	ReasonUnknown            = "unknown"
)

// Entitlement sources.
const (
	SourcePurchase    = "purchase"
	SourcePromotional = "promotional"
)

// DefaultGracePeriod is how long a billing issue keeps access alive while the
// gateway retries the charge.
const DefaultGracePeriod = 72 * time.Hour

// ErrNoLifecycleStore is returned when the lifecycle has no paywall to write to.
var ErrNoLifecycleStore = errors.New("cashier: lifecycle has no paywall store")

// SubscriberEvent is one canonical moment in a subscriber's life.
type SubscriberEvent struct {
	Type           string     `json:"type"`
	Subject        string     `json:"subject"`
	ProductID      string     `json:"product_id,omitempty"`
	OldProductID   string     `json:"old_product_id,omitempty"` // product_change
	FromSubject    string     `json:"from_subject,omitempty"`   // transfer
	EntitlementIDs []string   `json:"entitlement_ids,omitempty"`
	PeriodType     string     `json:"period_type,omitempty"`
	Reason         string     `json:"reason,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	At             time.Time  `json:"at"`
}

// Lifecycle applies subscriber events to entitlements and emits them.
type Lifecycle struct {
	catalog *Catalog
	paywall *Paywall
	grace   time.Duration
	onEvent func(SubscriberEvent)
}

// NewLifecycle builds the engine. A nil catalog gets an empty one (products
// then unlock entitlements named after themselves); grace <= 0 uses
// DefaultGracePeriod; onEvent may be nil.
func NewLifecycle(catalog *Catalog, paywall *Paywall, grace time.Duration, onEvent func(SubscriberEvent)) *Lifecycle {
	if catalog == nil {
		catalog = NewCatalog()
	}
	if grace <= 0 {
		grace = DefaultGracePeriod
	}
	return &Lifecycle{catalog: catalog, paywall: paywall, grace: grace, onEvent: onEvent}
}

// Catalog returns the product catalogue the lifecycle resolves against.
func (l *Lifecycle) Catalog() *Catalog { return l.catalog }

func (l *Lifecycle) emit(e SubscriberEvent) SubscriberEvent {
	e.At = time.Now()
	if l.onEvent != nil {
		l.onEvent(e)
	}
	return e
}

func (l *Lifecycle) store() (EntitlementStore, error) {
	if l.paywall == nil || l.paywall.Store() == nil {
		return nil, ErrNoLifecycleStore
	}
	return l.paywall.Store(), nil
}

// activeEntitlements returns the subject's currently active entitlements.
func (l *Lifecycle) activeEntitlements(subject string) []Entitlement {
	store, err := l.store()
	if err != nil {
		return nil
	}
	list, err := store.List(subject)
	if err != nil {
		return nil
	}
	now := time.Now()
	var out []Entitlement
	for _, e := range list {
		if e.ExpiresAt.IsZero() || e.ExpiresAt.After(now) {
			out = append(out, e)
		}
	}
	return out
}

func (l *Lifecycle) grantProduct(subject, productID string, expires time.Time, periodType, source string, willRenew bool) ([]string, error) {
	store, err := l.store()
	if err != nil {
		return nil, err
	}
	ents := l.catalog.EntitlementsFor(productID)
	for _, ent := range ents {
		if err := store.Grant(Entitlement{
			Subject:    subject,
			Plan:       ent,
			ExpiresAt:  expires,
			ProductID:  productID,
			PeriodType: periodType,
			Source:     source,
			WillRenew:  willRenew,
		}); err != nil {
			return nil, err
		}
	}
	return ents, nil
}

// revokeExclusive revokes the entitlements of oldProduct that newProduct does
// not also unlock, so a plan change never strips shared access.
func (l *Lifecycle) revokeExclusive(subject, oldProduct, newProduct string) {
	store, err := l.store()
	if err != nil {
		return
	}
	keep := map[string]bool{}
	for _, e := range l.catalog.EntitlementsFor(newProduct) {
		keep[e] = true
	}
	for _, e := range l.catalog.EntitlementsFor(oldProduct) {
		if !keep[e] {
			_ = store.Revoke(subject, e)
		}
	}
}

// RecordPurchase applies a verified subscription payment: it grants the
// product's entitlements until expiresAt and emits the matching event —
// initial_purchase for a first-time subscriber, renewal when the subject
// already held this product, or product_change when they held a different paid
// product (whose exclusive entitlements are revoked).
//
// periodType is one of the Period* constants; "" means PeriodNormal, and a
// product with trial days still in the future may pass PeriodTrial.
func (l *Lifecycle) RecordPurchase(subject, productID string, expiresAt time.Time, periodType string) (SubscriberEvent, error) {
	if periodType == "" {
		periodType = PeriodNormal
	}

	eventType := EventInitialPurchase
	oldProduct := ""
	for _, held := range l.activeEntitlements(subject) {
		if held.Source == SourcePromotional {
			continue
		}
		if held.ProductID == productID {
			eventType = EventRenewal
			break
		}
		if held.ProductID != "" && held.ProductID != productID {
			if p, ok := l.catalog.Product(held.ProductID); ok && p.Paid() {
				eventType = EventProductChange
				oldProduct = held.ProductID
			}
		}
	}

	ents, err := l.grantProduct(subject, productID, expiresAt, periodType, SourcePurchase, true)
	if err != nil {
		return SubscriberEvent{}, err
	}
	if eventType == EventProductChange {
		l.revokeExclusive(subject, oldProduct, productID)
	}

	exp := expiresAt
	return l.emit(SubscriberEvent{
		Type: eventType, Subject: subject, ProductID: productID, OldProductID: oldProduct,
		EntitlementIDs: ents, PeriodType: periodType, ExpiresAt: &exp,
	}), nil
}

// RecordNonRenewingPurchase applies a one-off purchase (a lifetime unlock, a
// consumable): entitlements are granted but nothing will renew. A zero
// expiresAt never lapses.
func (l *Lifecycle) RecordNonRenewingPurchase(subject, productID string, expiresAt time.Time) (SubscriberEvent, error) {
	ents, err := l.grantProduct(subject, productID, expiresAt, PeriodNormal, SourcePurchase, false)
	if err != nil {
		return SubscriberEvent{}, err
	}
	evt := SubscriberEvent{Type: EventNonRenewingPurchase, Subject: subject, ProductID: productID, EntitlementIDs: ents, PeriodType: PeriodNormal}
	if !expiresAt.IsZero() {
		exp := expiresAt
		evt.ExpiresAt = &exp
	}
	return l.emit(evt), nil
}

// RecordCancellation turns renewal off without touching access: the subject
// keeps every entitlement until it expires on its own. reason is one of the
// Reason* constants ("" → unsubscribe).
func (l *Lifecycle) RecordCancellation(subject, productID, reason string) (SubscriberEvent, error) {
	if reason == "" {
		reason = ReasonUnsubscribe
	}
	return l.setRenewal(subject, productID, false, EventCancellation, reason)
}

// RecordUncancellation re-enables renewal on a cancelled-but-not-expired
// subscription.
func (l *Lifecycle) RecordUncancellation(subject, productID string) (SubscriberEvent, error) {
	return l.setRenewal(subject, productID, true, EventUncancellation, "")
}

// RecordPause marks the subscription as pausing at period end (access is kept
// until then, like a cancellation with intent to return).
func (l *Lifecycle) RecordPause(subject, productID string) (SubscriberEvent, error) {
	return l.setRenewal(subject, productID, false, EventPaused, "")
}

func (l *Lifecycle) setRenewal(subject, productID string, willRenew bool, eventType, reason string) (SubscriberEvent, error) {
	store, err := l.store()
	if err != nil {
		return SubscriberEvent{}, err
	}
	ents := l.catalog.EntitlementsFor(productID)
	var latest *time.Time
	for _, held := range l.activeEntitlements(subject) {
		for _, ent := range ents {
			if held.Plan != ent {
				continue
			}
			held.WillRenew = willRenew
			if held.ProductID == "" {
				held.ProductID = productID
			}
			if err := store.Grant(held); err != nil {
				return SubscriberEvent{}, err
			}
			if !held.ExpiresAt.IsZero() {
				exp := held.ExpiresAt
				latest = &exp
			}
		}
	}
	return l.emit(SubscriberEvent{
		Type: eventType, Subject: subject, ProductID: productID,
		EntitlementIDs: ents, Reason: reason, ExpiresAt: latest,
	}), nil
}

// RecordBillingIssue applies a failed renewal charge: instead of revoking, it
// opens a grace period — each of the product's entitlements is extended to at
// least now+grace while the gateway retries. Returns when grace runs out.
func (l *Lifecycle) RecordBillingIssue(subject, productID string) (SubscriberEvent, error) {
	store, err := l.store()
	if err != nil {
		return SubscriberEvent{}, err
	}
	graceUntil := time.Now().Add(l.grace)
	ents := l.catalog.EntitlementsFor(productID)
	for _, held := range l.activeEntitlements(subject) {
		for _, ent := range ents {
			if held.Plan != ent {
				continue
			}
			if held.ExpiresAt.IsZero() || held.ExpiresAt.After(graceUntil) {
				continue // already covered past the grace window
			}
			held.ExpiresAt = graceUntil
			held.PeriodType = PeriodGrace
			if held.ProductID == "" {
				held.ProductID = productID
			}
			if err := store.Grant(held); err != nil {
				return SubscriberEvent{}, err
			}
		}
	}
	exp := graceUntil
	return l.emit(SubscriberEvent{
		Type: EventBillingIssue, Subject: subject, ProductID: productID,
		EntitlementIDs: ents, PeriodType: PeriodGrace, Reason: ReasonBillingError, ExpiresAt: &exp,
	}), nil
}

// RecordExtension pushes a subscription's expiry out (a support credit, an
// outage make-good) and emits subscription_extended.
func (l *Lifecycle) RecordExtension(subject, productID string, newExpiry time.Time) (SubscriberEvent, error) {
	ents, err := l.grantProduct(subject, productID, newExpiry, PeriodNormal, SourcePurchase, true)
	if err != nil {
		return SubscriberEvent{}, err
	}
	exp := newExpiry
	return l.emit(SubscriberEvent{
		Type: EventExtended, Subject: subject, ProductID: productID,
		EntitlementIDs: ents, ExpiresAt: &exp,
	}), nil
}

// RecordExpiration revokes a product's entitlements now and emits expiration.
// Call it from the sweep that notices a lapsed period, or on a refund /
// immediate revocation (pass the fitting reason).
func (l *Lifecycle) RecordExpiration(subject, productID, reason string) (SubscriberEvent, error) {
	store, err := l.store()
	if err != nil {
		return SubscriberEvent{}, err
	}
	if reason == "" {
		reason = ReasonUnknown
	}
	ents := l.catalog.EntitlementsFor(productID)
	for _, ent := range ents {
		if err := store.Revoke(subject, ent); err != nil {
			return SubscriberEvent{}, err
		}
	}
	return l.emit(SubscriberEvent{
		Type: EventExpiration, Subject: subject, ProductID: productID,
		EntitlementIDs: ents, Reason: reason,
	}), nil
}

// GrantPromotional gives an entitlement without a purchase (a comp, a beta
// invite, an outage credit) until `until`; zero = forever. It never renews.
func (l *Lifecycle) GrantPromotional(subject, entitlementID string, until time.Time) (SubscriberEvent, error) {
	store, err := l.store()
	if err != nil {
		return SubscriberEvent{}, err
	}
	if err := store.Grant(Entitlement{
		Subject: subject, Plan: entitlementID, ExpiresAt: until,
		PeriodType: PeriodPromotional, Source: SourcePromotional,
	}); err != nil {
		return SubscriberEvent{}, err
	}
	evt := SubscriberEvent{Type: EventPromotionalGrant, Subject: subject, EntitlementIDs: []string{entitlementID}, PeriodType: PeriodPromotional}
	if !until.IsZero() {
		exp := until
		evt.ExpiresAt = &exp
	}
	return l.emit(evt), nil
}

// RevokePromotional removes a promotional entitlement.
func (l *Lifecycle) RevokePromotional(subject, entitlementID string) (SubscriberEvent, error) {
	store, err := l.store()
	if err != nil {
		return SubscriberEvent{}, err
	}
	if err := store.Revoke(subject, entitlementID); err != nil {
		return SubscriberEvent{}, err
	}
	return l.emit(SubscriberEvent{Type: EventPromotionalRevoke, Subject: subject, EntitlementIDs: []string{entitlementID}}), nil
}

// Transfer moves every active entitlement from one subject to another
// (RevenueCat's TRANSFER — a purchase restored on a device signed into a
// different account).
func (l *Lifecycle) Transfer(from, to string) (SubscriberEvent, error) {
	store, err := l.store()
	if err != nil {
		return SubscriberEvent{}, err
	}
	moved := []string{}
	for _, held := range l.activeEntitlements(from) {
		granted := held
		granted.Subject = to
		if err := store.Grant(granted); err != nil {
			return SubscriberEvent{}, err
		}
		if err := store.Revoke(from, held.Plan); err != nil {
			return SubscriberEvent{}, err
		}
		moved = append(moved, held.Plan)
	}
	return l.emit(SubscriberEvent{Type: EventTransfer, Subject: to, FromSubject: from, EntitlementIDs: moved}), nil
}
