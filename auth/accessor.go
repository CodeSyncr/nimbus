package auth

import (
	"fmt"

	"github.com/CodeSyncr/nimbus/http"
)

// Accessor provides AdonisJS-style auth access bound to a single request.
// It implements http.Authenticator so controllers can write:
//
//	user, err := c.Auth().User()
//	c.Auth().Login(&user)
//	c.Auth().Logout()
//	if c.Auth().Check() { ... }
type Accessor struct {
	guard Guard
	c     *http.Context
}

// NewAccessor creates an auth accessor for the given guard and request context.
func NewAccessor(guard Guard, c *http.Context) *Accessor {
	return &Accessor{guard: guard, c: c}
}

func (a *Accessor) User() (any, error) {
	if a == nil || a.guard == nil {
		return nil, nil
	}
	return a.guard.User(a.c.Request.Context())
}

func (a *Accessor) Login(user any) error {
	if a == nil || a.guard == nil {
		return fmt.Errorf("auth: no guard configured")
	}
	u, ok := user.(User)
	if !ok {
		return fmt.Errorf("auth: user must implement auth.User (has GetID() string)")
	}
	return a.guard.Login(a.c.Request.Context(), u)
}

func (a *Accessor) Logout() error {
	if a == nil || a.guard == nil {
		return nil
	}
	return a.guard.Logout(a.c.Request.Context())
}

func (a *Accessor) Check() bool {
	u, err := a.User()
	return err == nil && u != nil
}

// UserFrom is a typed helper that gets the authenticated user with a single call.
//
//	user, err := auth.UserFrom[*models.User](c)
func UserFrom[T any](c *http.Context) (T, error) {
	var zero T
	if c.Auth() == nil {
		return zero, fmt.Errorf("auth: not configured (use auth.Init middleware)")
	}
	u, err := c.Auth().User()
	if err != nil {
		return zero, err
	}
	if u == nil {
		return zero, nil
	}
	typed, ok := u.(T)
	if !ok {
		return zero, fmt.Errorf("auth: user is %T, not %T", u, zero)
	}
	return typed, nil
}
