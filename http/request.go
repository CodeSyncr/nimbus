package http

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ── Request Input Helpers ───────────────────────────────────────

// Input returns a form or query parameter by key. Checks POST body first, then query string.
func (c *Context) Input(key string, def ...string) string {
	if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut || c.Request.Method == http.MethodPatch {
		_ = c.Request.ParseForm()
		if v := c.Request.PostFormValue(key); v != "" {
			return v
		}
	}
	if v := c.Request.URL.Query().Get(key); v != "" {
		return v
	}
	if len(def) > 0 {
		return def[0]
	}
	return ""
}

// InputInt returns a form or query parameter as int.
func (c *Context) InputInt(key string, def int) int {
	v := c.Input(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// InputBool returns a form or query parameter as bool.
// Truthy values: "1", "true", "yes", "on".
func (c *Context) InputBool(key string) bool {
	v := strings.ToLower(c.Input(key))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// InputFloat returns a form or query parameter as float64.
func (c *Context) InputFloat(key string, def float64) float64 {
	v := c.Input(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

// Query returns a query string parameter. Alias for URL query.
func (c *Context) Query(key string, def ...string) string {
	v := c.Request.URL.Query().Get(key)
	if v == "" && len(def) > 0 {
		return def[0]
	}
	return v
}

// QueryBool returns a query parameter as bool.
func (c *Context) QueryBool(key string) bool {
	v := strings.ToLower(c.Query(key))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// All returns all form values + query params merged into a map.
func (c *Context) All() map[string]string {
	_ = c.Request.ParseForm()
	result := make(map[string]string)
	for k, vv := range c.Request.URL.Query() {
		if len(vv) > 0 {
			result[k] = vv[0]
		}
	}
	for k, vv := range c.Request.PostForm {
		if len(vv) > 0 {
			result[k] = vv[0]
		}
	}
	return result
}

// Only returns a subset of input matching the given keys.
func (c *Context) Only(keys ...string) map[string]string {
	result := make(map[string]string, len(keys))
	for _, k := range keys {
		result[k] = c.Input(k)
	}
	return result
}

// Except returns all input except the given keys.
func (c *Context) Except(keys ...string) map[string]string {
	all := c.All()
	for _, k := range keys {
		delete(all, k)
	}
	return all
}

// Has returns true if the input key exists and is non-empty.
func (c *Context) Has(key string) bool {
	return c.Input(key) != ""
}

// Filled returns true if all given keys are present and non-empty.
func (c *Context) Filled(keys ...string) bool {
	for _, k := range keys {
		if c.Input(k) == "" {
			return false
		}
	}
	return true
}

// ── Request Body Binding ────────────────────────────────────────

// Bind decodes the request body (JSON or form) into v.
// Content-Type is auto-detected.
func (c *Context) Bind(v any) error {
	ct := c.Request.Header.Get("Content-Type")
	switch {
	case strings.Contains(ct, "application/json"):
		return json.NewDecoder(c.Request.Body).Decode(v)
	case strings.Contains(ct, "application/x-www-form-urlencoded"):
		if err := c.Request.ParseForm(); err != nil {
			return err
		}
		return bindForm(c.Request.PostForm, v)
	case strings.Contains(ct, "multipart/form-data"):
		if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
			return err
		}
		return bindForm(c.Request.PostForm, v)
	default:
		return json.NewDecoder(c.Request.Body).Decode(v)
	}
}

// BindJSON explicitly binds JSON body into v.
func (c *Context) BindJSON(v any) error {
	return json.NewDecoder(c.Request.Body).Decode(v)
}

// bindForm binds url.Values to a struct using json tags.
func bindForm(values map[string][]string, v any) error {
	// Simple JSON round-trip: convert to flat map, then marshal/unmarshal.
	flat := make(map[string]string, len(values))
	for k, vv := range values {
		if len(vv) > 0 {
			flat[k] = vv[0]
		}
	}
	b, err := json.Marshal(flat)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// ── File Upload ─────────────────────────────────────────────────

// File returns the uploaded file for the given form field name.
func (c *Context) File(name string) (multipart.File, *multipart.FileHeader, error) {
	if c.Request.MultipartForm == nil {
		if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
			return nil, nil, err
		}
	}
	return c.Request.FormFile(name)
}

// Files returns all uploaded files for a form field (e.g. "photos[]").
func (c *Context) Files(name string) []*multipart.FileHeader {
	if c.Request.MultipartForm == nil {
		_ = c.Request.ParseMultipartForm(32 << 20)
	}
	if c.Request.MultipartForm == nil {
		return nil
	}
	return c.Request.MultipartForm.File[name]
}

// HasFile returns true if a file was uploaded for the field.
func (c *Context) HasFile(name string) bool {
	_, fh, err := c.File(name)
	return err == nil && fh != nil && fh.Size > 0
}

// ── Cookie Helpers ──────────────────────────────────────────────

// Cookie returns a cookie value by name, or the default.
func (c *Context) Cookie(name string, def ...string) string {
	cookie, err := c.Request.Cookie(name)
	if err != nil || cookie.Value == "" {
		if len(def) > 0 {
			return def[0]
		}
		return ""
	}
	return cookie.Value
}

// SetCookie sets a response cookie with the given parameters.
func (c *Context) SetCookie(name, value string, maxAge int, path, domain string, secure, httpOnly bool) {
	http.SetCookie(c.Response, &http.Cookie{
		Name:     name,
		Value:    value,
		MaxAge:   maxAge,
		Path:     path,
		Domain:   domain,
		Secure:   secure,
		HttpOnly: httpOnly,
	})
}

// ClearCookie removes a cookie by setting MaxAge to -1.
func (c *Context) ClearCookie(name, path, domain string) {
	http.SetCookie(c.Response, &http.Cookie{
		Name:   name,
		Value:  "",
		MaxAge: -1,
		Path:   path,
		Domain: domain,
	})
}

// ── Request Metadata ────────────────────────────────────────────

// IP returns the client's IP address, checking X-Forwarded-For and X-Real-IP headers.
func (c *Context) IP() string {
	if xff := c.Request.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	if xri := c.Request.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	addr := c.Request.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx >= 0 {
		return addr[:idx]
	}
	return addr
}

// UserAgent returns the User-Agent header.
func (c *Context) UserAgent() string {
	return c.Request.Header.Get("User-Agent")
}

// IsAjax returns true if the request has X-Requested-With: XMLHttpRequest.
func (c *Context) IsAjax() bool {
	return c.Request.Header.Get("X-Requested-With") == "XMLHttpRequest"
}

// IsJSON returns true if the Accept header prefers JSON.
func (c *Context) IsJSON() bool {
	accept := c.Request.Header.Get("Accept")
	return strings.Contains(accept, "application/json")
}

// Method returns the HTTP method.
func (c *Context) Method() string {
	return c.Request.Method
}

// Path returns the request URL path.
func (c *Context) Path() string {
	return c.Request.URL.Path
}

// FullURL returns the full request URL including scheme and host.
func (c *Context) FullURL() string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if fp := c.Request.Header.Get("X-Forwarded-Proto"); fp != "" {
		scheme = fp
	}
	return fmt.Sprintf("%s://%s%s", scheme, c.Request.Host, c.Request.RequestURI)
}

// Header returns a request header value.
func (c *Context) Header(key string) string {
	return c.Request.Header.Get(key)
}

// SetHeader sets a response header.
func (c *Context) SetHeader(key, value string) {
	c.Response.Header().Set(key, value)
}

// ── Response Helpers ────────────────────────────────────────────

// NoContent sends a 204 No Content response.
func (c *Context) NoContent() {
	c.Response.WriteHeader(http.StatusNoContent)
}

// Created sends a 201 Created JSON response.
func (c *Context) Created(body any) error {
	return c.JSON(http.StatusCreated, body)
}

// Accepted sends a 202 Accepted JSON response.
func (c *Context) Accepted(body any) error {
	return c.JSON(http.StatusAccepted, body)
}

// BadRequest sends a 400 Bad Request JSON response.
func (c *Context) BadRequest(body any) error {
	return c.JSON(http.StatusBadRequest, body)
}

// NotFound sends a 404 Not Found JSON response.
func (c *Context) NotFound(body ...any) error {
	if len(body) > 0 {
		return c.JSON(http.StatusNotFound, body[0])
	}
	return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
}

// Forbidden sends a 403 Forbidden JSON response.
func (c *Context) Forbidden(body ...any) error {
	if len(body) > 0 {
		return c.JSON(http.StatusForbidden, body[0])
	}
	return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
}

// Unauthorized sends a 401 Unauthorized JSON response.
func (c *Context) Unauthorized(body ...any) error {
	if len(body) > 0 {
		return c.JSON(http.StatusUnauthorized, body[0])
	}
	return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
}

// ServerError sends a 500 Internal Server Error JSON response.
func (c *Context) ServerError(body ...any) error {
	if len(body) > 0 {
		return c.JSON(http.StatusInternalServerError, body[0])
	}
	return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
}

// HTML sends an HTML response with the given status code.
func (c *Context) HTML(code int, html string) {
	c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Response.WriteHeader(code)
	c.Response.Write([]byte(html))
}

// Data sends raw bytes with the given content type.
func (c *Context) Data(code int, contentType string, data []byte) {
	c.Response.Header().Set("Content-Type", contentType)
	c.Response.WriteHeader(code)
	c.Response.Write(data)
}

// Download sends a file as an attachment with the given filename.
func (c *Context) Download(filePath, fileName string) error {
	c.Response.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))
	http.ServeFile(c.Response, c.Request, filePath)
	return nil
}

// Inline sends a file to be displayed inline (e.g. PDF in browser).
func (c *Context) Inline(filePath, fileName string) error {
	c.Response.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", fileName))
	http.ServeFile(c.Response, c.Request, filePath)
	return nil
}

// Stream sends a streaming response. The callback writes directly to the response.
func (c *Context) Stream(code int, contentType string, fn func(w io.Writer) error) error {
	c.Response.Header().Set("Content-Type", contentType)
	c.Response.WriteHeader(code)
	return fn(c.Response)
}

// StreamJSON sends a streaming JSON response (useful for large datasets).
func (c *Context) StreamJSON(code int, fn func(enc *json.Encoder) error) error {
	c.Response.Header().Set("Content-Type", "application/json")
	c.Response.WriteHeader(code)
	enc := json.NewEncoder(c.Response)
	return fn(enc)
}

// SSE sends a Server-Sent Event. Call multiple times for streaming.
func (c *Context) SSE(event, data string) error {
	if event != "" {
		fmt.Fprintf(c.Response, "event: %s\n", event)
	}
	fmt.Fprintf(c.Response, "data: %s\n\n", data)
	if f, ok := c.Response.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

// SendFile serves a file from the filesystem.
func (c *Context) SendFile(dir, file string) {
	http.ServeFile(c.Response, c.Request, filepath.Join(dir, filepath.Clean(file)))
}

// Attachment sets the Content-Disposition header to "attachment".
func (c *Context) Attachment(filename string) {
	c.Response.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
}

// ContentType sets the Content-Type response header.
func (c *Context) ContentType(ct string) *Context {
	c.Response.Header().Set("Content-Type", ct)
	return c
}

// ── Response Caching ────────────────────────────────────────────

// CacheControl sets the Cache-Control response header.
func (c *Context) CacheControl(directives string) *Context {
	c.Response.Header().Set("Cache-Control", directives)
	return c
}

// NoCache sets headers to prevent browser caching.
func (c *Context) NoCache() *Context {
	c.Response.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Response.Header().Set("Pragma", "no-cache")
	c.Response.Header().Set("Expires", "0")
	return c
}

// Expires sets the Expires header.
func (c *Context) Expires(t time.Time) *Context {
	c.Response.Header().Set("Expires", t.UTC().Format(http.TimeFormat))
	return c
}

// LastModified sets the Last-Modified header.
func (c *Context) LastModified(t time.Time) *Context {
	c.Response.Header().Set("Last-Modified", t.UTC().Format(http.TimeFormat))
	return c
}

// ── Response Writer Wrapper ─────────────────────────────────────

// Write implements io.Writer on the response.
func (c *Context) Write(data []byte) (int, error) {
	return c.Response.Write(data)
}

// WriteString writes a string to the response.
func (c *Context) WriteString(s string) (int, error) {
	return io.WriteString(c.Response, s)
}

// Flush flushes the response writer if it supports flushing.
func (c *Context) Flush() {
	if f, ok := c.Response.(http.Flusher); ok {
		f.Flush()
	}
}

// ── Error Attachment ────────────────────────────────────────────

// Abort writes the given status code with no body and returns a generic error.
func (c *Context) Abort(code int) error {
	c.Response.WriteHeader(code)
	return fmt.Errorf("http: aborted with status %d", code)
}

// AbortWithJSON sends a JSON error and returns an error.
func (c *Context) AbortWithJSON(code int, body any) error {
	_ = c.JSON(code, body)
	return fmt.Errorf("http: aborted with status %d", code)
}

// ── Static helpers ──────────────────────────────────────────────

// ServeStatic serves static files from the given directory.
func ServeStatic(dir string) http.Handler {
	return http.FileServer(http.Dir(dir))
}

// ServeStaticFile serves a single file.
func ServeStaticFile(filePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filePath)
	}
}

// ── SPA (Single Page App) fallback ──────────────────────────────

// SPAHandler serves an SPA: serves static files from dir, falls back to index.html.
func SPAHandler(dir string) http.HandlerFunc {
	fs := http.Dir(dir)
	fileServer := http.FileServer(fs)
	return func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, r.URL.Path)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		fileServer.ServeHTTP(w, r)
	}
}
