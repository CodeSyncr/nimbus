package router

import (
	"github.com/CodeSyncr/nimbus/http"
)

// ResourceController defines the 7 RESTful actions for a resource.
// Implement all methods on a controller struct, then register with:
//
//	app.Router.Resource("posts", &PostsController{})
type ResourceController interface {
	Index(c *http.Context) error   // GET    /posts
	Create(c *http.Context) error  // GET    /posts/create
	Store(c *http.Context) error   // POST   /posts
	Show(c *http.Context) error    // GET    /posts/:id
	Edit(c *http.Context) error    // GET    /posts/:id/edit
	Update(c *http.Context) error  // PUT    /posts/:id
	Destroy(c *http.Context) error // DELETE /posts/:id
}

// ResourceOption configures which actions to register.
type ResourceOption func(*resourceConfig)

type resourceConfig struct {
	only    map[string]bool
	except  map[string]bool
	apiOnly bool
}

// ApiOnly excludes the create and edit form routes (useful for JSON APIs).
func ApiOnly() ResourceOption {
	return func(c *resourceConfig) { c.apiOnly = true }
}

// Only registers only the listed actions.
// Valid actions: "index", "create", "store", "show", "edit", "update", "destroy".
func Only(actions ...string) ResourceOption {
	return func(c *resourceConfig) {
		c.only = make(map[string]bool, len(actions))
		for _, a := range actions {
			c.only[a] = true
		}
	}
}

// Except registers all actions except the listed ones.
func Except(actions ...string) ResourceOption {
	return func(c *resourceConfig) {
		c.except = make(map[string]bool, len(actions))
		for _, a := range actions {
			c.except[a] = true
		}
	}
}

func (cfg *resourceConfig) shouldRegister(action string) bool {
	if cfg.apiOnly && (action == "create" || action == "edit") {
		return false
	}
	if len(cfg.only) > 0 {
		return cfg.only[action]
	}
	if len(cfg.except) > 0 {
		return !cfg.except[action]
	}
	return true
}

type resourceAction struct {
	action string
	method string
	suffix string
}

var allResourceActions = []resourceAction{
	{"index", http.MethodGet, ""},
	{"create", http.MethodGet, "/create"},
	{"store", http.MethodPost, ""},
	{"show", http.MethodGet, "/:id"},
	{"edit", http.MethodGet, "/:id/edit"},
	{"update", http.MethodPut, "/:id"},
	{"destroy", http.MethodDelete, "/:id"},
}

func registerResource(r *Router, prefix, name string, ctrl ResourceController, groupMW []Middleware, opts []ResourceOption) {
	cfg := &resourceConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	base := prefix + "/" + name

	handlers := map[string]HandlerFunc{
		"index":   ctrl.Index,
		"create":  ctrl.Create,
		"store":   ctrl.Store,
		"show":    ctrl.Show,
		"edit":    ctrl.Edit,
		"update":  ctrl.Update,
		"destroy": ctrl.Destroy,
	}

	for _, ra := range allResourceActions {
		if !cfg.shouldRegister(ra.action) {
			continue
		}
		path := base + ra.suffix
		rt := r.addRoute(ra.method, path, handlers[ra.action], groupMW)
		rt.As(name + "." + ra.action)

		// PATCH also maps to update
		if ra.action == "update" && cfg.shouldRegister("update") {
			patchRt := r.addRoute(http.MethodPatch, path, handlers["update"], groupMW)
			patchRt.As(name + ".update_patch")
		}
	}
}
