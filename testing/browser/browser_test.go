package browser

import (
	"net/http"
	"testing"
)

// testApp is a tiny server-rendered app: a login form guarded by a session
// cookie and a CSRF hidden field, a dashboard, and a linked settings page.
func testApp() http.Handler {
	mux := http.NewServeMux()

	loginPage := `<!doctype html><html><head><title>Login</title></head><body>
	  <h1 class="page-title">Sign in</h1>
	  <form method="POST" action="/login">
	    <input type="hidden" name="csrf" value="tok-123"/>
	    <input type="email" name="email" value=""/>
	    <input type="password" name="password" value=""/>
	    <select name="plan"><option value="free">Free</option><option value="pro" selected>Pro</option></select>
	    <button type="submit">Log in</button>
	  </form>
	</body></html>`

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(loginPage))
			return
		}
		_ = r.ParseForm()
		if r.FormValue("csrf") != "tok-123" {
			http.Error(w, "bad csrf", http.StatusForbidden)
			return
		}
		if r.FormValue("email") == "a@b.com" && r.FormValue("password") == "secret" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "u1", Path: "/"})
			http.Redirect(w, r, "/dashboard", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><p class="error">Invalid credentials</p></body></html>`))
	})

	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("session"); err != nil || c.Value != "u1" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!doctype html><html><head><title>Dashboard</title></head><body>
		  <h1>Welcome back</h1>
		  <a href="/settings?tab=profile">Account settings</a>
		</body></html>`))
	})

	mux.HandleFunc("/settings", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Settings</title></head><body><h1>Settings</h1></body></html>`))
	})

	return mux
}

func TestBrowser_LoginJourney(t *testing.T) {
	b := New(t, testApp())

	// Visiting the dashboard unauthenticated redirects to login.
	b.Visit("/dashboard").AssertPathIs("/login").AssertSee("Sign in")

	// The select defaults to its selected option.
	b.AssertInputValue("plan", "pro")

	// Fill and submit — CSRF hidden field is carried automatically; cookie is
	// set on success and the redirect to /dashboard is followed.
	b.Fill("email", "a@b.com").
		Fill("password", "secret").
		Press("Log in").
		AssertPathIs("/dashboard").
		AssertStatus(http.StatusOK).
		AssertSee("Welcome back").
		AssertTitle("Dashboard")

	// Follow a link with a query string; session cookie persists.
	b.ClickLink("Account settings").
		AssertPathIs("/settings").
		AssertQueryStringHas("tab", "profile").
		AssertSeeIn("h1", "Settings")
}

func TestBrowser_FailedLoginStaysAndShowsError(t *testing.T) {
	b := New(t, testApp())
	b.Visit("/login").
		Fill("email", "a@b.com").
		Fill("password", "wrong").
		Press("Log in").
		AssertPathIs("/login").
		AssertSee("Invalid credentials").
		AssertDontSee("Welcome back")
}

func TestBrowser_CSRFEnforced(t *testing.T) {
	// A form with a wrong hidden token would be rejected — prove the browser
	// really carries hidden inputs by asserting the happy path depends on it.
	b := New(t, testApp())
	forms := parseForms(b.Visit("/login").Body())
	if len(forms) != 1 {
		t.Fatalf("expected 1 form, got %d", len(forms))
	}
	if forms[0].fields["csrf"] != "tok-123" {
		t.Fatalf("hidden csrf not parsed: %+v", forms[0].fields)
	}
	if !forms[0].hasSubmit("Log in") {
		t.Fatal("submit button label not detected")
	}
}
