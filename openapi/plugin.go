package openapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// ---------------------------------------------------------------------------
// Plugin
// ---------------------------------------------------------------------------

// PluginConfig configures the OpenAPI documentation plugin.
type PluginConfig struct {
	// Path prefix for the docs UI (default: "/_docs").
	Path string

	// Generator config for OpenAPI spec generation.
	Generator GeneratorConfig

	// Enabled controls whether docs are served (default true).
	Enabled bool

	// CustomCSS is injected into the Swagger UI page.
	CustomCSS string

	// HideModels hides the Models section in Swagger UI.
	HideModels bool

	// DeepLinking enables deep linking to operations.
	DeepLinking bool

	// TryItOutEnabled enables the "Try it out" feature by default.
	TryItOutEnabled bool
}

// Plugin serves OpenAPI/Swagger documentation as a Nimbus plugin.
type Plugin struct {
	config PluginConfig
	spec   []byte
	router *router.Router
}

// NewPlugin creates a new OpenAPI documentation plugin.
func NewPlugin(cfg ...PluginConfig) *Plugin {
	c := PluginConfig{
		Path:            "/_docs",
		Enabled:         true,
		DeepLinking:     true,
		TryItOutEnabled: true,
	}
	if len(cfg) > 0 {
		c = cfg[0]
		if c.Path == "" {
			c.Path = "/_docs"
		}
	}
	return &Plugin{config: c}
}

func (p *Plugin) Name() string    { return "openapi" }
func (p *Plugin) Version() string { return "1.0.0" }

func (p *Plugin) Register(app interface{ Container() interface{} }) error { return nil }

// Boot generates the OpenAPI spec and registers doc routes.
// This accepts the nimbus.App but we keep it interface-based to avoid import cycle.
func (p *Plugin) Boot(app interface{}) error { return nil }

// RegisterRoutes mounts the documentation endpoints on the router.
func (p *Plugin) RegisterRoutes(r *router.Router) {
	p.router = r

	prefix := strings.TrimSuffix(p.config.Path, "/")

	// Serve the OpenAPI JSON spec.
	r.Get(prefix+"/openapi.json", func(c *nhttp.Context) error {
		spec := p.generateSpec()
		c.Response.Header().Set("Content-Type", "application/json")
		c.Response.Header().Set("Access-Control-Allow-Origin", "*")
		_, err := c.Response.Write(spec)
		return err
	})

	// Serve the Swagger UI HTML.
	r.Get(prefix, func(c *nhttp.Context) error {
		c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := p.swaggerUIHTML(prefix + "/openapi.json")
		_, err := c.Response.Write([]byte(html))
		return err
	})

	// Serve Redoc alternative.
	r.Get(prefix+"/redoc", func(c *nhttp.Context) error {
		c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := p.redocHTML(prefix + "/openapi.json")
		_, err := c.Response.Write([]byte(html))
		return err
	})

	// Serve Scalar alternative.
	r.Get(prefix+"/scalar", func(c *nhttp.Context) error {
		c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := p.scalarHTML(prefix + "/openapi.json")
		_, err := c.Response.Write([]byte(html))
		return err
	})

	// Serve raw spec as YAML-ish pretty JSON.
	r.Get(prefix+"/spec", func(c *nhttp.Context) error {
		spec := p.generateSpec()
		c.Response.Header().Set("Content-Type", "application/json")
		c.Response.Header().Set("Access-Control-Allow-Origin", "*")
		// Pretty print
		var obj interface{}
		if err := json.Unmarshal(spec, &obj); err != nil {
			_, writeErr := c.Response.Write(spec)
			return writeErr
		}
		pretty, _ := json.MarshalIndent(obj, "", "  ")
		_, err := c.Response.Write(pretty)
		return err
	})
}

// generateSpec builds the OpenAPI JSON from the router's registered routes.
func (p *Plugin) generateSpec() []byte {
	if p.spec != nil {
		return p.spec
	}

	gen := NewGenerator(p.config.Generator)
	var routes []*router.Route
	if p.router != nil {
		routes = p.router.Routes()
	}
	spec, err := gen.JSON(routes)
	if err != nil {
		spec = []byte(`{"error":"` + err.Error() + `"}`)
	}

	// Cache the spec.
	p.spec = spec
	return spec
}

// InvalidateCache clears the cached spec so it's regenerated on next request.
func (p *Plugin) InvalidateCache() {
	p.spec = nil
}

