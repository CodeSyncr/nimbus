package errors

import (
	"log"
	"strings"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
	"github.com/CodeSyncr/nimbus/validation"
)

// Handler is a global error handler. When a handler returns an error, this middleware catches it.
// Behavior:
//   - validation.ValidationErrors → 422 JSON
//   - *AppError → tracked error with ID, reported to registered reporters
//   - HTTPError → status from error
//   - fallback → 500 JSON with error ID for tracking
func Handler() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) (err error) {
			err = next(c)
			if err == nil {
				return nil
			}
			// Validation errors
			if ve, ok := err.(validation.ValidationErrors); ok {
				log.Printf("validation error: %v", ve)
				_ = c.JSON(http.StatusUnprocessableEntity, ve.ToMap())
				return nil
			}
			// AppError with ID tracking
			if ae, ok := err.(*AppError); ok {
				log.Printf("error_id=%s status=%d %s", ae.ID, ae.Status, ae.Message)
				reportCtx := map[string]any{
					"error_id": ae.ID,
					"status":   ae.Status,
				}
				if rid, ok := c.Get("request_id"); ok {
					reportCtx["request_id"] = rid
				}
				go ReportError(ae, reportCtx)
				status := ae.Status
				if status == 0 {
					status = http.StatusInternalServerError
				}
				_ = c.JSON(status, map[string]string{
					"error":    http.StatusText(status),
					"error_id": ae.ID,
				})
				return nil
			}
			// Explicit HTTP errors
			if he, ok := err.(HTTPError); ok {
				WriteHTTPError(c, he)
				return nil
			}
			if he, ok := err.(*HTTPError); ok {
				WriteHTTPError(c, *he)
				return nil
			}

			// Fallback 500 with error ID for tracking
			appErr := Wrap(http.StatusInternalServerError, err)
			log.Printf("error_id=%s handler error: %v", appErr.ID, err)
			reportCtx := map[string]any{
				"error_id": appErr.ID,
				"status":   500,
			}
			if rid, ok := c.Get("request_id"); ok {
				reportCtx["request_id"] = rid
			}
			go ReportError(appErr, reportCtx)
			_ = c.JSON(http.StatusInternalServerError, map[string]string{
				"error":    "Internal server error",
				"error_id": appErr.ID,
			})
			return nil
		}
	}
}

// HTTPError represents an HTTP error with status and optional payload.
type HTTPError struct {
	Status  int
	Message string
	Payload any
}

func (e HTTPError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return http.StatusText(e.Status)
}

// WriteHTTPError renders an HTTPError to the response. It is used by both the
// errors.Handler middleware and the router's fallback when no global handler
// is installed.
func WriteHTTPError(c *http.Context, he HTTPError) {
	if he.Status == 0 {
		he.Status = http.StatusInternalServerError
	}
	accept := c.Request.Header.Get("Accept")
	// For now, always return JSON; HTML error views can be implemented in app code.
	if he.Payload != nil {
		_ = c.JSON(he.Status, he.Payload)
		return
	}
	msg := he.Message
	if msg == "" {
		msg = http.StatusText(he.Status)
	}
	resp := map[string]string{"error": msg}
	// If client prefers HTML, still respond with JSON but status code set accordingly.
	if strings.Contains(accept, "application/json") || strings.Contains(accept, "text/json") || accept == "" {
		_ = c.JSON(he.Status, resp)
		return
	}
	// Simple text fallback for non-JSON clients.
	c.Response.WriteHeader(he.Status)
	_, _ = c.Response.Write([]byte(msg))
}
