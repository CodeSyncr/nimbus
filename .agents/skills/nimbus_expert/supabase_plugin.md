# Supabase Plugin - Nimbus

The Supabase plugin provides deep integration with Supabase services including Authentication (GoTrue), Database queries & RPC (PostgREST), Realtime socket subscriptions, Storage, Edge Functions, and JWT verification middleware.

## Installation & Setup

1. **Scaffolding**: When using `nimbus create` to scaffold a project, you can select **Supabase** as the database driver, which automatically configures the plugin.
2. **Environment Variables** (all required except `SUPABASE_DB_URL`):
   ```env
   SUPABASE_URL=https://your-project.supabase.co
   SUPABASE_ANON_KEY=your-anon-key
   SUPABASE_SERVICE_ROLE_KEY=your-service-role-key
   SUPABASE_JWT_SECRET=your-jwt-secret
   SUPABASE_DB_URL=postgres://postgres:pass@db.xyz.supabase.co:5432/postgres
   ```
   > **Important**: `SUPABASE_JWT_SECRET` is **required** for the auth middleware to work. It is the HMAC signing secret found in Supabase Dashboard → Settings → API → JWT Secret. Do NOT confuse it with the anon key.

3. **Registration** (`bin/server.go`):
   ```go
   import "github.com/CodeSyncr/nimbus/plugins/supabase"

   app.Use(supabase.New(nil)) // uses ConfigFromEnv()
   // or with explicit config:
   app.Use(supabase.New(&supabase.Config{
       URL:            "https://xyz.supabase.co",
       AnonKey:        "...",
       ServiceRoleKey: "...",
       JWTSecret:      "...",
       DatabaseURL:    "postgres://...",
   }))
   ```

---

## Authentication (`GoTrue`)

Manage user signup, login, password resets, and session management using the auth client:

```go
client := supabase.GetClient()

// Sign up a new user
session, err := client.Auth.SignUp(supabase.SignUpRequest{
    Email:    "user@example.com",
    Password: "password123",
})

// Sign in with email and password
session, err := client.Auth.SignInWithPassword(supabase.SignInRequest{
    Email:    "user@example.com",
    Password: "password123",
})

// Get user from access token
user, err := client.Auth.GetUser(session.AccessToken)

// Update user attributes
updated, err := client.Auth.UpdateUser(session.AccessToken, supabase.UpdateUserRequest{
    Email: "new@example.com",
    Data:  map[string]any{"name": "New Name"},
})

// Refresh tokens
newSession, err := client.Auth.RefreshToken(session.RefreshToken)

// Sign out
err = client.Auth.SignOut(session.AccessToken)
```

### Admin Methods (require ServiceRoleKey)

```go
// List users
users, err := client.Auth.AdminListUsers(1, 50)

// Get user by ID
user, err := client.Auth.AdminGetUser("user-uuid")

// Update user by ID
user, err := client.Auth.AdminUpdateUser("user-uuid", supabase.AdminUpdateUserRequest{
    Email: "admin@example.com",
})

// Delete user
err = client.Auth.AdminDeleteUser("user-uuid")

// Invite user by email
user, err := client.Auth.InviteByEmail(supabase.InviteRequest{
    Email: "invite@example.com",
})

// Generate admin link (confirm, recovery, etc.)
result, err := client.Auth.GenerateLink("signup", "user@example.com", nil)
```

---

## Authentication Middleware

The plugin includes JWT verification middleware to secure API endpoints. **Requires `SUPABASE_JWT_SECRET` to be set.**

```go
import "github.com/CodeSyncr/nimbus/plugins/supabase"

// Secure a route group with Supabase JWT validation
protected := app.Router.Group("/api", supabase.AuthMiddleware())
protected.Get("/profile", func(ctx *http.Context) error {
    claims := supabase.GetClaims(ctx)
    userID := supabase.GetUserID(ctx)
    return ctx.JSON(200, map[string]string{"user_id": userID})
})

// Optional auth — injects claims if token exists but doesn't reject
public := app.Router.Group("/public", supabase.OptionalAuthMiddleware())

// Named middleware registration (via plugin):
// "supabase.auth" and "supabase.auth.optional"
```

---

## Database & RPC Calls (`PostgREST`)

```go
client := supabase.GetClient()

// Call a database RPC function
var result []map[string]any
err := client.Rpc("get_active_users", map[string]any{"min_age": 18}, &result)
```

---

## Realtime Client

Subscribe to database changes, broadcasts, or presence updates. The client includes **automatic reconnection** with exponential backoff.

```go
client := supabase.GetClient()

// Optional: set error handler for connection issues
client.Realtime.OnError = func(err error) {
    log.Printf("realtime error: %v", err)
}

// Connect to the WebSocket server
err := client.Realtime.Connect()
if err != nil {
    return err
}
defer client.Realtime.Close()

// Create and join a channel
channel := client.Realtime.Channel("room-1")

// Subscribe to Postgres changes
channel.OnPostgresChanges(supabase.EventInsert, func(payload supabase.ChangePayload) {
    fmt.Printf("New row in %s: %v\n", payload.Table, payload.Record)
})

// Listen to Broadcast messages
channel.OnBroadcast("chat-message", func(payload supabase.BroadcastPayload) {
    fmt.Printf("Broadcast: %v\n", payload.Payload)
})

// Track Presence
channel.OnPresence(func(payload supabase.PresencePayload) {
    fmt.Printf("Presence: %s\n", payload.Key)
})

// Subscribe to the channel
err = channel.Subscribe()

// Send a broadcast
err = channel.Broadcast("chat-message", map[string]any{"text": "hello"})
```

---

## Storage

```go
client := supabase.GetClient()
bucket := client.Storage.From("avatars")

// Upload
err := bucket.Upload("users/123/profile.png", file, "image/png")

// Download
reader, err := bucket.Download("users/123/profile.png")
defer reader.Close()

// Signed URL (1 hour)
url, err := bucket.CreateSignedURL("users/123/profile.png", time.Hour)

// Public URL
url := bucket.GetPublicURL("users/123/profile.png")

// List files
objects, err := bucket.List("users/", supabase.WithLimit(50))

// Delete
err = bucket.Remove([]string{"users/123/profile.png"})
```

---

## Edge Functions

```go
client := supabase.GetClient()

// Invoke an edge function with JSON payload
resp, err := client.Functions.Invoke("send-email", map[string]any{
    "to": "user@example.com",
})
fmt.Println(resp.Text())

// Invoke with raw body
resp, err = client.Functions.InvokeRaw("process", reader, "text/plain")
```

---

## Health Check Integration

The plugin registers a health check endpoint checking the reachability of the configured Supabase endpoint:
- **Identifier**: `supabase`
- **Behavior**: Returns status `OK` if Supabase URL responds to health queries.

Optional HTTP handler:
```go
app.Router.Get("/supabase/health", supabase.HealthHandler())
```
