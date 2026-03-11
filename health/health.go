package health

import (
	"context"
	"encoding/json"
	"time"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Check runs all registered checks and returns status.
type Check func(ctx context.Context) error

// Result holds the health check result.
type Result struct {
	Status  string            `json:"status"` // "ok" or "degraded"
	Checks  map[string]string `json:"checks,omitempty"`
	Message string            `json:"message,omitempty"`
}

// Checker runs health checks.
type Checker struct {
	checks []struct {
		name string
		fn   Check
	}
}

// New creates a health checker.
func New() *Checker {
	return &Checker{}
}

// Add registers a check.
func (c *Checker) Add(name string, fn Check) {
	c.checks = append(c.checks, struct {
		name string
		fn   Check
	}{name, fn})
}

// DB adds a database ping check.
func (c *Checker) DB(db *gorm.DB) {
	c.Add("database", func(ctx context.Context) error {
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		return sqlDB.PingContext(ctx)
	})
}

// Redis adds a Redis ping check.
func (c *Checker) Redis(rdb *redis.Client) {
	c.Add("redis", func(ctx context.Context) error {
		return rdb.Ping(ctx).Err()
	})
}

// Run executes all checks. Returns Result with status "ok" if all pass, "degraded" otherwise.
func (c *Checker) Run(ctx context.Context) Result {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	result := Result{Status: "ok", Checks: make(map[string]string)}
	for _, ch := range c.checks {
		if err := ch.fn(ctx); err != nil {
			result.Checks[ch.name] = err.Error()
			result.Status = "degraded"
		} else {
			result.Checks[ch.name] = "ok"
		}
	}
	return result
}

// Handler returns an http.Handler that writes JSON health status. You can
// mount this as a liveness or readiness endpoint depending on which checks
// you register on the Checker.
func (c *Checker) Handler() http.StdHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result := c.Run(r.Context())
		code := http.StatusOK
		if result.Status != "ok" {
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(result)
	}
}
