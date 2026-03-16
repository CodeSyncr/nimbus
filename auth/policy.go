package auth

import (
	"context"
	"fmt"
	"sync"
)

// ── Policy Interface ────────────────────────────────────────────

// Policy checks if a user can perform an action (plan: userPolicy.Update(user)).
type Policy interface {
	// Allow returns true if the user can perform the action on the resource.
	Allow(ctx context.Context, user User, action string, resource any) bool
}

// PolicyFunc adapts a function to Policy.
type PolicyFunc func(ctx context.Context, user User, action string, resource any) bool

func (f PolicyFunc) Allow(ctx context.Context, user User, action string, resource any) bool {
	return f(ctx, user, action, resource)
}

// ── Resource Policy ─────────────────────────────────────────────

// ResourcePolicy defines authorization methods for a specific resource type.
// Implement the methods you need; unimplemented methods default to false.
//
// Example:
//
//	type PostPolicy struct{ auth.BasePolicy }
//
//	func (p *PostPolicy) View(ctx context.Context, user auth.User, post *Post) bool { return true }
//	func (p *PostPolicy) Update(ctx context.Context, user auth.User, post *Post) bool {
//	    return user.GetID() == post.AuthorID
//	}
type ResourcePolicy interface {
	// ResourceName returns the name used for gate lookups (e.g. "post", "comment").
	ResourceName() string
}

// BasePolicy provides a default ResourceName from the struct and a Before hook that returns nil (no override).
type BasePolicy struct{}

func (BasePolicy) ResourceName() string { return "" }

// Before is called before any ability check. Return:
//   - (*bool)(true) to always allow
//   - (*bool)(false) to always deny
//   - nil to fall through to the specific ability check
func (BasePolicy) Before(_ context.Context, _ User, _ string) *bool { return nil }

// ── Gate ────────────────────────────────────────────────────────

// Gate is an authorization gate that manages policies and can check abilities.
// It is the central authorization registry (similar to Laravel's Gate facade).
type Gate struct {
	mu         sync.RWMutex
	abilities  map[string]AbilityFunc
	policies   map[string]ResourcePolicy
	beforeHook []BeforeFunc
	afterHook  []AfterFunc
}

// AbilityFunc checks if a user can perform an ability on a resource.
type AbilityFunc func(ctx context.Context, user User, resource any) bool

// BeforeFunc is called before any ability check.
// Return *bool to short-circuit (true=allow, false=deny), or nil to continue.
type BeforeFunc func(ctx context.Context, user User, ability string) *bool

// AfterFunc is called after every ability check with the result.
type AfterFunc func(ctx context.Context, user User, ability string, result bool)

// NewGate creates a new authorization gate.
func NewGate() *Gate {
	return &Gate{
		abilities: make(map[string]AbilityFunc),
		policies:  make(map[string]ResourcePolicy),
	}
}

// Define registers a named ability check.
//
//	gate.Define("edit-post", func(ctx context.Context, user auth.User, resource any) bool {
//	    post := resource.(*Post)
//	    return user.GetID() == post.AuthorID
//	})
func (g *Gate) Define(ability string, fn AbilityFunc) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.abilities[ability] = fn
}

// RegisterPolicy registers a resource policy.
//
//	gate.RegisterPolicy("post", &PostPolicy{})
func (g *Gate) RegisterPolicy(name string, policy ResourcePolicy) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.policies[name] = policy
}

// Before registers a global before hook. Runs before every authorization check.
func (g *Gate) Before(fn BeforeFunc) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.beforeHook = append(g.beforeHook, fn)
}

// After registers a global after hook. Runs after every authorization check.
func (g *Gate) After(fn AfterFunc) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.afterHook = append(g.afterHook, fn)
}

// Allows checks if the user is authorized for the given ability.
func (g *Gate) Allows(ctx context.Context, user User, ability string, resource any) bool {
	g.mu.RLock()
	befores := g.beforeHook
	afters := g.afterHook
	g.mu.RUnlock()

	// Run before hooks.
	for _, fn := range befores {
		if result := fn(ctx, user, ability); result != nil {
			g.runAfters(ctx, user, ability, *result, afters)
			return *result
		}
	}

	result := g.checkAbility(ctx, user, ability, resource)
	g.runAfters(ctx, user, ability, result, afters)
	return result
}

