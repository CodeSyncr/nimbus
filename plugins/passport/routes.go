package passport

import (
	"errors"
	"net/url"
	"strings"

	"github.com/CodeSyncr/nimbus/auth"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// RegisterRoutes mounts the OAuth2 endpoints under the configured prefix:
//
//	GET  /oauth/authorize   consent screen (user must be logged in)
//	POST /oauth/authorize   approve/deny → redirect back with ?code=
//	POST /oauth/token       exchange grant for tokens
//	POST /oauth/introspect  RFC 7662 token introspection (client-authenticated)
//	POST /oauth/revoke      RFC 7009 token revocation
func (p *Plugin) RegisterRoutes(r *router.Router) {
	pfx := p.server.cfg.RoutePrefix
	r.Get(pfx+"/authorize", p.handleAuthorizeGet)
	r.Post(pfx+"/authorize", p.handleAuthorizePost)
	r.Post(pfx+"/token", p.handleToken)
	r.Post(pfx+"/introspect", p.handleIntrospect)
	r.Post(pfx+"/revoke", p.handleRevoke)
}

// handleAuthorizeGet validates the request and renders the consent screen.
// First-party clients skip consent and get a code immediately.
func (p *Plugin) handleAuthorizeGet(c *nhttp.Context) error {
	q := c.Request.URL.Query()
	req, err := p.server.ValidateAuthorize(c.Ctx(),
		q.Get("client_id"), q.Get("response_type"), q.Get("redirect_uri"),
		q.Get("scope"), q.Get("state"), q.Get("code_challenge"), q.Get("code_challenge_method"))
	if err != nil {
		return oauthRedirectError(c, q.Get("redirect_uri"), q.Get("state"), err)
	}

	user := auth.UserFromContext(c.Ctx())
	if user == nil {
		// Not logged in — bounce to login, preserving the authorize URL.
		c.Redirect(nhttp.StatusFound, "/login?redirect="+url.QueryEscape(c.Request.URL.String()))
		return nil
	}

	// First-party clients are implicitly trusted: issue immediately.
	if req.Client.FirstParty {
		return p.issueAndRedirect(c, req, user.GetID())
	}

	return c.View(p.server.cfg.ConsentView, map[string]any{
		"client_name": req.Client.Name,
		"scopes":      req.Scopes,
		"params":      authorizeHiddenFields(q),
		"prefix":      p.server.cfg.RoutePrefix,
	})
}

// handleAuthorizePost processes the consent form. approve=1 issues a code.
func (p *Plugin) handleAuthorizePost(c *nhttp.Context) error {
	_ = c.Request.ParseForm()
	f := c.Request.Form
	req, err := p.server.ValidateAuthorize(c.Ctx(),
		f.Get("client_id"), f.Get("response_type"), f.Get("redirect_uri"),
		f.Get("scope"), f.Get("state"), f.Get("code_challenge"), f.Get("code_challenge_method"))
	if err != nil {
		return oauthRedirectError(c, f.Get("redirect_uri"), f.Get("state"), err)
	}
	user := auth.UserFromContext(c.Ctx())
	if user == nil {
		return c.JSON(nhttp.StatusUnauthorized, map[string]string{"error": "login_required"})
	}
	if f.Get("approve") != "1" {
		return oauthRedirectError(c, req.RedirectURI, req.State, errors.New("access_denied"))
	}
	return p.issueAndRedirect(c, req, user.GetID())
}

// issueAndRedirect mints an auth code and 302s back to the client redirect URI.
func (p *Plugin) issueAndRedirect(c *nhttp.Context, req *AuthorizeRequest, userID string) error {
	code, err := p.server.IssueAuthCode(c.Ctx(), req, userID)
	if err != nil {
		return oauthRedirectError(c, req.RedirectURI, req.State, ErrInvalidRequest)
	}
	u, _ := url.Parse(req.RedirectURI)
	qs := u.Query()
	qs.Set("code", code)
	if req.State != "" {
		qs.Set("state", req.State)
	}
	u.RawQuery = qs.Encode()
	c.Redirect(nhttp.StatusFound, u.String())
	return nil
}

// handleToken dispatches on grant_type and returns the token response.
func (p *Plugin) handleToken(c *nhttp.Context) error {
	_ = c.Request.ParseForm()
	f := c.Request.Form
	clientID, clientSecret := clientCredentials(c, f)

	var (
		resp *TokenResponse
		err  error
	)
	switch f.Get("grant_type") {
	case GrantAuthorizationCode:
		resp, err = p.server.ExchangeAuthCode(c.Ctx(), clientID, clientSecret,
			f.Get("code"), f.Get("redirect_uri"), f.Get("code_verifier"))
	case GrantClientCredentials:
		resp, err = p.server.ClientCredentials(c.Ctx(), clientID, clientSecret, f.Get("scope"))
	case GrantRefreshToken:
		resp, err = p.server.Refresh(c.Ctx(), clientID, clientSecret, f.Get("refresh_token"), f.Get("scope"))
	default:
		err = ErrUnsupportedGrant
	}
	if err != nil {
		return oauthTokenError(c, err)
	}
	// Tokens must never be cached (RFC 6749 §5.1).
	c.Response.Header().Set("Cache-Control", "no-store")
	c.Response.Header().Set("Pragma", "no-cache")
	return c.JSON(nhttp.StatusOK, resp)
}

// handleIntrospect implements RFC 7662. The caller must authenticate as a client.
func (p *Plugin) handleIntrospect(c *nhttp.Context) error {
	_ = c.Request.ParseForm()
	f := c.Request.Form
	clientID, clientSecret := clientCredentials(c, f)
	if _, err := p.server.authenticateClient(c.Ctx(), clientID, clientSecret); err != nil {
		return c.JSON(nhttp.StatusUnauthorized, map[string]string{"error": "invalid_client"})
	}
	at, err := p.server.ValidateAccessToken(c.Ctx(), f.Get("token"))
	if err != nil {
		return c.JSON(nhttp.StatusOK, map[string]any{"active": false})
	}
	return c.JSON(nhttp.StatusOK, map[string]any{
		"active":     true,
		"scope":      at.Scopes,
		"client_id":  at.ClientID,
		"sub":        at.UserID,
		"exp":        at.ExpiresAt.Unix(),
		"token_type": "Bearer",
	})
}

// handleRevoke implements RFC 7009 (best-effort; always 200).
func (p *Plugin) handleRevoke(c *nhttp.Context) error {
	_ = c.Request.ParseForm()
	_ = p.server.RevokeAccessToken(c.Ctx(), c.Request.Form.Get("token"))
	return c.JSON(nhttp.StatusOK, map[string]string{"revoked": "true"})
}

// ── request helpers ────────────────────────────────────────────────

// clientCredentials extracts client_id/secret from HTTP Basic auth (preferred)
// or the request body.
func clientCredentials(c *nhttp.Context, f url.Values) (id, secret string) {
	if u, pw, ok := c.Request.BasicAuth(); ok {
		return u, pw
	}
	return f.Get("client_id"), f.Get("client_secret")
}

// authorizeHiddenFields carries the authorize params through the consent form
// so the POST re-validates against the exact same request. Returned as an
// ordered slice of {name,value} maps the consent template can @each over.
func authorizeHiddenFields(q url.Values) []map[string]string {
	keys := []string{"client_id", "response_type", "redirect_uri", "scope", "state", "code_challenge", "code_challenge_method"}
	out := make([]map[string]string, 0, len(keys))
	for _, k := range keys {
		if v := q.Get(k); v != "" {
			out = append(out, map[string]string{"name": k, "value": v})
		}
	}
	return out
}

// oauthTokenError maps a server error to an RFC 6749 token-endpoint response.
func oauthTokenError(c *nhttp.Context, err error) error {
	code := errorCode(err)
	status := nhttp.StatusBadRequest
	if code == "invalid_client" {
		status = nhttp.StatusUnauthorized
	}
	return c.JSON(status, map[string]string{"error": code})
}

// oauthRedirectError sends the user-agent back to the client with ?error= when
// a redirect_uri is known; otherwise renders a plain error.
func oauthRedirectError(c *nhttp.Context, redirectURI, state string, err error) error {
	code := errorCode(err)
	if redirectURI == "" {
		return c.JSON(nhttp.StatusBadRequest, map[string]string{"error": code})
	}
	u, perr := url.Parse(redirectURI)
	if perr != nil {
		return c.JSON(nhttp.StatusBadRequest, map[string]string{"error": "invalid_request"})
	}
	qs := u.Query()
	qs.Set("error", code)
	if state != "" {
		qs.Set("state", state)
	}
	u.RawQuery = qs.Encode()
	c.Redirect(nhttp.StatusFound, u.String())
	return nil
}

// errorCode reduces a server error to its OAuth error code string.
func errorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidClient):
		return "invalid_client"
	case errors.Is(err, ErrInvalidGrant):
		return "invalid_grant"
	case errors.Is(err, ErrInvalidScope):
		return "invalid_scope"
	case errors.Is(err, ErrUnauthorized):
		return "unauthorized_client"
	case errors.Is(err, ErrUnsupportedGrant):
		return "unsupported_grant_type"
	case err != nil && strings.Contains(err.Error(), "access_denied"):
		return "access_denied"
	default:
		return "invalid_request"
	}
}
