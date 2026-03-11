package horizon

import (
	"context"
	"os"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/queue"
	"github.com/CodeSyncr/nimbus/router"
)

// RegisterRoutes mounts the Horizon dashboard routes (Laravel Horizon style).
func (p *Plugin) RegisterRoutes(r *router.Router) {
	if os.Getenv("APP_ENV") == "production" && os.Getenv("HORIZON_ENABLED") != "true" {
		return
	}
	path := "/horizon"
	if pth := os.Getenv("HORIZON_PATH"); pth != "" {
		path = pth
	}
	grp := r.Group(path)
	grp.Get("/", p.dashboardHandler)
	grp.Get("/monitoring", p.monitoringHandler)
	grp.Get("/metrics", p.metricsPageHandler)
	grp.Get("/api/metrics", p.metricsHandler)
	grp.Get("/batches", p.batchesHandler)
	grp.Get("/pending", p.pendingHandler)
	grp.Get("/completed", p.completedHandler)
	grp.Get("/silenced", p.silencedHandler)
	grp.Get("/failed", p.failedPageHandler)
	grp.Get("/api/failed", p.failedListHandler)
	grp.Post("/failed/:id/forget", p.failedForgetHandler)
	grp.Post("/failed/forget-all", p.failedForgetAllHandler)
	grp.Post("/failed/:id/retry", p.failedRetryHandler)
}

// authorize returns true if the request is allowed to access Horizon.
func (p *Plugin) authorize(c *http.Context) bool {
	if p.opts.Gate != nil {
		return p.opts.Gate(c)
	}
	// Default: allow in non-production or when HORIZON_ENABLED=true
	if os.Getenv("APP_ENV") != "production" || os.Getenv("HORIZON_ENABLED") == "true" {
		return true
	}
	return false
}

func (p *Plugin) horizonPath() string {
	if pth := os.Getenv("HORIZON_PATH"); pth != "" {
		return pth
	}
	return "/horizon"
}

// baseData returns common template data for all Horizon pages.
func (p *Plugin) baseData(page string, title string) map[string]any {
	return map[string]any{
		"page":        page,
		"title":       title,
		"horizonPath": p.horizonPath(),
	}
}

func (p *Plugin) dashboardHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	snap := p.snapshot()
	data := p.baseData("dashboard", "Dashboard")
	data["stats"] = snap
	data["queues"] = snap["queues"]
	data["hasQueues"] = snap["has_queues"]
	data["hasFailed"] = snap["has_failed"]
	return c.View("horizon/dashboard", data)
}

func (p *Plugin) monitoringHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	snap := p.snapshot()
	data := p.baseData("monitoring", "Monitoring")
	data["stats"] = snap
	data["queues"] = snap["queues"]
	data["hasQueues"] = snap["has_queues"]
	data["hasFailed"] = snap["has_failed"]
	return c.View("horizon/monitoring", data)
}

func (p *Plugin) metricsPageHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	snap := p.snapshot()
	data := p.baseData("metrics", "Metrics")
	data["stats"] = snap
	data["queues"] = snap["queues"]
	data["hasQueues"] = snap["has_queues"]
	return c.View("horizon/metrics", data)
}

func (p *Plugin) batchesHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	data := p.baseData("batches", "Batches")
	return c.View("horizon/batches", data)
}

func (p *Plugin) pendingHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	snap := p.snapshot()
	data := p.baseData("pending", "Pending jobs")
	data["queues"] = snap["queues"]
	data["hasQueues"] = snap["has_queues"]
	return c.View("horizon/pending", data)
}

func (p *Plugin) completedHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	data := p.baseData("completed", "Completed jobs")
	return c.View("horizon/completed", data)
}

func (p *Plugin) silencedHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	data := p.baseData("silenced", "Silenced jobs")
	return c.View("horizon/silenced", data)
}

func (p *Plugin) failedPageHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	data := p.baseData("failed", "Failed jobs")
	if p.failed != nil {
		list, _ := p.failed.List(c.Request.Context())
		data["failedJobs"] = list
		data["failedCount"] = len(list)
	} else {
		data["failedJobs"] = []queue.FailedJobRecord{}
		data["failedCount"] = 0
	}
	return c.View("horizon/failed", data)
}

func (p *Plugin) metricsHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	return c.JSON(http.StatusOK, p.snapshot())
}

func (p *Plugin) failedListHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	if p.failed == nil {
		return c.JSON(http.StatusNotImplemented, map[string]string{"error": "failed job store not configured (set RedisURL)"})
	}
	list, err := p.failed.List(c.Request.Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"failed": list})
}

func (p *Plugin) failedForgetHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	if p.failed == nil {
		return c.JSON(http.StatusNotImplemented, map[string]string{"error": "failed job store not configured"})
	}
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
	}
	if err := p.failed.Forget(c.Request.Context(), id); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Plugin) failedForgetAllHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	if p.failed == nil {
		return c.JSON(http.StatusNotImplemented, map[string]string{"error": "failed job store not configured"})
	}
	if err := p.failed.ForgetAll(c.Request.Context()); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Plugin) failedRetryHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	if p.failed == nil {
		return c.JSON(http.StatusNotImplemented, map[string]string{"error": "failed job store not configured"})
	}
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
	}
	mgr := queue.GetGlobal()
	if mgr == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "queue manager not available"})
	}
	enqueue := func(ctx context.Context, payload *queue.JobPayload) error {
		return mgr.Adapter().Push(ctx, payload)
	}
	if err := p.failed.Retry(c.Request.Context(), id, enqueue); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "retried"})
}

// snapshot returns a safe copy of current stats for templates/APIs.
func (p *Plugin) snapshot() map[string]any {
	p.stats.mu.RLock()
	defer p.stats.mu.RUnlock()

	out := map[string]any{
		"started_at":       p.stats.StartedAt,
		"total_dispatched": p.stats.TotalDispatched,
		"total_processed":  p.stats.TotalProcessed,
		"total_failed":     p.stats.TotalFailed,
	}
	queues := make([]map[string]any, 0, len(p.stats.PerQueue))
	for _, qs := range p.stats.PerQueue {
		item := map[string]any{
			"name":       qs.Name,
			"dispatched": qs.Dispatched,
			"processed":  qs.Processed,
			"failed":     qs.Failed,
		}
		if qs.LastDispatched != nil {
			item["last_dispatched"] = qs.LastDispatched
		}
		if qs.LastProcessed != nil {
			item["last_processed"] = qs.LastProcessed
		}
		queues = append(queues, item)
	}
	out["queues"] = queues
	out["has_failed"] = p.stats.TotalFailed > 0
	out["has_queues"] = len(queues) > 0
	return out
}
