package auth

import (
	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// Init returns middleware that resolves the auth guard from the container and
// sets c.Auth() for AdonisJS-style access. Use as a global middleware:
//
//	app.Router.Use(auth.Init("auth.guard"))
//
// Then in any controller:
//
//	user, err := c.Auth().User()
//	c.Auth().Login(user)
//	c.Auth().Check()
//
// Or with the typed generic helper:
//
//	user, err := auth.UserFrom[*models.User](c)
func Init(containerKey ...string) router.Middleware {
	key := "auth.guard"
	if len(containerKey) > 0 && containerKey[0] != "" {
		key = containerKey[0]
	}
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			if c.Container != nil {
				if g, err := c.Container.Make(key); err == nil {
					if guard, ok := g.(Guard); ok {
						c.SetAuth(NewAccessor(guard, c))
					}
				}
			}
			return next(c)
		}
	}
}

// RequireAuth returns middleware that loads the user from the guard and sets it on the request context.
// If no user and redirectTo is non-empty, redirects; else returns 401.
func RequireAuth(guard Guard, redirectTo string) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			req := c.Request
			user, err := guard.User(req.Context())
			if err != nil {
				return err
			}
			if user != nil {
				req = req.WithContext(WithUser(req.Context(), user))
				c.Request = req
				return next(c)
			}
			if redirectTo != "" {
				c.Redirect(http.StatusFound, redirectTo)
				return nil
			}
			c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return nil
		}
	}
}
