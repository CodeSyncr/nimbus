package errors

import (
	"github.com/CodeSyncr/nimbus/http"
)

// NotFoundHandler returns a router handler for unmatched routes (use with
// app.Router.Fallback). It is registered automatically by nimbus.New().
func NotFoundHandler() func(*http.Context) error {
	return func(c *http.Context) error {
		return HTTPError{
			Status:  http.StatusNotFound,
			Message: "The page you are looking for could not be found.",
		}
	}
}