// Denies is the inverse of Allows.
func (g *Gate) Denies(ctx context.Context, user User, ability string, resource any) bool {
	return !g.Allows(ctx, user, ability, resource)
}

// Authorize checks authorization and returns an error if denied.
func (g *Gate) Authorize(ctx context.Context, user User, ability string, resource any) error {
	if g.Denies(ctx, user, ability, resource) {
		return fmt.Errorf("auth: unauthorized action %q", ability)
	}
	return nil
}

// Any returns true if the user can perform any of the given abilities.
func (g *Gate) Any(ctx context.Context, user User, abilities []string, resource any) bool {
	for _, ability := range abilities {
		if g.Allows(ctx, user, ability, resource) {
			return true
		}
	}
	return false
}

// None returns true if the user cannot perform any of the given abilities.
func (g *Gate) None(ctx context.Context, user User, abilities []string, resource any) bool {
	return !g.Any(ctx, user, abilities, resource)
}

// ForUser returns a UserGate scoped to the given user for convenience.
func (g *Gate) ForUser(user User) *UserGate {
	return &UserGate{gate: g, user: user}
}

func (g *Gate) checkAbility(ctx context.Context, user User, ability string, resource any) bool {
	g.mu.RLock()
	fn, ok := g.abilities[ability]
	g.mu.RUnlock()
	if ok {
		return fn(ctx, user, resource)
	}
	return false
}

func (g *Gate) runAfters(ctx context.Context, user User, ability string, result bool, afters []AfterFunc) {
	for _, fn := range afters {
		fn(ctx, user, ability, result)
	}
}

// ── UserGate (scoped to a specific user) ────────────────────────

// UserGate wraps a Gate for a specific user for convenient fluent checks.
type UserGate struct {
	gate *Gate
	user User
}

// Can checks if the user is authorized.
func (ug *UserGate) Can(ctx context.Context, ability string, resource any) bool {
	return ug.gate.Allows(ctx, ug.user, ability, resource)
}

// Cannot checks if the user is NOT authorized.
func (ug *UserGate) Cannot(ctx context.Context, ability string, resource any) bool {
	return ug.gate.Denies(ctx, ug.user, ability, resource)
}

// Authorize checks authorization and returns an error if denied.
func (ug *UserGate) Authorize(ctx context.Context, ability string, resource any) error {
	return ug.gate.Authorize(ctx, ug.user, ability, resource)
}

// ── Default Gate (global singleton) ─────────────────────────────

var defaultGate = NewGate()

// DefaultGate returns the global gate instance.
func DefaultGate() *Gate { return defaultGate }

// Can is a convenience function to check against the default gate.
func Can(ctx context.Context, ability string, resource any) bool {
	user := UserFromContext(ctx)
	if user == nil {
		return false
	}
	return defaultGate.Allows(ctx, user, ability, resource)
}

// Cannot is the inverse of Can.
func Cannot(ctx context.Context, ability string, resource any) bool {
	return !Can(ctx, ability, resource)
}

// AuthorizeAction checks the default gate and returns an error if denied.
func AuthorizeAction(ctx context.Context, ability string, resource any) error {
	user := UserFromContext(ctx)
	if user == nil {
		return fmt.Errorf("auth: unauthenticated")
	}
	return defaultGate.Authorize(ctx, user, ability, resource)
}

// DefineAbility shortcut to register on the default gate.
func DefineAbility(ability string, fn AbilityFunc) {
	defaultGate.Define(ability, fn)
}

// boolPtr is a helper to create a *bool.
func boolPtr(v bool) *bool { return &v }

// AllowAll short-circuits authorization to always allow (useful for admins).
func AllowAll() *bool { return boolPtr(true) }

// DenyAll short-circuits authorization to always deny.
func DenyAll() *bool { return boolPtr(false) }
