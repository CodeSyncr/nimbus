package router

import (
	"fmt"
	"net/http"
	"strconv"

	nhttp "github.com/CodeSyncr/nimbus/http"
)

// Bindable is the interface that models must implement to support route model binding.
// When a route parameter matches the parameter name, the model is automatically
// resolved from the database and injected into the context store.
//
// Example model:
//
//	type User struct {
//	    database.Model
//	    Name  string
//	    Email string
//	}
//
//	func (u *User) RouteKey() string        { return "id" }
//	func (u *User) FindForRoute(val string) (any, error) {
//	    var user User
//	    err := db.Where("id = ?", val).First(&user).Error
//	    return &user, err
//	}
type Bindable interface {
	// RouteKey returns the route parameter name to match (e.g. "id", "slug").
	RouteKey() string
	// FindForRoute looks up the model by the given parameter value.
	// Return the found model or an error if not found.
	FindForRoute(value string) (any, error)
}

// ModelBinding is a route model binding registration.
type ModelBinding struct {
	// Param is the route parameter name (e.g. "id", "user").
	Param string
	// ContextKey is the key used to store the resolved model in c.Set().
	ContextKey string
	// Model is a Bindable instance used to resolve the model.
	Model Bindable
}

// BindModel returns middleware that resolves route parameters to models.
// It inspects the registered bindings, extracts the corresponding URL parameter,
// calls FindForRoute, and stores the result in the context via c.Set().
//
// Usage:
//
//	r.Get("/users/{id}", showUser).
//	    Use(router.BindModel(router.ModelBinding{
//	        Param:      "id",
//	        ContextKey: "user",
//	        Model:      &User{},
//	    }))
//
// Then in the handler:
//
//	func showUser(c *http.Context) error {
//	    user := c.MustGet("user").(*User)
//	    return c.JSON(200, user)
//	}
func BindModel(bindings ...ModelBinding) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *nhttp.Context) error {
			for _, b := range bindings {
				paramVal := c.Param(b.Param)
				if paramVal == "" {
					continue
				}

				model, err := b.Model.FindForRoute(paramVal)
				if err != nil {
					return &modelNotFoundError{
						Status:  http.StatusNotFound,
						Message: fmt.Sprintf("%s not found", b.ContextKey),
					}
				}
				c.Set(b.ContextKey, model)
			}
			return next(c)
		}
	}
}

// BindModelParam is a simpler version that uses the Bindable interface directly.
// The route parameter is taken from model.RouteKey(), and the context key
// defaults to the param name with "_model" suffix.
//
//	r.Get("/posts/{id}", showPost).Use(router.BindModelParam(&Post{}))
func BindModelParam(model Bindable) Middleware {
	return BindModel(ModelBinding{
		Param:      model.RouteKey(),
		ContextKey: model.RouteKey() + "_model",
		Model:      model,
	})
}

// ParamInt is a helper to extract an integer route parameter. Returns 0, false
// if the param is missing or not a valid integer.
func ParamInt(c *nhttp.Context, name string) (int, bool) {
	s := c.Param(name)
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// ParamInt64 extracts an int64 route parameter.
func ParamInt64(c *nhttp.Context, name string) (int64, bool) {
	s := c.Param(name)
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// modelNotFoundError is a local HTTP error type to avoid importing errors
// (which would create a cycle since errors imports router).
type modelNotFoundError struct {
	Status  int
	Message string
}

func (e *modelNotFoundError) Error() string { return e.Message }
