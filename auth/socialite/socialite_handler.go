package socialite

import (
	"fmt"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
	"github.com/CodeSyncr/nimbus/session"
)

// CallbackFunc is called after a successful OAuth callback with the social user.
// The handler should create or find the local user, log them in, and redirect.
type CallbackFunc func(c *nhttp.Context, user *SocialUser) error

// RedirectHandler returns a handler that redirects the user to the OAuth provider.
// The provider name is read from the :provider route param.
//
// Example route: app.GET("/auth/:provider", socialite.RedirectHandler())
func (s *Socialite) RedirectHandler() router.HandlerFunc {
	return func(c *nhttp.Context) error {
		providerName := c.Param("provider")
		p, err := s.Provider(providerName)
		if err != nil {
			return c.JSON(nhttp.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("Unknown OAuth provider: %s", providerName),
			})
		}

		state := generateState()

		// Store state in session for CSRF verification
		sess := session.FromContext(c.Request.Context())
		if sess != nil {
			sess.Set("oauth_state", state)
		}

		authURL := p.AuthURL(state, nil)
		c.Redirect(nhttp.StatusFound, authURL)
		return nil
	}
}

// CallbackHandler returns a handler that processes the OAuth callback from
// the provider. It verifies the state parameter, exchanges the code for
// user information, and calls the provided callback function.
//
// Example route: app.GET("/auth/:provider/callback", socialite.CallbackHandler(myCallback))
func (s *Socialite) CallbackHandler(fn CallbackFunc) router.HandlerFunc {
	return func(c *nhttp.Context) error {
		providerName := c.Param("provider")
		p, err := s.Provider(providerName)
		if err != nil {
			return c.JSON(nhttp.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("Unknown OAuth provider: %s", providerName),
			})
		}

		// Verify state
		state := c.Request.URL.Query().Get("state")
		sess := session.FromContext(c.Request.Context())
		if sess != nil {
			expected, _ := sess.Get("oauth_state").(string)
			if expected != "" && expected != state {
				return c.JSON(nhttp.StatusBadRequest, map[string]string{
					"error": "Invalid OAuth state — possible CSRF attack",
				})
			}
			sess.Delete("oauth_state")
		}

		// Check for error from provider
		if errMsg := c.Request.URL.Query().Get("error"); errMsg != "" {
			desc := c.Request.URL.Query().Get("error_description")
			return c.JSON(nhttp.StatusBadRequest, map[string]string{
				"error":       errMsg,
				"description": desc,
			})
		}

		code := c.Request.URL.Query().Get("code")
		if code == "" {
			return c.JSON(nhttp.StatusBadRequest, map[string]string{
				"error": "Missing authorization code",
			})
		}

		// Exchange code for user info
		socialUser, err := p.Exchange(c.Request.Context(), code)
		if err != nil {
			return c.JSON(nhttp.StatusInternalServerError, map[string]string{
				"error": err.Error(),
			})
		}

		return fn(c, socialUser)
	}
}
