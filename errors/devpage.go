package errors

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// ---------------------------------------------------------------------------
// Smart Error Pages — Rich dev-mode error diagnostics
// ---------------------------------------------------------------------------

// DevPageConfig configures the smart error page behaviour.
type DevPageConfig struct {
	// Enabled turns on rich HTML error pages (typically only in development).
	Enabled bool

	// AppRoot is the project root directory for resolving source files.
	// Defaults to the current working directory.
	AppRoot string

	// ContextLines is the number of source lines shown above/below error.
	ContextLines int

	// ShowRequest shows request headers and body in the error page.
	ShowRequest bool

	// ShowEnv shows environment variables (sanitised) in the error page.
	ShowEnv bool

	// BrandName to display on the page (default: "Nimbus").
	BrandName string

	// BrandColor for the header (default: "#6366f1").
	BrandColor string
}

// StackFrame represents a single frame in a stack trace.
type StackFrame struct {
	File     string       `json:"file"`
	Line     int          `json:"line"`
	Function string       `json:"function"`
	Source   []SourceLine `json:"source,omitempty"`
	IsApp    bool         `json:"is_app"` // true if in the app's source tree
}

// SourceLine is a line of source code with its line number.
type SourceLine struct {
	Number    int    `json:"number"`
	Code      string `json:"code"`
	Highlight bool   `json:"highlight"` // true for the error line
}

// DevError is the structured error object passed to the error page.
type DevError struct {
	Status  int          `json:"status"`
	Message string       `json:"message"`
	Type    string       `json:"type"`
	Stack   []StackFrame `json:"stack"`
	Request *RequestInfo `json:"request,omitempty"`
	Hints   []string     `json:"hints,omitempty"`
}

