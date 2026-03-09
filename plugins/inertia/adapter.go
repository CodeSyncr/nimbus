package inertia

import (
	"embed"
	"net/http"
	"os"

	"github.com/petaki/inertia-go"
)

//go:embed template
var defaultTemplateFS embed.FS

// petakiAdapter wraps github.com/petaki/inertia-go for use with the Plugin.
type petakiAdapter struct {
	inner *inertia.Inertia
}

func (a *petakiAdapter) Middleware(next interface{}) interface{} {
	if h, ok := next.(http.Handler); ok {
		wrapped := a.inner.Middleware(h)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			viteDev := os.Getenv("VITE_DEV") == "1" || os.Getenv("VITE_DEV") == "true"
			ctx := a.inner.WithViewData(r.Context(), "viteDev", viteDev)
			wrapped.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	return next
}

func (a *petakiAdapter) Render(w, r interface{}, component string, props map[string]any) error {
	respW, ok := w.(http.ResponseWriter)
	if !ok {
		return nil
	}
	req, ok := r.(*http.Request)
	if !ok {
		return nil
	}
	return a.inner.Render(respW, req, component, props)
}

// createManager builds the Inertia manager from plugin config.
func (p *Plugin) createManager() (Manager, error) {
	url := p.config.URL
	if url == "" {
		url = "http://localhost:3000"
	}
	root := p.config.RootTemplate
	if root == "" {
		root = "template/app.html"
	}
	version := p.config.Version

	var inner *inertia.Inertia
	if p.config.TemplateFS != nil {
		inner = inertia.NewWithFS(url, root, version, p.config.TemplateFS)
	} else if _, err := os.Stat(root); err == nil {
		inner = inertia.New(url, root, version)
	} else {
		// Use embedded default template
		inner = inertia.NewWithFS(url, "template/app.html", version, defaultTemplateFS)
	}

	if p.config.SSRURL != "" {
		inner.EnableSsr(p.config.SSRURL)
	}

	return &petakiAdapter{inner: inner}, nil
}

// wrapHandler wraps the HTTP handler with Inertia middleware.
func (p *Plugin) wrapHandler(h http.Handler) http.Handler {
	if wrapped := p.manager.Middleware(h); wrapped != nil {
		if mw, ok := wrapped.(http.Handler); ok {
			return mw
		}
	}
	return h
}
