package metrics

import (
	"runtime"
)

// RuntimeStats captures a snapshot of Go runtime metrics that are commonly
// useful when debugging performance issues (GC, heap usage, goroutines).
// It is framework-agnostic and can be exposed via any HTTP handler, logged
// periodically, or consumed by observability plugins like Telescope.
type RuntimeStats struct {
	Goroutines   int    `json:"goroutines"`
	NumGC        uint32 `json:"num_gc"`
	LastGCUnixNs uint64 `json:"last_gc_unix_ns"`

	HeapAlloc    uint64 `json:"heap_alloc"`
	HeapSys      uint64 `json:"heap_sys"`
	HeapIdle     uint64 `json:"heap_idle"`
	HeapInuse    uint64 `json:"heap_inuse"`
	HeapReleased uint64 `json:"heap_released"`
	HeapObjects  uint64 `json:"heap_objects"`

	PauseTotalNs uint64 `json:"pause_total_ns"`
	NextGC       uint64 `json:"next_gc"`
}

// ReadRuntimeStats reads runtime.MemStats and returns a simplified snapshot.
// You can expose this via an endpoint, log it periodically, or feed it into
// dashboards like the Telescope plugin.
func ReadRuntimeStats() RuntimeStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return RuntimeStats{
		Goroutines:   runtime.NumGoroutine(),
		NumGC:        m.NumGC,
		LastGCUnixNs: m.LastGC,

		HeapAlloc:    m.HeapAlloc,
		HeapSys:      m.HeapSys,
		HeapIdle:     m.HeapIdle,
		HeapInuse:    m.HeapInuse,
		HeapReleased: m.HeapReleased,
		HeapObjects:  m.HeapObjects,

		PauseTotalNs: m.PauseTotalNs,
		NextGC:       m.NextGC,
	}
}

