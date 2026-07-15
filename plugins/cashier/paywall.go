package cashier

import (
	stdhttp "net/http"
	"sync"
	"time"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// Entitlement grants a subject (usually a user id) access to a plan until
// ExpiresAt. A zero ExpiresAt means it never expires.
type Entitlement struct {
	Subject   string
	Plan      string
	ExpiresAt time.Time
}

// EntitlementStore persists entitlements. Implement it to back the paywall with
// a database; MemoryEntitlementStore is the default in-memory implementation.
type EntitlementStore interface {
	Grant(e Entitlement) error
	Revoke(subject, plan string) error
	Active(subject, plan string) (bool, error)
	List(subject string) ([]Entitlement, error)
}

// Paywall gates access to plans/features based on entitlements. Grant on a
// successful payment (typically in a webhook handler); gate routes with
// RequirePlan.
type Paywall struct {
	store EntitlementStore
}

// NewPaywall builds a paywall over a store (nil → in-memory).
func NewPaywall(store EntitlementStore) *Paywall {
	if store == nil {
		store = NewMemoryEntitlementStore()
	}
	return &Paywall{store: store}
}

// Store returns the underlying store.
func (p *Paywall) Store() EntitlementStore { return p.store }

// Grant gives subject access to plan until expires (zero time = forever).
func (p *Paywall) Grant(subject, plan string, expires time.Time) error {
	return p.store.Grant(Entitlement{Subject: subject, Plan: plan, ExpiresAt: expires})
}

// Revoke removes a subject's access to a plan.
func (p *Paywall) Revoke(subject, plan string) error { return p.store.Revoke(subject, plan) }

// HasAccess reports whether subject currently has active access to plan.
func (p *Paywall) HasAccess(subject, plan string) bool {
	ok, _ := p.store.Active(subject, plan)
	return ok
}

// RequirePlan returns middleware that blocks a route unless the current subject
// has active access to plan. subjectFn extracts the subject (e.g. the user id)
// from the request; return "" for an anonymous/unknown user. Blocked requests
// receive HTTP 402 Payment Required.
func (p *Paywall) RequirePlan(plan string, subjectFn func(*nhttp.Context) string) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *nhttp.Context) error {
			subject := ""
			if subjectFn != nil {
				subject = subjectFn(c)
			}
			if subject == "" || !p.HasAccess(subject, plan) {
				return c.JSON(stdhttp.StatusPaymentRequired, map[string]any{
					"error": "payment_required",
					"plan":  plan,
				})
			}
			return next(c)
		}
	}
}

// ── In-memory store ───────────────────────────────────────────────

// MemoryEntitlementStore is a concurrency-safe in-memory EntitlementStore. Use a
// database-backed store in production (entitlements should survive a restart).
type MemoryEntitlementStore struct {
	mu   sync.RWMutex
	data map[string]map[string]Entitlement // subject → plan → entitlement
}

// NewMemoryEntitlementStore creates an empty in-memory store.
func NewMemoryEntitlementStore() *MemoryEntitlementStore {
	return &MemoryEntitlementStore{data: map[string]map[string]Entitlement{}}
}

func (s *MemoryEntitlementStore) Grant(e Entitlement) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[e.Subject] == nil {
		s.data[e.Subject] = map[string]Entitlement{}
	}
	s.data[e.Subject][e.Plan] = e
	return nil
}

func (s *MemoryEntitlementStore) Revoke(subject, plan string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if plans := s.data[subject]; plans != nil {
		delete(plans, plan)
	}
	return nil
}

func (s *MemoryEntitlementStore) Active(subject, plan string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.data[subject][plan]
	if !ok {
		return false, nil
	}
	if !e.ExpiresAt.IsZero() && time.Now().After(e.ExpiresAt) {
		return false, nil
	}
	return true, nil
}

func (s *MemoryEntitlementStore) List(subject string) ([]Entitlement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Entitlement
	for _, e := range s.data[subject] {
		out = append(out, e)
	}
	return out, nil
}
