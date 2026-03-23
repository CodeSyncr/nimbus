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
