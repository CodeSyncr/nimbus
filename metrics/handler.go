package metrics

import (
	"net/http"
)

// Handler returns an http.Handler that serves the /metrics endpoint
// in Prometheus text exposition format.
func Handler() http.Handler {
	return RegistryHandler(DefaultRegistry)
}

// RegistryHandler returns an http.Handler for a specific registry.
func RegistryHandler(r *Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(r.Expose()))
	})
}
