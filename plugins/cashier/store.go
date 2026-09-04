package cashier

import (
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/cashier/contracts"
)

// SubscriptionStore persists the local mirror of gateway subscriptions.
//
// Upsert is keyed on (gateway, gateway id): a webhook, a verification and a
// manual refresh all describe the same subscription, and each must update the
// one row rather than insert a duplicate. Implement it over your database; the
// in-memory store is the default and is what the tests run against.
type SubscriptionStore interface {
	Upsert(sub *contracts.Subscription) error
	Get(gateway, id string) (*contracts.Subscription, bool)
	ForSubject(subject string) []*contracts.Subscription
	ActiveForSubject(subject string) []*contracts.Subscription
}

// MemorySubscriptionStore is a process-local SubscriptionStore. It is the
// default and is intended for tests and single-process apps; a real deployment
// backs the mirror with the cashier_subscriptions table.
type MemorySubscriptionStore struct {
	mu   sync.RWMutex
	subs map[string]*contracts.Subscription
}

// NewMemorySubscriptionStore builds an empty in-memory store.
func NewMemorySubscriptionStore() *MemorySubscriptionStore {
	return &MemorySubscriptionStore{subs: map[string]*contracts.Subscription{}}
}

func subKey(gateway, id string) string { return gateway + "\x00" + id }

// Upsert inserts or replaces the mirror of a subscription.
func (m *MemorySubscriptionStore) Upsert(sub *contracts.Subscription) error {
	if sub == nil || sub.ID == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	clone := *sub
	m.subs[subKey(sub.Gateway, sub.ID)] = &clone
	return nil
}

// Get returns one mirrored subscription.
func (m *MemorySubscriptionStore) Get(gateway, id string) (*contracts.Subscription, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.subs[subKey(gateway, id)]
	return s, ok
}

// ForSubject returns every mirrored subscription for a subject.
func (m *MemorySubscriptionStore) ForSubject(subject string) []*contracts.Subscription {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*contracts.Subscription
	for _, s := range m.subs {
		if s.Subject == subject {
			out = append(out, s)
		}
	}
	return out
}

// ActiveForSubject returns only the subscriptions that currently grant access.
func (m *MemorySubscriptionStore) ActiveForSubject(subject string) []*contracts.Subscription {
	var out []*contracts.Subscription
	for _, s := range m.ForSubject(subject) {
		if subscriptionGrantsAccess(s) {
			out = append(out, s)
		}
	}
	return out
}

// subscriptionGrantsAccess is the local access rule, shared by the store and
// the facade so "does this subscription count" is answered one way. A cancelled
// subscription still paid through its period keeps access until it ends.
func subscriptionGrantsAccess(s *contracts.Subscription) bool {
	if s == nil {
		return false
	}
	switch s.Status {
	case contracts.SubActive, contracts.SubTrialing:
		if s.CurrentPeriodEnd != nil && time.Now().After(*s.CurrentPeriodEnd) {
			return false
		}
		return true
	case contracts.SubCanceled:
		return s.CurrentPeriodEnd != nil && time.Now().Before(*s.CurrentPeriodEnd)
	default:
		return false
	}
}

// SubscribedTo reports whether a subject has an active subscription, optionally
// to a specific plan (empty plan → any).
func (c *Cashier) SubscribedTo(subject, plan string) bool {
	if c.Subscriptions == nil {
		return false
	}
	for _, s := range c.Subscriptions.ActiveForSubject(subject) {
		if plan == "" || s.PlanID == plan {
			return true
		}
	}
	return false
}
