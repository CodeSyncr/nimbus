# Sanctum-style API tokens (personal access tokens)

Nimbus ships **database-backed Bearer tokens** similar to [Laravel Sanctum](https://laravel.com/docs/sanctum) API tokens (not SPA cookie mode).

## Setup

1. **Migration** — run the generator (adds `personal_access_tokens` + example controller):

   ```bash
   nimbus make:api-token
   nimbus db:migrate
   ```

   Or include the same migration when scaffolding auth with the **access_token** guard (`nimbus new` / `make:auth`); you still need the `personal_access_tokens` table.

2. **Guard** — in `start/kernel.go` (or equivalent):

   ```go
   tokenGuard := auth.NewTokenGuard(db, auth.UserLoaderFunc(func(ctx context.Context, id string) (auth.User, error) {
       var u models.User
       err := db.WithContext(ctx).First(&u, "id = ?", id).Error
       return &u, err
   }))
   ```

3. **Routes** — protect API groups:

   ```go
   api := app.Router.Group("/api")
   api.Use(auth.RequireToken(tokenGuard))
   api.Get("/user", func(c *http.Context) error {
       u, _ := auth.UserFromContext(c.Request.Context())
       return c.JSON(200, u)
   })
   ```

4. **Abilities** — optional scopes (JSON array string), checked with `auth.RequireAbility("posts:write")` **after** `RequireToken`:

   ```go
   nat, _ := tokenGuard.CreateToken(ctx, userID, "cli", `["read:posts","write:posts"]`, nil)
   // nat.PlainText — show once to the user
   ```

`PersonalAccessToken.HasAbility` parses abilities with `encoding/json` for correct escaping.

## Differences from Laravel Sanctum

- No first-party **SPA / session hybrid** in this package; use **session guard** for same-site browser apps and **token guard** for APIs.
- Token CRUD beyond `CreateToken` / `RevokeToken` / `ListTokens` is up to your controllers (see `make:api-token` scaffold).