// swaggerUIHTML returns an HTML page with embedded Swagger UI.
func (p *Plugin) swaggerUIHTML(specURL string) string {
	customCSS := p.config.CustomCSS
	if customCSS == "" {
		customCSS = nimbusDarkCSS()
	}

	tryItOut := "false"
	if p.config.TryItOutEnabled {
		tryItOut = "true"
	}
	deepLinking := "false"
	if p.config.DeepLinking {
		deepLinking = "true"
	}
	hideModels := ""
	if p.config.HideModels {
		hideModels = `defaultModelsExpandDepth: -1,`
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>API Documentation — Nimbus</title>
  <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    html { box-sizing: border-box; overflow-y: scroll; }
    *, *:before, *:after { box-sizing: inherit; }
    body { margin: 0; background: #1a1a2e; }
    .topbar { display: none; }
    %s
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-standalone-preset.js"></script>
  <script>
    SwaggerUIBundle({
      url: "%s",
      dom_id: '#swagger-ui',
      deepLinking: %s,
      tryItOutEnabled: %s,
      %s
      presets: [
        SwaggerUIBundle.presets.apis,
        SwaggerUIStandalonePreset
      ],
      plugins: [
        SwaggerUIBundle.plugins.DownloadUrl
      ],
      layout: "StandaloneLayout",
      requestInterceptor: (req) => {
        req.headers['X-Requested-With'] = 'SwaggerUI';
        return req;
      }
    });
  </script>
</body>
</html>`, customCSS, specURL, deepLinking, tryItOut, hideModels)
}

// redocHTML returns an HTML page with Redoc documentation viewer.
func (p *Plugin) redocHTML(specURL string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <title>API Documentation — Nimbus (Redoc)</title>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>body { margin: 0; padding: 0; }</style>
</head>
<body>
  <redoc spec-url='%s' theme='{"colors":{"primary":{"main":"#6366f1"}},"typography":{"fontFamily":"Inter, system-ui, sans-serif"}}'></redoc>
  <script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>
</body>
</html>`, specURL)
}

// scalarHTML returns an HTML page with Scalar API reference.
func (p *Plugin) scalarHTML(specURL string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <title>API Documentation — Nimbus (Scalar)</title>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1">
</head>
<body>
  <script id="api-reference" data-url="%s"></script>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</body>
</html>`, specURL)
}

// nimbusDarkCSS returns custom CSS for a Nimbus-branded dark Swagger UI.
func nimbusDarkCSS() string {
	return `
    .swagger-ui .topbar { background: #16213e; }
    .swagger-ui .info .title { color: #e2e8f0; font-size: 2em; }
    .swagger-ui .info { margin: 30px 0; }
    .swagger-ui .scheme-container { background: #16213e; box-shadow: none; }
    .swagger-ui .opblock.opblock-get { background: rgba(99,102,241,0.08); border-color: #6366f1; }
    .swagger-ui .opblock.opblock-get .opblock-summary-method { background: #6366f1; }
    .swagger-ui .opblock.opblock-post { background: rgba(34,197,94,0.08); border-color: #22c55e; }
    .swagger-ui .opblock.opblock-post .opblock-summary-method { background: #22c55e; }
    .swagger-ui .opblock.opblock-put { background: rgba(234,179,8,0.08); border-color: #eab308; }
    .swagger-ui .opblock.opblock-put .opblock-summary-method { background: #eab308; }
    .swagger-ui .opblock.opblock-delete { background: rgba(239,68,68,0.08); border-color: #ef4444; }
    .swagger-ui .opblock.opblock-delete .opblock-summary-method { background: #ef4444; }
    .swagger-ui .opblock.opblock-patch { background: rgba(168,85,247,0.08); border-color: #a855f7; }
    .swagger-ui .opblock.opblock-patch .opblock-summary-method { background: #a855f7; }
    .swagger-ui .btn.execute { background: #6366f1; border-color: #6366f1; }
    .swagger-ui .btn.execute:hover { background: #4f46e5; }
    .swagger-ui select { border: 1px solid #334155; background: #1e293b; color: #e2e8f0; }
    .swagger-ui input[type=text] { border: 1px solid #334155; background: #1e293b; color: #e2e8f0; }
    .swagger-ui textarea { background: #1e293b; color: #e2e8f0; border-color: #334155; }
    .swagger-ui .model-box { background: #1e293b; }
    .swagger-ui section.models { border: 1px solid #334155; }
    .swagger-ui section.models h4 { color: #e2e8f0; }
    .swagger-ui .model { color: #cbd5e1; }
    .swagger-ui .model-title { color: #e2e8f0; }
    .swagger-ui .response-col_status { color: #e2e8f0; }
    .swagger-ui table thead tr th { color: #94a3b8; border-bottom-color: #334155; }
    .swagger-ui table tbody tr td { border-bottom-color: #1e293b; color: #cbd5e1; }
    .swagger-ui .opblock-body pre.microlight { background: #0f172a !important; color: #e2e8f0; }
    .swagger-ui .highlight-code > .microlight { background: #0f172a !important; }
    .swagger-ui .opblock-description-wrapper p { color: #94a3b8; }
    .swagger-ui .opblock .opblock-summary-description { color: #94a3b8; }
    .swagger-ui .opblock .opblock-summary-path { color: #e2e8f0; }
    .swagger-ui .response-col_description__inner p { color: #cbd5e1; }
    .swagger-ui .parameter__name { color: #e2e8f0; }
    .swagger-ui .parameter__type { color: #94a3b8; }
    .swagger-ui .tab li button.tablinks { color: #94a3b8; }
    .swagger-ui .tab li button.tablinks.active { color: #e2e8f0; }
    .swagger-ui .copy-to-clipboard { background: #1e293b; }
    .swagger-ui .download-contents { background: #1e293b; color: #e2e8f0; }
    .swagger-ui .info .title small.version-stamp { background: #6366f1; }
    .swagger-ui .info a { color: #818cf8; }
    .swagger-ui .info p, .swagger-ui .info li { color: #94a3b8; }
    .swagger-ui .renderedMarkdown p { color: #94a3b8; }
    `
}

// Ensure Plugin satisfies http.Handler for Mount compatibility.
var _ http.Handler = (*PluginHandler)(nil)

// PluginHandler wraps the plugin as an http.Handler for mounting.
type PluginHandler struct {
	plugin *Plugin
	mux    *http.ServeMux
}

// NewPluginHandler creates an http.Handler that serves docs at the given prefix.
func NewPluginHandler(p *Plugin) *PluginHandler {
	h := &PluginHandler{plugin: p, mux: http.NewServeMux()}
	prefix := strings.TrimSuffix(p.config.Path, "/")

	h.mux.HandleFunc(prefix+"/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(p.generateSpec())
	})

	h.mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(p.swaggerUIHTML(prefix + "/openapi.json")))
	})

	h.mux.HandleFunc(prefix+"/redoc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(p.redocHTML(prefix + "/openapi.json")))
	})

	return h
}

func (h *PluginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}
