# Supabase Plugin - Nimbus

The Supabase plugin provides deep integration with Supabase services including Authentication (GoTrue), Database queries & RPC (PostgREST), Realtime socket subscriptions, and JWT verification middleware.

## Installation & Setup

1. **Scaffolding**: When using `nimbus create` to scaffold a project, you can select **Supabase** as the database driver, which automatically configures the plugin.
2. **Environment Variables**:
   ```env
   SUPABASE_URL=https://your-project.supabase.co
   SUPABASE_ANON_KEY=your-anon-key
   SUPABASE_SERVICE_ROLE_KEY=your-service-role-key
   SUPABASE_JWT_SECRET=your-jwt-secret
   ```

3. **Registration** (`bin/server.go`):
   ```go
   app.Use(supabase.New())
   ```

---

## Authentication (`GoTrue`)

Manage user signup, login, password resets, and session management using the auth client:

```go
auth := supabase.Get().Auth

// Sign up a new user
response, err := auth.SignUp(ctx, "user@example.com", "password123")

// Sign in a user
session, err := auth.SignInWithPassword(ctx, "user@example.com", "password123")
```

---

## Database & RPC Calls (`PostgREST`)

Interact with the database. The client exposes arbitrary Database Function calls (RPC) for custom operations:

```go
client := supabase.Get()

// Call a database RPC function
var result MyCustomStruct
err := client.Rpc(ctx, "get_active_users", map[string]any{"limit": 10}, &result)
```

---

## Realtime Client

Subscribe to database changes, broadcasts, or presence updates:

```go
client := supabase.Get()

// Create and join a channel
channel := client.Realtime.Channel("room_1")
err := channel.Subscribe(func(event string, payload map[string]any) {
    fmt.Printf("Received realtime event %s: %v\n", event, payload)
})
```

---

## Authentication Middleware

The plugin includes JWT verification middleware to secure API endpoints:

```go
// Secure a route group with Supabase JWT validation
router.Group("/api", func(r *router.Router) {
    r.Use(supabase.VerifySupabaseJWT())
    r.Get("/profile", ProfileController)
})
```

---

## Health Check Integration

The plugin registers a health check endpoint checking the reachability of the configured Supabase endpoint:
- **Identifier**: `supabase`
- **Behavior**: Returns status `OK` if Supabase URL responds to health queries.
