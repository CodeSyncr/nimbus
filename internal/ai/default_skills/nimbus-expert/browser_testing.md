# Browser / E2E Testing (Dusk-style) - Nimbus

`testing/browser` is a Dusk-style end-to-end harness. It drives the app **in-process** through its `http.Handler` (the router) with a cookie jar, so sessions, auth, and CSRF flow like a real browser. Because Nimbus renders HTML server-side, full journeys (visit, follow links, fill+submit forms, assert on-page text) run with **no external browser** at unit-test speed. No new dependencies — pure stdlib (`net/http/cookiejar`, `regexp`).

## Files

```
testing/browser/
  browser.go     // Browser, New(t, handler), Visit/ClickLink/Fill/Type/Check/Select/Press/Submit, redirect-following request engine
  html.go        // regex HTML scanning: parseForms, findLinkHref, inputValue, elementText, pageTitle, stripTags
  assertions.go  // AssertSee/DontSee/SeeIn, AssertPathIs/BeginsWith, AssertQueryStringHas, AssertTitle, AssertInputValue, AssertStatus/Ok, AssertHeader; accessors Status/Path/Body/Text/Value
```

## Usage

```go
import "github.com/CodeSyncr/nimbus/testing/browser"

func TestLoginJourney(t *testing.T) {
    app := bootstrapTestApp(t)
    b := browser.New(t, app.Router)

    b.Visit("/dashboard").AssertPathIs("/login").AssertSee("Sign in")

    b.Fill("email", "a@b.com").Fill("password", "secret").
        Press("Log in").                 // hidden CSRF input carried automatically
        AssertPathIs("/dashboard").      // 302 followed; session cookie kept
        AssertSee("Welcome back").AssertTitle("Dashboard")

    b.ClickLink("Account settings").
        AssertQueryStringHas("tab", "profile").
        AssertSeeIn("h1", "Settings")
}
```

## Key behaviors

- **Cookie jar** persists across requests (`cookiejar.New`), synthetic origin `http://localhost` (override with `WithBaseURL`).
- **Redirects** followed automatically (up to 10); 301/302/303 switch to GET with no body.
- **Forms:** `parseForms` extracts action/method, all `<input>` (incl. checked checkboxes/radios and hidden CSRF), `<textarea>`, `<select>` (selected or first option), and submit labels from `<button>`/`<input type=submit>`. `Press(text)` picks the form whose submit matches; queued `Fill/Type` values overlay the form's defaults; hidden inputs are always carried. `Submit()` uses the first form.
- **Assertions** fail the test and return the Browser for chaining. `AssertSee` matches visible text (tags stripped). `AssertSeeIn` selector supports a tag name, `.class`, or `#id`.

## Real JavaScript

The in-process browser runs **no JS**. For SPA/client-rendered flows, serve the app with `httptest.NewServer` and drive headless Chrome via `chromedp` against its URL. Server-rendered pages and Livewire round-trips (which POST back to the server) are covered by the in-process browser — faster, no browser binary. A bundled chromedp driver is intentionally **not** shipped (would require an unverifiable external dep + Chrome).

**Tests:** `testing/browser/browser_test.go` — login journey (session cookie + CSRF hidden field + redirect follow + link click with query string), failed-login-stays-with-error, and form/CSRF parsing. Complements the existing `testing.TestClient` (single request/response assertions).
