package inertia

import "io/fs"

// Config holds Inertia.js plugin configuration.
type Config struct {
	// URL is the application URL (e.g. "http://localhost:3000").
	// Used for redirects and asset URLs.
	URL string

	// RootTemplate is the path to the root HTML template.
	// Must contain a div with id="app" and data-page="{{ marshal .page }}".
	// Example: "resources/views/app.html"
	// If empty and TemplateFS is nil, uses the embedded default.
	RootTemplate string

	// TemplateFS is an optional embed.FS for the root template.
	// When set, RootTemplate is relative to this filesystem.
	TemplateFS fs.FS

	// Version is the asset version for cache busting.
	// Change this when assets change to force client reload.
	Version string

	// SSRURL is the optional Server-Side Rendering Node server URL.
	// When set, Inertia will use SSR for initial page load.
	SSRURL string
}

// DefaultConfig returns sensible defaults for development.
func DefaultConfig() Config {
	return Config{
		URL:          "http://localhost:3000",
		RootTemplate: "resources/views/app.html",
		Version:      "1",
	}
}
