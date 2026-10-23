package router

import (
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	nhttp "github.com/CodeSyncr/nimbus/http"
)

// What the Nimbus router adds on top of chi for one JSON route, with no
// network in the way: compare the two and read the difference.
func BenchmarkChiJSON(b *testing.B) {
	r := chi.NewRouter()
	r.Get("/", func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"hello": "chi"})
	})
	req := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
}

func BenchmarkNimbusJSON(b *testing.B) {
	r := New()
	r.Get("/", func(c *nhttp.Context) error {
		return c.JSON(nhttp.StatusOK, map[string]string{"hello": "nimbus"})
	})
	req := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
}
