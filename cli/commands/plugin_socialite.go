package commands

// socialiteServerInsert is added to bin/server.go when socialite is installed.
const socialiteServerInsert = `	app.Use(socialite.NewPlugin(socialite.Config{
		Providers: config.SocialiteProviders(),
	}, func(c *nhttp.Context, user *socialite.SocialUser) error {
		// TODO: Find or create user in database, then log them in.
		// Example:
		//   dbUser, _ := findOrCreateUser(user)
		//   guard.Login(c.Request.Context(), dbUser)
		return c.Redirect(nhttp.StatusSeeOther, "/dashboard")
	}))
`

// socialiteConfigFile is scaffolded at config/socialite.go when socialite is installed.
const socialiteConfigFile = `/*
|--------------------------------------------------------------------------
| Social Authentication Configuration
|--------------------------------------------------------------------------
|
| Configure your OAuth providers here. Each provider needs a Client ID,
| Client Secret, and Redirect URL from the provider's developer console.
|
| Supported providers: GitHub, Google, Discord, Apple
|
| Setup guides:
|   GitHub  → https://github.com/settings/developers
|   Google  → https://console.cloud.google.com/apis/credentials
|   Discord → https://discord.com/developers/applications
|   Apple   → https://developer.apple.com/account/resources/identifiers
|
| Environment variables:
|   GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET, GITHUB_REDIRECT_URL
|   GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, GOOGLE_REDIRECT_URL
|   DISCORD_CLIENT_ID, DISCORD_CLIENT_SECRET, DISCORD_REDIRECT_URL
|   APPLE_CLIENT_ID, APPLE_CLIENT_SECRET, APPLE_REDIRECT_URL, APPLE_TEAM_ID
|
*/

package config

import (
	"os"

	"github.com/CodeSyncr/nimbus/auth/socialite"
)

// SocialiteProviders returns the configured OAuth providers.
// Provider entries are only included when their Client ID is set,
// so you can enable/disable providers via environment variables.
func SocialiteProviders() map[string]socialite.ProviderConfig {
	providers := make(map[string]socialite.ProviderConfig)

	// ── GitHub ──────────────────────────────────────────────
	if id := os.Getenv("GITHUB_CLIENT_ID"); id != "" {
		providers["github"] = socialite.ProviderConfig{
			ClientID:     id,
			ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
			RedirectURL:  envOr("GITHUB_REDIRECT_URL", "http://localhost:3333/auth/github/callback"),
			Scopes:       []string{"user:email"},
		}
	}

	// ── Google ──────────────────────────────────────────────
	if id := os.Getenv("GOOGLE_CLIENT_ID"); id != "" {
		providers["google"] = socialite.ProviderConfig{
			ClientID:     id,
			ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
			RedirectURL:  envOr("GOOGLE_REDIRECT_URL", "http://localhost:3333/auth/google/callback"),
			Scopes:       []string{"openid", "email", "profile"},
		}
	}

	// ── Discord ─────────────────────────────────────────────
	if id := os.Getenv("DISCORD_CLIENT_ID"); id != "" {
		providers["discord"] = socialite.ProviderConfig{
			ClientID:     id,
			ClientSecret: os.Getenv("DISCORD_CLIENT_SECRET"),
			RedirectURL:  envOr("DISCORD_REDIRECT_URL", "http://localhost:3333/auth/discord/callback"),
			Scopes:       []string{"identify", "email"},
		}
	}

	// ── Apple ───────────────────────────────────────────────
	if id := os.Getenv("APPLE_CLIENT_ID"); id != "" {
		providers["apple"] = socialite.ProviderConfig{
			ClientID:     id,
			ClientSecret: os.Getenv("APPLE_CLIENT_SECRET"),
			RedirectURL:  envOr("APPLE_REDIRECT_URL", "http://localhost:3333/auth/apple/callback"),
			Scopes:       []string{"name", "email"},
		}
	}

	return providers
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
`
