package commands

const telescopeConfigFile = `/*
|--------------------------------------------------------------------------
| Telescope Configuration
|--------------------------------------------------------------------------
|
| Telescope is an elegant debug assistant for your Nimbus app.
| It records requests, queries, jobs, mail, events, and more.
|
| Telescope is enabled by default in development and disabled
| in production. You can override this via the TELESCOPE_ENABLED
| env variable.
|
| See: /docs/telescope
|
*/

package config

var Telescope TelescopeConfig

type TelescopeConfig struct {
	// Enabled controls whether Telescope records entries.
	// Default: true in development, false in production.
	Enabled bool

	// Path is the URL where the Telescope dashboard is served.
	// Default: "/telescope"
	Path string

	// MaxEntries is the maximum number of entries the in-memory
	// ring buffer keeps. Older entries are discarded when full.
	MaxEntries int

	// Watchers controls which entry types Telescope records.
	// Set individual watchers to false to reduce noise.
	Watchers TelescopeWatchers
}

type TelescopeWatchers struct {
	Requests      bool
	Queries       bool
	Models        bool
	Commands      bool
	Schedule      bool
	Jobs          bool
	Events        bool
	Mail          bool
	Notifications bool
	Cache         bool
	Redis         bool
	Gate          bool
	Exceptions    bool
	Logs          bool
	Views         bool
	HTTPClient    bool
}

func loadTelescope() {
	isDev := env("APP_ENV", "development") != "production"

	Telescope = TelescopeConfig{
		Enabled:    envBool("TELESCOPE_ENABLED", isDev),
		Path:       env("TELESCOPE_PATH", "/telescope"),
		MaxEntries: envInt("TELESCOPE_MAX_ENTRIES", 100),

		Watchers: TelescopeWatchers{
			Requests:      true,
			Queries:       true,
			Models:        true,
			Commands:      true,
			Schedule:      true,
			Jobs:          true,
			Events:        true,
			Mail:          true,
			Notifications: true,
			Cache:         true,
			Redis:         true,
			Gate:          true,
			Exceptions:    true,
			Logs:          true,
			Views:         true,
			HTTPClient:    true,
		},
	}
}
`
