# Authentication & Policies - Nimbus

Nimbus provides a flexible authentication system with support for multiple guards, providers, and policies.

## Auth System

The `auth` package defines the `Guard` interface for authenticating requests and managing sessions.

### Key Components

-   **User Interface**: Apps implement the `User` interface (`GetID() string`).
-   **Guards**: Authenticate requests (e.g., `SessionGuard`, `TokenGuard`).
    -   `User(ctx)`: Returns the currently authenticated user.
    -   `Login(ctx, user)`: Log in a user.
    -   `Logout(ctx)`: Log out the current user.

## Guards

### Stateless Guard (JWT/PASETO)
Authenticates users via stateless tokens. Supports JWT (HMAC) and PASETO (V4 Local).
```go
import "github.com/CodeSyncr/nimbus/auth"

// Initialize with a driver (JWT or PASETO)
driver := auth.NewJWTDriver("my-secret")
// OR: driver := auth.NewPasetoDriver("my-32-byte-hex-key")

guard := auth.NewStatelessGuard(driver, myUserLoader)

// Middleware usage
app.Router.Use(auth.RequireStatelessToken(guard))
```

#### Token Generation
```go
token, err := guard.GenerateToken(user.GetID(), 24 * time.Hour)
```

### Session Guard
Uses the `session` middleware to persist user ID in the session.
```go
import "github.com/CodeSyncr/nimbus/auth"

guard := auth.NewSessionGuardWithLoader(myUserLoader)
app.Router.Use(auth.RequireAuth(guard, "/login"))
```

### Token Guard (Opaque/Stateful)
Authenticates users via API tokens stored in the database (Personal Access Tokens).
```go
import "github.com/CodeSyncr/nimbus/auth"

guard := auth.NewTokenGuard(db, myUserLoader)
app.Router.Use(auth.RequireToken(guard))
```

#### Token abilities / scopes (Sanctum-style)
`PersonalAccessToken.Abilities` is a JSON array (e.g. `["read:posts","write:posts"]`); `["*"]` or empty = all. Check on the token record or gate routes with middleware (all must run **after** `RequireToken`):
```go
// On the token record (auth.CurrentToken(ctx)):
pat.HasAbility("read:posts")
pat.HasAnyAbility("read:posts", "admin")   // OR — at least one
pat.HasAllAbilities("read:posts", "write:posts") // AND — every one

// Route middleware:
api.Use(auth.RequireAbility("read:posts"))              // single ability
api.Use(auth.RequireAnyAbility("read:posts", "admin"))  // OR (Sanctum `abilities:`)
api.Use(auth.RequireAllAbilities("read", "write"))      // AND (Sanctum `ability:`)
```
All return **403** with a descriptive error when the token lacks the ability. Create tokens with abilities: `guard.CreateToken(ctx, userID, name, `["read:posts"]`, expiresAt)`.

> For issuing OAuth2 tokens to *third-party* apps (delegated access, consent, grants), use the [Passport plugin](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/passport_plugin.md) instead.

### Multi-Guard — try web & API guards on the same route
Nimbus does not ship a combined guard, but you can write one using the `Guard` interface.
`User(ctx)` returns `nil, nil` (not an error) when a guard finds no user, so walk guards in order:
```go
// app/middleware/any_guard.go
func AnyGuard(redirectTo string, guards ...auth.Guard) router.Middleware {
    return func(next router.HandlerFunc) router.HandlerFunc {
        return func(c *http.Context) error {
            for _, g := range guards {
                user, err := g.User(c.Request.Context())
                if err != nil {
                    return err
                }
                if user != nil {
                    c.Request = c.Request.WithContext(auth.WithUser(c.Request.Context(), user))
                    return next(c)
                }
            }
            if redirectTo != "" {
                c.Redirect(http.StatusFound, redirectTo)
                return nil
            }
            return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
        }
    }
}

// Usage in routes.go — accepts both browser sessions AND API tokens
app.Router.Get("/dashboard", dashboardHandler).
    Use(middleware.AnyGuard("", sessionGuard, tokenGuard))
```
The user is then retrieved identically regardless of which guard matched:
```go
user := auth.UserFromContext(c.Request.Context())
appUser := user.(*models.User)
```



## Policies

Policies are used to authorize actions based on the authenticated user.

### Defining a Policy

```go
auth.DefinePolicy("delete-post", func(user User, post *Post) bool {
    return user.GetID() == post.UserID
})
```

### Using a Policy

In a controller:
```go
if !auth.Can(c.Request.Context(), "delete-post", post) {
    return c.Status(http.StatusForbidden).JSON(map[string]string{"error": "Unauthorized"})
}
```

## Socialite

The `auth/socialite` package provides a unified interface for OAuth2 authentication with providers like GitHub, Google, LinkedIn, etc.

### Usage

1.  Redirect to provider: `return socialite.Driver("github").Redirect(c)`
2.  Handle callback: `user, _ := socialite.Driver("github").User(c)`

## Best Practices

1.  **Use Loaders**: Always use a `UserLoader` to fetch users from the database during authentication.
2.  **Context Extraction**: Use `auth.UserFromContext(ctx)` in your services and controllers.
3.  **Strict Policies**: Define granular policies for any sensitive action.
