package router

// Route represents a registered route and supports chaining (e.g. .As()).
type Route struct {
	router *Router
	method string
	path   string
	name   string
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
