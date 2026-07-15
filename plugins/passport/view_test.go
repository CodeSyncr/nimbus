package passport

import (
	"strings"
	"testing"

	"github.com/CodeSyncr/nimbus/view"
)

// TestConsentViewRenders ensures the bundled consent template parses and
// executes with the data shape handleAuthorizeGet passes.
func TestConsentViewRenders(t *testing.T) {
	p := NewPlugin(nil, Config{})
	view.RegisterPluginViews("passport", p.ViewsFS())

	out, err := view.Render("passport/oauth-authorize", map[string]any{
		"client_name": "Acme App",
		"scopes":      []string{"read:profile", "write:profile"},
		"prefix":      "/oauth",
		"params": []map[string]string{
			{"name": "client_id", "value": "abc"},
			{"name": "redirect_uri", "value": "https://app.test/cb"},
		},
	})
	if err != nil {
		t.Fatalf("render consent: %v", err)
	}
	if !strings.Contains(out, "Acme App") || !strings.Contains(out, "read:profile") {
		t.Fatalf("consent screen missing client/scope content")
	}
	if !strings.Contains(out, `name="client_id" value="abc"`) {
		t.Fatalf("consent form missing carried-through params")
	}
}
