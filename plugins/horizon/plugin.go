/*
|--------------------------------------------------------------------------
| Horizon Plugin for Nimbus (Laravel Horizon 1:1 style)
|--------------------------------------------------------------------------
|
| Horizon provides a queue dashboard and code-driven worker config:
| metrics, failed jobs (list/forget/retry), dashboard authorization,
| and optional Redis-backed failed job store.
|
| Usage:
|
|   app.Use(horizon.New())
|   app.Use(horizon.NewWithOptions(horizon.Options{ RedisURL: os.Getenv("REDIS_URL"), Gate: myGate }))
|
*/

package horizon

import (
	"os"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/queue"
	"github.com/CodeSyncr/nimbus/view"
	"github.com/redis/go-redis/v9"
)

var (
	_ nimbus.Plugin    = (*Plugin)(nil)
	_ nimbus.HasRoutes = (*Plugin)(nil)
	_ nimbus.HasViews  = (*Plugin)(nil)
)

// Options configures the Horizon plugin (Laravel-style).
type Options struct {
	// Config is the Horizon worker config (environments, supervisors). If nil, DefaultConfig() is used.
	Config *Config
	// Gate authorizes dashboard access. If nil, dashboard is allowed only when APP_ENV is not production or HORIZON_ENABLED=true.
	Gate func(*http.Context) bool
	// RedisURL enables failed job store and pause/continue state. If empty, failed jobs are not persisted.
	RedisURL string
}

// Plugin integrates a Horizon-style dashboard and optional failed job store.
type Plugin struct {
	nimbus.BasePlugin
	stats  *Stats
	opts   Options
	redis  *redis.Client
	failed queue.FailedJobStore
}

// New creates a new Horizon plugin with default options.
func New() *Plugin {
	return NewWithOptions(Options{})
}

// NewWithOptions creates a Horizon plugin with the given options.
func NewWithOptions(opts Options) *Plugin {
	p := &Plugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "horizon",
			PluginVersion: "1.0.0",
		},
		stats: NewStats(),
		opts:  opts,
	}
	if opts.Config == nil {
		cfg := DefaultConfig()
		p.opts.Config = &cfg
	}
	if p.opts.RedisURL != "" {
		if opt, err := redis.ParseURL(p.opts.RedisURL); err == nil {
			p.redis = redis.NewClient(opt)
			p.failed = queue.NewRedisFailedStore(p.redis)
		}
	}
	return p
}

// Register registers the plugin views, queue observer, and optional failed job store.
func (p *Plugin) Register(app *nimbus.App) error {
	view.RegisterPluginViews("horizon", p.ViewsFS())
	queue.SetObserver(p.stats)
	if p.failed != nil {
		queue.SetFailedJobStore(p.failed)
	}
	return nil
}

// Config returns the Horizon config (for CLI/workers).
func (p *Plugin) Config() *Config { return p.opts.Config }

// Gate returns the authorization gate (for routes).
func (p *Plugin) Gate() func(*http.Context) bool { return p.opts.Gate }

// Redis returns the Redis client if RedisURL was set (for pause/continue, etc.).
func (p *Plugin) Redis() *redis.Client { return p.redis }

// FailedStore returns the failed job store if Redis was configured.
func (p *Plugin) FailedStore() queue.FailedJobStore { return p.failed }

// Boot is a no-op for now (reserved for future enhancements).
func (p *Plugin) Boot(app *nimbus.App) error {
	return nil
}

// DefaultConfig returns default configuration for Horizon.
func (p *Plugin) DefaultConfig() map[string]any {
	return map[string]any{
		"enabled": os.Getenv("APP_ENV") == "development" || os.Getenv("APP_ENV") == "",
		"path":    "/horizon",
	}
}

// Stats holds in-memory queue statistics for the Horizon dashboard.
type Stats struct {
	mu sync.RWMutex

	StartedAt time.Time

	TotalDispatched int64
	TotalProcessed  int64
	TotalFailed     int64

	PerQueue map[string]*QueueStats
}

// QueueStats holds metrics for a specific queue.
type QueueStats struct {
	Name           string
	Dispatched     int64
	Processed      int64
	Failed         int64
	LastDispatched *time.Time
	LastProcessed  *time.Time
}

// NewStats creates a new Stats instance.
func NewStats() *Stats {
	return &Stats{
		StartedAt: time.Now(),
		PerQueue:  make(map[string]*QueueStats),
	}
}

// ensureQueue returns or creates stats for a queue.
func (s *Stats) ensureQueue(name string) *QueueStats {
	if name == "" {
		name = "default"
	}
	qs, ok := s.PerQueue[name]
	if !ok {
		qs = &QueueStats{Name: name}
		s.PerQueue[name] = qs
	}
	return qs
}

// JobDispatched implements queue.Observer.
func (s *Stats) JobDispatched(payload *queue.JobPayload) {
	if payload == nil {
		return
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TotalDispatched++
	qs := s.ensureQueue(payload.Queue)
	qs.Dispatched++
	qs.LastDispatched = &now
}

// JobProcessed implements queue.Observer.
func (s *Stats) JobProcessed(payload *queue.JobPayload, err error) {
	if payload == nil {
		return
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.TotalFailed++
		qs := s.ensureQueue(payload.Queue)
		qs.Failed++
		qs.LastProcessed = &now
		return
	}
	s.TotalProcessed++
	qs := s.ensureQueue(payload.Queue)
	qs.Processed++
	qs.LastProcessed = &now
}