// RequestInfo captures request details for error diagnosis.
type RequestInfo struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Query   map[string]string `json:"query,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// SmartErrorHandler returns middleware that renders rich HTML error pages
// in development mode, with source code context and diagnostic hints.
func SmartErrorHandler(cfg ...DevPageConfig) router.Middleware {
	config := DevPageConfig{
		Enabled:      true,
		ContextLines: 8,
		ShowRequest:  true,
		ShowEnv:      false,
		BrandName:    "Nimbus",
		BrandColor:   "#6366f1",
	}
	if len(cfg) > 0 {
		config = cfg[0]
	}
	if config.ContextLines == 0 {
		config.ContextLines = 8
	}
	if config.BrandName == "" {
		config.BrandName = "Nimbus"
	}
	if config.BrandColor == "" {
		config.BrandColor = "#6366f1"
	}
	if config.AppRoot == "" {
		config.AppRoot, _ = os.Getwd()
	}

	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *nhttp.Context) error {
			err := next(c)
			if err == nil {
				return nil
			}

			if !config.Enabled {
				// Fallback to default handling.
				return err
			}

			// Build the dev error.
			devErr := buildDevError(c, err, config)

			// Check Accept header — if JSON requested, return JSON.
			accept := c.Request.Header.Get("Accept")
			if strings.Contains(accept, "application/json") {
				return c.JSON(devErr.Status, devErr)
			}

			// Render rich HTML error page.
			htmlContent := renderDevErrorPage(devErr, config)
			c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
			c.Response.WriteHeader(devErr.Status)
			_, _ = c.Response.Write([]byte(htmlContent))
			return nil
		}
	}
}

// buildDevError constructs a DevError from the caught error.
func buildDevError(c *nhttp.Context, err error, cfg DevPageConfig) DevError {
	status := 500
	message := err.Error()
	errType := fmt.Sprintf("%T", err)

	// Extract status from HTTPError.
	if he, ok := err.(HTTPError); ok {
		status = he.Status
		if he.Message != "" {
			message = he.Message
		}
	}
	if he, ok := err.(*HTTPError); ok {
		status = he.Status
		if he.Message != "" {
			message = he.Message
		}
	}

	de := DevError{
		Status:  status,
		Message: message,
		Type:    errType,
		Stack:   captureStack(cfg),
		Hints:   generateHints(err, status),
	}

	if cfg.ShowRequest {
		de.Request = captureRequest(c)
	}

	return de
}

// captureStack collects the call stack with source context.
func captureStack(cfg DevPageConfig) []StackFrame {
	var frames []StackFrame
	pcs := make([]uintptr, 50)
	n := runtime.Callers(4, pcs) // skip captureStack, buildDevError, handler, runtime
	pcs = pcs[:n]

	runtimeFrames := runtime.CallersFrames(pcs)
	for {
		frame, more := runtimeFrames.Next()

		// Skip runtime internals.
		if strings.Contains(frame.Function, "runtime.") && !strings.Contains(frame.Function, "runtime/debug") {
			if !more {
				break
			}
			continue
		}

		sf := StackFrame{
			File:     frame.File,
			Line:     frame.Line,
			Function: frame.Function,
		}

		// Determine if this is app code.
		if cfg.AppRoot != "" && strings.HasPrefix(frame.File, cfg.AppRoot) {
			sf.IsApp = true
		}

		// Read source context.
		sf.Source = readSourceContext(frame.File, frame.Line, cfg.ContextLines)

		frames = append(frames, sf)

		if !more {
			break
		}
	}

	return frames
}

// readSourceContext reads source lines around the error line.
func readSourceContext(file string, line int, contextLines int) []SourceLine {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}

	lines := strings.Split(string(data), "\n")
	start := line - contextLines - 1
	end := line + contextLines

	if start < 0 {
		start = 0
	}
	if end > len(lines) {
		end = len(lines)
	}

	var result []SourceLine
	for i := start; i < end; i++ {
		result = append(result, SourceLine{
			Number:    i + 1,
			Code:      lines[i],
			Highlight: i+1 == line,
		})
	}
	return result
}

// captureRequest builds RequestInfo from the HTTP context.
func captureRequest(c *nhttp.Context) *RequestInfo {
	ri := &RequestInfo{
		Method:  c.Request.Method,
		URL:     c.Request.URL.String(),
		Headers: make(map[string]string),
		Query:   make(map[string]string),
	}

	// Capture headers (sanitise auth).
	for k, v := range c.Request.Header {
		val := strings.Join(v, ", ")
		lower := strings.ToLower(k)
		if lower == "authorization" || lower == "cookie" || lower == "x-api-key" {
			val = "[REDACTED]"
		}
		ri.Headers[k] = val
	}

	// Capture query params.
	for k, v := range c.Request.URL.Query() {
		ri.Query[k] = strings.Join(v, ", ")
	}

	return ri
}

// generateHints produces diagnostic hints based on the error.
func generateHints(err error, status int) []string {
	var hints []string
	msg := strings.ToLower(err.Error())

	switch status {
	case 404:
		hints = append(hints,
			"Check that the route is registered in start/routes.go",
			"Verify the HTTP method matches (GET vs POST)",
			"Check for typos in the URL path",
		)
	case 401:
		hints = append(hints,
			"Ensure the request includes valid authentication credentials",
			"Check if the auth middleware is configured correctly",
			"Verify the token/session hasn't expired",
		)
	case 403:
		hints = append(hints,
			"The user is authenticated but lacks permission",
			"Check your policy/authorization rules",
			"Verify the user's role matches the required permissions",
		)
	case 405:
		hints = append(hints,
			"The HTTP method is not allowed for this route",
			"Check start/routes.go for allowed methods on this path",
		)
	case 422:
		hints = append(hints,
			"The request body failed validation",
			"Check the request payload matches the expected schema",
			"Review your validation rules in the controller",
		)
	case 429:
		hints = append(hints,
			"Rate limit exceeded",
			"Check your rate limiter configuration in config/limiter.go",
			"Consider increasing the rate limit for development",
		)
	}

	// Error message based hints.
	if strings.Contains(msg, "connection refused") {
		hints = append(hints,
			"Database or external service connection refused",
			"Check that the database server is running",
			"Verify database credentials in .env or config/database.go",
		)
	}
	if strings.Contains(msg, "no such table") || strings.Contains(msg, "relation") {
		hints = append(hints,
			"Database table not found — run migrations: nimbus migrate",
			"Check that the model name matches the table name",
		)
	}
	if strings.Contains(msg, "nil pointer") || strings.Contains(msg, "nil dereference") {
		hints = append(hints,
			"A nil pointer was dereferenced",
			"Check that all required dependencies are initialized",
			"Verify container bindings are registered before use",
		)
	}
	if strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "timeout") {
		hints = append(hints,
			"Request or operation timed out",
			"Check external service availability",
			"Consider increasing timeout values",
		)
	}
	if strings.Contains(msg, "json") && (strings.Contains(msg, "unmarshal") || strings.Contains(msg, "decode")) {
		hints = append(hints,
			"JSON parsing error — check the request body format",
			"Ensure Content-Type is application/json",
			"Verify the JSON structure matches the expected model",
		)
	}
	if strings.Contains(msg, "template") {
		hints = append(hints,
			"Template rendering error",
			"Check for syntax errors in your .nimbus template files",
			"Verify all template variables are being passed",
		)
	}
	if strings.Contains(msg, "record not found") {
		hints = append(hints,
			"The requested record was not found in the database",
			"Check the ID/slug in the URL",
			"Verify the data exists by checking the database directly",
		)
	}
	if strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique") {
		hints = append(hints,
			"Attempted to insert a duplicate record",
			"Check unique constraints on the database table",
			"Verify the data doesn't already exist before inserting",
		)
	}
	if strings.Contains(msg, "permission denied") || strings.Contains(msg, "access denied") {
		hints = append(hints,
			"File system or OS permission denied",
			"Check file/directory permissions",
			"Ensure the application has write access to storage/",
		)
	}

	if len(hints) == 0 {
		hints = append(hints,
			"Check the stack trace below for the error origin",
			"Enable debug logging for more details",
		)
	}

	return hints
}

// renderDevErrorPage produces the full HTML error page.
func renderDevErrorPage(de DevError, cfg DevPageConfig) string {
	var b strings.Builder

	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>`)
	b.WriteString(fmt.Sprintf("%d %s", de.Status, html.EscapeString(de.Message)))
	b.WriteString(` — `)
	b.WriteString(html.EscapeString(cfg.BrandName))
	b.WriteString(`</title>
<style>
:root {
  --brand: `)
	b.WriteString(cfg.BrandColor)
	b.WriteString(`;
  --bg: #0f172a;
  --surface: #1e293b;
  --surface2: #334155;
  --text: #e2e8f0;
  --text-dim: #94a3b8;
  --red: #ef4444;
  --orange: #f97316;
  --green: #22c55e;
  --blue: #3b82f6;
  --yellow: #eab308;
  --mono: 'SF Mono', 'Cascadia Code', 'Fira Code', 'JetBrains Mono', monospace;
}
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: Inter, system-ui, -apple-system, sans-serif; background: var(--bg); color: var(--text); line-height: 1.6; }
.header { background: linear-gradient(135deg, var(--brand), #4338ca); padding: 24px 32px; display: flex; align-items: center; justify-content: space-between; }
.header-left { display: flex; align-items: center; gap: 16px; }
.status-badge { background: rgba(255,255,255,0.15); border-radius: 8px; padding: 6px 16px; font-size: 14px; font-weight: 700; letter-spacing: 0.5px; }
.error-type { font-size: 12px; color: rgba(255,255,255,0.6); font-family: var(--mono); margin-top: 4px; }
.brand { font-size: 14px; font-weight: 700; opacity: 0.8; letter-spacing: 1px; text-transform: uppercase; }
.error-message { background: var(--surface); border-left: 4px solid var(--red); padding: 20px 28px; margin: 24px 32px; border-radius: 0 8px 8px 0; font-size: 18px; font-family: var(--mono); color: var(--red); word-break: break-word; }
.container { max-width: 1400px; margin: 0 auto; padding: 0 32px 64px; }
.section { margin-top: 32px; }
.section-title { font-size: 13px; font-weight: 700; text-transform: uppercase; letter-spacing: 1px; color: var(--text-dim); margin-bottom: 12px; display: flex; align-items: center; gap: 8px; }
.section-title .icon { font-size: 16px; }

/* Hints */
.hints { display: grid; gap: 8px; }
.hint { background: var(--surface); border-radius: 8px; padding: 12px 16px; display: flex; align-items: flex-start; gap: 10px; font-size: 14px; border: 1px solid var(--surface2); }
.hint-icon { color: var(--yellow); font-size: 16px; flex-shrink: 0; margin-top: 2px; }
.hint-text { color: var(--text-dim); }

/* Stack */
.stack-frame { background: var(--surface); border-radius: 8px; margin-bottom: 8px; overflow: hidden; border: 1px solid var(--surface2); transition: border-color 0.2s; }
.stack-frame:hover { border-color: var(--brand); }
.stack-frame.app-frame { border-left: 3px solid var(--brand); }
.frame-header { padding: 12px 16px; cursor: pointer; display: flex; align-items: center; gap: 12px; }
.frame-header:hover { background: rgba(99,102,241,0.05); }
.frame-fn { font-family: var(--mono); font-size: 13px; color: var(--text); font-weight: 600; }
.frame-file { font-family: var(--mono); font-size: 12px; color: var(--text-dim); margin-left: auto; }
.frame-badge { font-size: 10px; font-weight: 700; padding: 2px 8px; border-radius: 4px; text-transform: uppercase; letter-spacing: 0.5px; }
.frame-badge.app { background: rgba(99,102,241,0.2); color: var(--brand); }
.frame-badge.vendor { background: rgba(148,163,184,0.15); color: var(--text-dim); }
.source-code { display: none; border-top: 1px solid var(--surface2); }
.stack-frame.expanded .source-code { display: block; }
.source-line { display: flex; font-family: var(--mono); font-size: 12px; line-height: 1.8; }
.source-line.highlight { background: rgba(239,68,68,0.12); }
.line-number { width: 60px; text-align: right; padding-right: 16px; color: var(--text-dim); user-select: none; flex-shrink: 0; }
.line-code { white-space: pre; overflow-x: auto; padding-right: 16px; color: var(--text); }
.source-line.highlight .line-number { color: var(--red); font-weight: 700; }
.source-line.highlight .line-code { color: #fca5a5; }

/* Request */
.request-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 16px; }
.request-card { background: var(--surface); border-radius: 8px; padding: 16px; border: 1px solid var(--surface2); }
.request-card h4 { font-size: 12px; text-transform: uppercase; letter-spacing: 0.5px; color: var(--text-dim); margin-bottom: 10px; }
.kv-row { display: flex; justify-content: space-between; padding: 4px 0; font-size: 13px; font-family: var(--mono); border-bottom: 1px solid rgba(51,65,85,0.5); }
.kv-row:last-child { border-bottom: none; }
.kv-key { color: var(--text-dim); }
.kv-val { color: var(--text); max-width: 60%; text-align: right; word-break: break-all; }
.method-badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 11px; font-weight: 700; }
.method-badge.GET { background: rgba(99,102,241,0.2); color: #818cf8; }
.method-badge.POST { background: rgba(34,197,94,0.2); color: #4ade80; }
.method-badge.PUT { background: rgba(234,179,8,0.2); color: #facc15; }
.method-badge.PATCH { background: rgba(168,85,247,0.2); color: #c084fc; }
.method-badge.DELETE { background: rgba(239,68,68,0.2); color: #f87171; }

/* Toggle */
.toggle-vendor { background: var(--surface); border: 1px solid var(--surface2); border-radius: 6px; padding: 8px 14px; color: var(--text-dim); font-size: 13px; cursor: pointer; margin-bottom: 12px; }
.toggle-vendor:hover { border-color: var(--brand); color: var(--text); }

/* Scrollbar */
::-webkit-scrollbar { width: 6px; height: 6px; }
::-webkit-scrollbar-track { background: var(--surface); }
::-webkit-scrollbar-thumb { background: var(--surface2); border-radius: 3px; }
</style>
</head>
<body>
`)

	// Header.
	b.WriteString(fmt.Sprintf(`<div class="header">
  <div class="header-left">
    <span class="status-badge">%d</span>
    <div>
      <div style="font-size:20px;font-weight:700;">%s</div>
      <div class="error-type">%s</div>
    </div>
  </div>
  <span class="brand">%s</span>
</div>
`, de.Status, nhttp.StatusText(de.Status), html.EscapeString(de.Type), html.EscapeString(cfg.BrandName)))

	// Error message.
	b.WriteString(fmt.Sprintf(`<div class="error-message">%s</div>
`, html.EscapeString(de.Message)))

	b.WriteString(`<div class="container">`)

	// Hints section.
	if len(de.Hints) > 0 {
		b.WriteString(`<div class="section">
  <div class="section-title"><span class="icon">💡</span> Possible Fixes</div>
  <div class="hints">`)
		for _, hint := range de.Hints {
			b.WriteString(fmt.Sprintf(`
    <div class="hint">
      <span class="hint-icon">→</span>
      <span class="hint-text">%s</span>
    </div>`, html.EscapeString(hint)))
		}
		b.WriteString(`
  </div>
</div>`)
	}

	// Stack trace.
	if len(de.Stack) > 0 {
		b.WriteString(`
<div class="section">
  <div class="section-title"><span class="icon">📋</span> Stack Trace</div>
  <button class="toggle-vendor" onclick="toggleVendor()">Show/Hide Vendor Frames</button>`)

		for i, frame := range de.Stack {
			frameClass := "stack-frame"
			badgeClass := "vendor"
			badgeText := "VENDOR"
			if frame.IsApp {
				frameClass += " app-frame"
				badgeClass = "app"
				badgeText = "APP"
			} else {
				frameClass += " vendor-frame"
			}
			// First app frame is expanded by default.
			if i == 0 || (frame.IsApp && !hasAppFrameBefore(de.Stack, i)) {
				frameClass += " expanded"
			}

			// Shorten file path for display.
			displayFile := frame.File
			if cfg.AppRoot != "" {
				displayFile = strings.TrimPrefix(displayFile, cfg.AppRoot+"/")
			}

			b.WriteString(fmt.Sprintf(`
  <div class="%s">
    <div class="frame-header" onclick="this.parentElement.classList.toggle('expanded')">
      <span class="frame-badge %s">%s</span>
      <span class="frame-fn">%s</span>
      <span class="frame-file">%s:%d</span>
    </div>
    <div class="source-code">`,
				frameClass, badgeClass, badgeText,
				html.EscapeString(shortFunction(frame.Function)),
				html.EscapeString(displayFile), frame.Line))

			for _, sl := range frame.Source {
				lineClass := "source-line"
				if sl.Highlight {
					lineClass += " highlight"
				}
				b.WriteString(fmt.Sprintf(`
      <div class="%s">
        <span class="line-number">%d</span>
        <span class="line-code">%s</span>
      </div>`, lineClass, sl.Number, html.EscapeString(sl.Code)))
			}

			b.WriteString(`
    </div>
  </div>`)
		}

		b.WriteString(`
</div>`)
	}

	// Request info.
	if de.Request != nil {
		b.WriteString(`
<div class="section">
  <div class="section-title"><span class="icon">🌐</span> Request</div>
  <div class="request-grid">`)

		// Method + URL card.
		b.WriteString(fmt.Sprintf(`
    <div class="request-card">
      <h4>Request</h4>
      <div class="kv-row">
        <span class="kv-key">Method</span>
        <span class="kv-val"><span class="method-badge %s">%s</span></span>
      </div>
      <div class="kv-row">
        <span class="kv-key">URL</span>
        <span class="kv-val">%s</span>
      </div>
    </div>`,
			de.Request.Method, de.Request.Method,
			html.EscapeString(de.Request.URL)))

		// Headers card.
		if len(de.Request.Headers) > 0 {
			b.WriteString(`
    <div class="request-card">
      <h4>Headers</h4>`)
			for k, v := range de.Request.Headers {
				b.WriteString(fmt.Sprintf(`
      <div class="kv-row">
        <span class="kv-key">%s</span>
        <span class="kv-val">%s</span>
      </div>`, html.EscapeString(k), html.EscapeString(v)))
			}
			b.WriteString(`
    </div>`)
		}

		// Query params card.
		if len(de.Request.Query) > 0 {
			b.WriteString(`
    <div class="request-card">
      <h4>Query Parameters</h4>`)
			for k, v := range de.Request.Query {
				b.WriteString(fmt.Sprintf(`
      <div class="kv-row">
        <span class="kv-key">%s</span>
        <span class="kv-val">%s</span>
      </div>`, html.EscapeString(k), html.EscapeString(v)))
			}
			b.WriteString(`
    </div>`)
		}

		b.WriteString(`
  </div>
</div>`)
	}

	b.WriteString(`
</div>

<script>
// Expand/collapse stack frames.
function toggleVendor() {
  document.querySelectorAll('.vendor-frame').forEach(el => {
    el.style.display = el.style.display === 'none' ? '' : 'none';
  });
}
// Keyboard shortcut: press 'v' to toggle vendor frames.
document.addEventListener('keydown', (e) => {
  if (e.key === 'v' && !e.ctrlKey && !e.metaKey && e.target.tagName !== 'INPUT') {
    toggleVendor();
  }
});
</script>
</body>
</html>`)

	return b.String()
}

// shortFunction trims the function name to the last package + function.
func shortFunction(fn string) string {
	// github.com/CodeSyncr/nimbus/http.(*Context).JSON → http.(*Context).JSON
	if i := strings.LastIndex(fn, "/"); i >= 0 {
		fn = fn[i+1:]
	}
	return fn
}

// hasAppFrameBefore checks if any earlier frame is an app frame.
func hasAppFrameBefore(frames []StackFrame, idx int) bool {
	for i := 0; i < idx; i++ {
		if frames[i].IsApp {
			return true
		}
	}
	return false
}

// RelPath returns a path relative to the working directory.
func RelPath(path string) string {
	wd, _ := os.Getwd()
	rel, err := filepath.Rel(wd, path)
	if err != nil {
		return path
	}
	return rel
}
