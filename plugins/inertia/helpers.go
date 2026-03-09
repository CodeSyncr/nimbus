package inertia

import (
	"github.com/CodeSyncr/nimbus/context"
)

// Render renders an Inertia page. Use in handlers to return Vue/React/Svelte
// component data. When the request has the X-Inertia header, returns JSON.
// Otherwise returns full HTML with the root template.
//
//	func (c *HomeController) Index(ctx *context.Context) error {
//	    users := loadUsers()
//	    return inertia.Render(ctx, "Home/Index", map[string]any{
//	        "users": users,
//	    })
//	}
func Render(c *context.Context, component string, props map[string]any) error {
	mgr := getManager()
	if mgr == nil {
		return c.View("error", map[string]any{
			"message": "Inertia plugin not loaded",
		})
	}
	return mgr.Render(c.Response, c.Request, component, props)
}
