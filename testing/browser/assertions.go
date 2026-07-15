package browser

import (
	"net/http"
	"strings"
)

// ── Accessors ──────────────────────────────────────────────────────

// Status returns the final HTTP status of the current page.
func (b *Browser) Status() int { return b.status }

// Path returns the current URL path.
func (b *Browser) Path() string { return b.path() }

// Body returns the raw HTML of the current page.
func (b *Browser) Body() string { return b.body }

// Text returns the current page's visible text (tags stripped).
func (b *Browser) Text() string { return stripTags(b.body) }

// Value returns the value of a named form field on the current page.
func (b *Browser) Value(name string) string {
	v, _ := inputValue(b.body, name)
	return v
}

// ── Assertions (fail the test, return the Browser for chaining) ────

// AssertStatus fails unless the current status matches.
func (b *Browser) AssertStatus(code int) *Browser {
	b.t.Helper()
	if b.status != code {
		b.t.Errorf("browser: expected status %d, got %d at %s", code, b.status, b.path())
	}
	return b
}

// AssertOk asserts a 200 response.
func (b *Browser) AssertOk() *Browser { return b.AssertStatus(http.StatusOK) }

// AssertSee fails unless text appears in the page's visible text.
func (b *Browser) AssertSee(text string) *Browser {
	b.t.Helper()
	if !strings.Contains(b.Text(), text) {
		b.t.Errorf("browser: expected to see %q at %s", text, b.path())
	}
	return b
}

// AssertDontSee fails if text appears in the page's visible text.
func (b *Browser) AssertDontSee(text string) *Browser {
	b.t.Helper()
	if strings.Contains(b.Text(), text) {
		b.t.Errorf("browser: expected NOT to see %q at %s", text, b.path())
	}
	return b
}

// AssertSeeIn fails unless text appears within the visible text of the first
// element matching a very small CSS-ish selector: a tag name, .class, or #id.
func (b *Browser) AssertSeeIn(selector, text string) *Browser {
	b.t.Helper()
	frag, ok := elementText(b.body, selector)
	if !ok {
		b.t.Errorf("browser: selector %q not found at %s", selector, b.path())
		return b
	}
	if !strings.Contains(frag, text) {
		b.t.Errorf("browser: expected to see %q within %q, got %q", text, selector, frag)
	}
	return b
}

// AssertPathIs fails unless the current path equals want.
func (b *Browser) AssertPathIs(want string) *Browser {
	b.t.Helper()
	if b.path() != want {
		b.t.Errorf("browser: expected path %q, got %q", want, b.path())
	}
	return b
}

// AssertPathBeginsWith fails unless the current path has the given prefix.
func (b *Browser) AssertPathBeginsWith(prefix string) *Browser {
	b.t.Helper()
	if !strings.HasPrefix(b.path(), prefix) {
		b.t.Errorf("browser: expected path to begin with %q, got %q", prefix, b.path())
	}
	return b
}

// AssertQueryStringHas fails unless the current URL has query key (and value, if given).
func (b *Browser) AssertQueryStringHas(key string, value ...string) *Browser {
	b.t.Helper()
	if b.url == nil {
		b.t.Errorf("browser: no current URL")
		return b
	}
	q := b.url.Query()
	if !q.Has(key) {
		b.t.Errorf("browser: expected query param %q at %s", key, b.url.String())
		return b
	}
	if len(value) > 0 && q.Get(key) != value[0] {
		b.t.Errorf("browser: query %q = %q, want %q", key, q.Get(key), value[0])
	}
	return b
}

// AssertTitle fails unless the <title> equals want.
func (b *Browser) AssertTitle(want string) *Browser {
	b.t.Helper()
	if got := pageTitle(b.body); got != want {
		b.t.Errorf("browser: expected title %q, got %q", want, got)
	}
	return b
}

// AssertInputValue fails unless the named field has the given value.
func (b *Browser) AssertInputValue(name, want string) *Browser {
	b.t.Helper()
	got, ok := inputValue(b.body, name)
	if !ok {
		b.t.Errorf("browser: input %q not found at %s", name, b.path())
		return b
	}
	if got != want {
		b.t.Errorf("browser: input %q = %q, want %q", name, got, want)
	}
	return b
}

// AssertHeader fails unless a response header equals want.
func (b *Browser) AssertHeader(key, want string) *Browser {
	b.t.Helper()
	if got := b.header.Get(key); got != want {
		b.t.Errorf("browser: header %q = %q, want %q", key, got, want)
	}
	return b
}
