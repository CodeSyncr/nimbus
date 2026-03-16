package router

// Route represents a registered route and supports chaining (e.g. .As()).
type Route struct {
	router *Router
	method string
	path   string
	name   string
	Meta   RouteMeta
}

// RouteMeta holds route metadata for documentation and OpenAPI generation.
type RouteMeta struct {
	Summary     string            `json:"summary,omitempty"`
	Description string            `json:"description,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Deprecated  bool              `json:"deprecated,omitempty"`
	RequestBody any               `json:"request_body,omitempty"` // struct type for body
	Response    any               `json:"response,omitempty"`     // struct type for response
	Responses   map[int]any       `json:"responses,omitempty"`    // status -> struct
	Params      []ParamMeta       `json:"params,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Security    []string          `json:"security,omitempty"`
}

// ParamMeta describes a route parameter.
type ParamMeta struct {
	Name        string `json:"name"`
	In          string `json:"in"`   // path, query, header
	Type        string `json:"type"` // string, integer, boolean
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

// As assigns a name to this route for URL generation.
//
//	app.Router.Get("/users", handler).As("users.index")
//	app.Router.Get("/users/:id", handler).As("users.show")
func (rt *Route) As(name string) *Route {
	rt.name = name
	if rt.router != nil {
		rt.router.namedRoutes[name] = rt
	}
	return rt
}

// Describe sets a summary for API documentation.
func (rt *Route) Describe(summary string) *Route {
	rt.Meta.Summary = summary
	return rt
}

// Tag adds tags for API documentation grouping.
func (rt *Route) Tag(tags ...string) *Route {
	rt.Meta.Tags = append(rt.Meta.Tags, tags...)
	return rt
}

// Body sets the expected request body type for documentation.
func (rt *Route) Body(v any) *Route {
	rt.Meta.RequestBody = v
	return rt
}

// Returns sets the expected response type for documentation.
func (rt *Route) Returns(status int, v any) *Route {
	if rt.Meta.Responses == nil {
		rt.Meta.Responses = make(map[int]any)
	}
	rt.Meta.Responses[status] = v
	if status >= 200 && status < 300 {
		rt.Meta.Response = v
	}
	return rt
}

// Secure marks the route as requiring authentication.
func (rt *Route) Secure(schemes ...string) *Route {
	if len(schemes) == 0 {
		schemes = []string{"bearerAuth"}
	}
	rt.Meta.Security = schemes
	return rt
}

// DeprecatedRoute marks the route as deprecated.
func (rt *Route) DeprecatedRoute() *Route {
	rt.Meta.Deprecated = true
	return rt
}

// Method returns the HTTP method.
func (rt *Route) Method() string { return rt.method }

// Path returns the route path.
func (rt *Route) Path() string { return rt.path }

// Name returns the route name.
func (rt *Route) Name() string { return rt.name }
