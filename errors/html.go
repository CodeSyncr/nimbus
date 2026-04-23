package errors

import (
	"bytes"
	_ "embed"
	"html/template"
	"strings"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/validation"
)

//go:embed templates/error.html
var defaultErrorHTML string

var errorPageTmpl = template.Must(template.New("error").Parse(defaultErrorHTML))

// HTMLPageData is passed to the default error page template.
type HTMLPageData struct {
	Status     int
	StatusText string
	Title      string
	Message    string
	ErrorID    string
	// Validation, when set, renders field errors (422).
	Validation map[string][]string
	// JSONPayload when set shows raw JSON for API-style errors in dev tooltips (optional).
	JSONPayload string
}

// WantsHTML reports whether the client prefers an HTML error response.
// Browsers typically send Accept: text/html; XHR/API clients often send
// Accept: application/json or X-Requested-With: XMLHttpRequest.
func WantsHTML(c *http.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	if c.Request.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		return false
	}
	accept := c.Request.Header.Get("Accept")
	if accept == "" {
		return true
	}
	accept = strings.ToLower(accept)
	if strings.Contains(accept, "text/html") || strings.Contains(accept, "*/*") {
		return true
	}
	if strings.Contains(accept, "application/json") || strings.Contains(accept, "text/json") {
		return false
	}
	// Non-JSON explicit types → HTML for browser navigations
	return !strings.Contains(accept, "application/json")
}

// RenderDefaultHTML writes the built-in HTML error page.
func RenderDefaultHTML(c *http.Context, data HTMLPageData) error {
	if data.Status == 0 {
		data.Status = http.StatusInternalServerError
	}
	if data.StatusText == "" {
		data.StatusText = http.StatusText(data.Status)
	}
	if data.Title == "" {
		data.Title = data.StatusText
	}
	c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Response.WriteHeader(data.Status)
	var buf bytes.Buffer
	if err := errorPageTmpl.Execute(&buf, data); err != nil {
		_, _ = c.Response.Write([]byte("<!DOCTYPE html><title>Error</title><p>Something went wrong.</p>"))
		return err
	}
	_, err := c.Response.Write(buf.Bytes())
	return err
}

// writeValidationHTML responds with HTML for validation errors.
func writeValidationHTML(c *http.Context, ve validation.ValidationErrors) error {
	m := ve.ToMap()
	msg := "The given data was invalid."
	return RenderDefaultHTML(c, HTMLPageData{
		Status:     http.StatusUnprocessableEntity,
		StatusText: http.StatusText(http.StatusUnprocessableEntity),
		Title:      "Validation error",
		Message:    msg,
		Validation: m,
	})
}

// writeAppErrorHTML renders AppError as HTML.
func writeAppErrorHTML(c *http.Context, ae *AppError) error {
	st := ae.Status
	if st == 0 {
		st = http.StatusInternalServerError
	}
	return RenderDefaultHTML(c, HTMLPageData{
		Status:     st,
		StatusText: http.StatusText(st),
		Title:      http.StatusText(st),
		Message:    ae.Message,
		ErrorID:    ae.ID,
	})
}

// writeGeneric500HTML renders fallback 500 with error_id.
func writeGeneric500HTML(c *http.Context, errorID string) error {
	return RenderDefaultHTML(c, HTMLPageData{
		Status:     http.StatusInternalServerError,
		StatusText: "Internal Server Error",
		Title:      "Internal Server Error",
		Message:    "An unexpected error occurred. Reference ID:",
		ErrorID:    errorID,
	})
}

