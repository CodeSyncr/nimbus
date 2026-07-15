# Passport Plugin (OAuth2 Server) - Nimbus

`plugins/passport` turns a Nimbus app into a full OAuth2 authorization server (modeled on Laravel Passport). Third-party and first-party clients obtain tokens to call your API on a user's behalf. Grants: `authorization_code` (with PKCE), `client_credentials`, `refresh_token`. Also RFC 7662 introspection and RFC 7009 revocation. Tokens are **opaque, stored SHA-256 hashed**, and fully revocable.

**Passport vs. Sanctum:** Use [access tokens (Sanctum)](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/auth.md) when your own app/SPA calls your API. Use Passport when *other people's* apps need delegated access via standard OAuth2.

## Directory Layout

```
plugins/passport/
  passport.go    // Plugin, NewPlugin(db, Config), container binding "passport", ViewsFS
  config.go      // Config{RoutePrefix, AccessTokenTTL, RefreshTokenTTL, AuthCodeTTL, ConsentView, AllowPublicClientsWithoutPKCE}
  server.go      // Server: CreateClient, ValidateAuthorize, IssueAuthCode, ExchangeAuthCode, ClientCredentials, Refresh, ValidateAccessToken, RevokeAccessToken; PKCE (S256/plain)
  routes.go      // /oauth/authorize (GET+POST), /token, /introspect, /revoke
  middleware.go  // RequireAccessToken(s), RequireScope(scope), AccessTokenFrom(ctx)
  models/        // OAuthClient, OAuthAuthCode, OAuthAccessToken, OAuthRefreshToken + Migrations()
  views/         // oauth-authorize.nimbus (consent screen)
```

## Installation

```go
import "github.com/CodeSyncr/nimbus/plugins/passport"
app.Use(passport.NewPlugin(db, passport.Config{}))
```

Migrates `oauth_clients`, `oauth_auth_codes`, `oauth_access_tokens`, `oauth_refresh_tokens`. Mounts endpoints under `/oauth` (configurable). Binds `passport` → `*passport.Server`. Defaults: access 1h, refresh 30d, code 10m, **PKCE required for public clients** (OAuth 2.1).

## Registering clients

```go
srv := app.Container.MustMake("passport").(*passport.Server)
res, _ := srv.CreateClient(ctx, "Acme", ownerUserID,
    []string{"https://acme.test/callback"},               // redirect allowlist (exact match)
    []string{passport.GrantAuthorizationCode, passport.GrantRefreshToken},
    []string{"read:profile", "write:posts"},              // allowed scopes ("*" = any)
    true)                                                  // confidential (false = public/PKCE)
// res.PlainSecret shown once (hashed at rest); res.Client.ClientID is the public id.
```

Client model flags: `Confidential` (public clients have no secret + must use PKCE), `FirstParty` (skips the consent screen — issues a code immediately), `Revoked`.

## Grant flows

- **authorization_code + PKCE:** client → `/oauth/authorize?response_type=code&client_id=&redirect_uri=&scope=&state=&code_challenge=&code_challenge_method=S256`. User logs in (else redirected to `/login?redirect=`), approves consent → redirect back with `?code=&state=`. Client POSTs `/oauth/token` with `grant_type=authorization_code`, `code`, `redirect_uri`, `code_verifier` → access + refresh tokens. Codes are single-use + short-lived.
- **client_credentials:** confidential clients only; no refresh token (RFC 6749 §4.4.3).
- **refresh_token:** rotates — old access + refresh tokens are revoked, new pair issued. Scopes can be narrowed, never widened.

Client auth at `/token` and `/introspect`: HTTP Basic auth **or** `client_id`/`client_secret` in the body.

## Protecting your API (resource server)

```go
srv := app.Container.MustMake("passport").(*passport.Server)
api := app.Router.Group("/api", passport.RequireAccessToken(srv)) // 401 + WWW-Authenticate on failure
api.Use(passport.RequireScope("read:profile"))                    // 403 insufficient_scope otherwise
api.Get("/me", func(c *http.Context) error {
    at := passport.AccessTokenFrom(c.Ctx()) // *models.OAuthAccessToken: UserID, Scopes, ClientID
    return c.JSON(200, map[string]string{"user": at.UserID})
})
```

## Errors

Server returns sentinel errors (`ErrInvalidClient`, `ErrInvalidGrant`, `ErrInvalidScope`, `ErrUnauthorized`, `ErrUnsupportedGrant`, `ErrInvalidRequest`); routes.go maps them to RFC 6749 codes (`invalid_client` → 401, others → 400) and redirect-based `?error=` for the authorize endpoint.

## Adding a grant / customization

Extend `Server` in server.go and dispatch in `routes.go handleToken`. The consent screen is `Config.ConsentView` (default `passport/oauth-authorize`), receiving `client_name`, `scopes []string`, `prefix`, and `params` (carried-through hidden fields).

**Tests:** `plugins/passport/server_test.go` (all grants, PKCE, rotation, revocation, scope enforcement) + `view_test.go` (consent render). Uses in-memory sqlite.
