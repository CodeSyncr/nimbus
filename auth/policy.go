package auth

import "context"

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
