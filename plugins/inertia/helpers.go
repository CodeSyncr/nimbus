package inertia

import (
	"github.com/CodeSyncr/nimbus/http"
)

const contextKey = "inertiaProps"

// Render renders an Inertia page. Use in handlers to return Vue/React/Svelte
// component data. When the request has the X-Inertia header, returns JSON.
// Otherwise returns full HTML with the root template.
//
//	func (c *HomeController) Index(ctx *http.Context) error {
//	    users := loadUsers()
//	    return inertia.Render(ctx, "Home/Index", map[string]any{
//	        "users": users,
//	    })
//	}
func Render(c *http.Context, component string, props map[string]any) error {
	mgr := getManager()
	if mgr == nil {
		return c.View("error", map[string]any{
			"message": "Inertia plugin not loaded",
		})
	}

	// Merge request-scoped props
	if requestProps, ok := c.Get(contextKey); ok {
		if rpMap, ok := requestProps.(map[string]any); ok {
			if props == nil {
				props = make(map[string]any)
			}
			for k, v := range rpMap {
				if _, exists := props[k]; !exists {
					props[k] = v
				}
			}
		}
	}

	return mgr.Render(c.Response, c.Request, component, props)
}

// Share shares a prop globally for all Inertia responses.
// WARNING: Use only for static data that never changes per-request.
// For request-specific data (user, flash, etc.), use ShareProp instead.
func Share(key string, value any) {
	mgr := getManager()
	if mgr != nil {
		mgr.Share(key, value)
	}
}

// ShareProp shares a prop only for the current request.
// Use this in middleware or handlers to share data like the authenticated user
// or flash messages without affecting other concurrent requests.
func ShareProp(c *http.Context, key string, value any) {
	var props map[string]any

	if existing, ok := c.Get(contextKey); ok {
		if p, ok := existing.(map[string]any); ok {
			props = p
		}
	}

	if props == nil {
		props = make(map[string]any)
	}

	props[key] = value
	c.Set(contextKey, props)
}
