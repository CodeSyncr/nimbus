// Package browser is a Dusk-style end-to-end testing harness for Nimbus.
//
// It drives your application in-process through its http.Handler (the router),
// keeping a cookie jar so sessions, auth, and CSRF flow exactly as in a real
// browser. Because Nimbus renders HTML on the server, most end-to-end journeys
// — visiting pages, following links, filling and submitting forms, asserting
// on-page text — can be tested with no external browser at all:
//
//	b := browser.New(t, app.Router)
//	b.Visit("/login").
//	    Fill("email", "a@b.com").
//	    Fill("password", "secret").
//	    Press("Log in").
//	    AssertPathIs("/dashboard").
//	    AssertSee("Welcome back")
//
// For journeys that need real JavaScript (SPA widgets, client-side routing),
// drive a headless Chrome with chromedp against a running test server; the
// docs show the recommended recipe. The in-process browser here needs no
// external browser and covers server-rendered flows end to end.
package browser

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Browser is a stateful, in-process test browser. It is not safe for
// concurrent use; drive one Browser per test.
type Browser struct {
	t       testing.TB
	handler http.Handler
	jar     *cookiejar.Jar
	baseURL string

	// current page state
	status int
	url    *url.URL
	body   string
	header http.Header

	form         formValues // queued Fill/Type values for the next submit
	maxRedirects int
}

// New creates a Browser that drives the given handler (typically app.Router).
func New(t testing.TB, handler http.Handler) *Browser {
	jar, _ := cookiejar.New(nil)
	return &Browser{
		t:            t,
		handler:      handler,
		jar:          jar,
		baseURL:      "http://localhost",
		maxRedirects: 10,
	}
}

// WithBaseURL overrides the synthetic origin used for cookies/absolute URLs.
func (b *Browser) WithBaseURL(u string) *Browser {
	b.baseURL = strings.TrimRight(u, "/")
	return b
}

// ── Navigation ─────────────────────────────────────────────────────

// Visit performs a GET and follows redirects.
func (b *Browser) Visit(path string) *Browser {
	b.request(http.MethodGet, path, nil, "")
	return b
}

// ClickLink follows the first <a> whose visible text contains text.
func (b *Browser) ClickLink(text string) *Browser {
	b.t.Helper()
	href, ok := findLinkHref(b.body, text)
	if !ok {
		b.t.Fatalf("browser: no link containing text %q on %s", text, b.path())
		return b
	}
	b.request(http.MethodGet, href, nil, "")
	return b
}

// ── Forms ──────────────────────────────────────────────────────────

// Form is a set of field values queued for the next submit.
type formValues map[string]string

// pending returns the browser's working form value set, creating it on demand.
func (b *Browser) pending() formValues {
	if b.form == nil {
		b.form = formValues{}
	}
	return b.form
}

// Fill sets a form field value (input, textarea, select).
func (b *Browser) Fill(name, value string) *Browser {
	b.pending()[name] = value
	return b
}

// Type is an alias for Fill (Dusk parity).
func (b *Browser) Type(name, value string) *Browser { return b.Fill(name, value) }

// Check ticks a checkbox (sends value "1").
func (b *Browser) Check(name string) *Browser { return b.Fill(name, "1") }

// Uncheck unticks a checkbox (sends value "0").
func (b *Browser) Uncheck(name string) *Browser { return b.Fill(name, "0") }

// Select chooses an option value for a select field.
func (b *Browser) Select(name, value string) *Browser { return b.Fill(name, value) }

// Press submits the form containing the button/submit whose label or value
// matches text. Hidden inputs (e.g. CSRF tokens) on that form are included
// automatically; queued Fill/Type values override the form's defaults.
func (b *Browser) Press(text string) *Browser {
	b.t.Helper()
	return b.submitForm(func(f *htmlForm) bool { return f.hasSubmit(text) }, text)
}

// Submit posts the first form on the page (with its hidden inputs) merged with
// any queued Fill/Type values. Use when the form has no named button.
func (b *Browser) Submit() *Browser {
	b.t.Helper()
	return b.submitForm(func(*htmlForm) bool { return true }, "")
}

func (b *Browser) submitForm(match func(*htmlForm) bool, label string) *Browser {
	forms := parseForms(b.body)
	var target *htmlForm
	for i := range forms {
		if match(&forms[i]) {
			target = &forms[i]
			break
		}
	}
	if target == nil {
		if label != "" {
			b.t.Fatalf("browser: no form with submit %q on %s", label, b.path())
		} else {
			b.t.Fatalf("browser: no form found on %s", b.path())
		}
		return b
	}

	// Form defaults, then overlay queued values.
	vals := url.Values{}
	for k, v := range target.fields {
		vals.Set(k, v)
	}
	for k, v := range b.pending() {
		vals.Set(k, v)
	}
	b.form = nil // consume queued values

	action := target.action
	if action == "" {
		action = b.path()
	}
	method := strings.ToUpper(target.method)
	if method == "" {
		method = http.MethodGet
	}
	if method == http.MethodGet {
		b.request(http.MethodGet, appendQuery(action, vals), nil, "")
	} else {
		b.request(http.MethodPost, action, strings.NewReader(vals.Encode()), "application/x-www-form-urlencoded")
	}
	return b
}

// ── Core request/redirect engine ───────────────────────────────────

func (b *Browser) request(method, path string, body io.Reader, contentType string) {
	u := b.resolve(path)
	for i := 0; ; i++ {
		req := httptest.NewRequest(method, u.String(), body)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		for _, c := range b.jar.Cookies(u) {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		b.handler.ServeHTTP(rec, req)

		res := rec.Result()
		if cs := res.Cookies(); len(cs) > 0 {
			b.jar.SetCookies(u, cs)
		}

		b.status = rec.Code
		b.header = rec.Header()
		b.url = u

		// Follow redirects.
		if isRedirect(rec.Code) && i < b.maxRedirects {
			loc := rec.Header().Get("Location")
			if loc == "" {
				break
			}
			u = b.resolve(loc)
			// 303 and (per browsers) 302 switch to GET without a body.
			if rec.Code == http.StatusSeeOther || rec.Code == http.StatusFound || rec.Code == http.StatusMovedPermanently {
				method, body, contentType = http.MethodGet, nil, ""
			}
			continue
		}
		b.body = rec.Body.String()
		break
	}
}

func (b *Browser) resolve(path string) *url.URL {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		u, _ := url.Parse(path)
		return u
	}
	u, _ := url.Parse(b.baseURL)
	ref, _ := url.Parse(path)
	return u.ResolveReference(ref)
}

func (b *Browser) path() string {
	if b.url == nil {
		return ""
	}
	return b.url.Path
}

func isRedirect(code int) bool {
	switch code {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

func appendQuery(action string, vals url.Values) string {
	if len(vals) == 0 {
		return action
	}
	sep := "?"
	if strings.Contains(action, "?") {
		sep = "&"
	}
	return action + sep + vals.Encode()
}
