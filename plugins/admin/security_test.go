package admin

import (
	"github.com/CodeSyncr/nimbus"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
	"net/http/httptest"
	"testing"
)

func TestAdminRequiresExplicitAuthorization(t *testing.T) {
	p := New(nil, Config{})
	if err := p.Boot(nimbus.New()); err == nil {
		t.Fatal("boot accepted missing authorization")
	}
	r := router.New()
	p.RegisterRoutes(r)
	for _, method := range []string{"GET", "POST"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, "/admin/posts", nil))
		if w.Code != 403 {
			t.Fatalf("%s: %d", method, w.Code)
		}
	}
}
func TestAdminMutationsRequireCSRF(t *testing.T) {
	p := New(nil, Config{Authorize: func(*nhttp.Context) bool { return true }})
	r := router.New()
	p.RegisterRoutes(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/admin/posts", nil))
	if w.Code != 403 {
		t.Fatalf("unprotected mutation: %d", w.Code)
	}
}
