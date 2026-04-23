package horizon

import (
	"context"
	"os"
	"strings"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/metrics"
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
	grp.Get("/api/workloads", p.workloadsHandler)
	grp.Get("/api/metrics/prometheus", p.prometheusMetricsHandler)
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
	snap := p.snapshot()
	data := p.baseData("batches", "Batches")
	data["stats"] = snap
	data["queues"] = snap["queues"]
	data["hasQueues"] = snap["has_queues"]
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
	data["redis_workloads"] = snap["redis_workloads"]
	data["redis_workloads_ok"] = snap["redis_workloads_ok"]
	return c.View("horizon/pending", data)
}

func (p *Plugin) completedHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	snap := p.snapshot()
	data := p.baseData("completed", "Completed jobs")
	data["stats"] = snap
	data["queues"] = snap["queues"]
	data["hasQueues"] = snap["has_queues"]
	data["hasFailed"] = snap["has_failed"]
	return c.View("horizon/completed", data)
}

func (p *Plugin) silencedHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	snap := p.snapshot()
	data := p.baseData("silenced", "Silenced jobs")
	data["stats"] = snap
	data["queues"] = snap["queues"]
	data["hasQueues"] = snap["has_queues"]
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

func (p *Plugin) prometheusMetricsHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	c.Response.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	c.Response.WriteHeader(http.StatusOK)
	_, _ = c.Response.Write([]byte(metrics.DefaultRegistry.Expose()))
	return nil
}

func (p *Plugin) workloadsHandler(c *http.Context) error {
	if !p.authorize(c) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "unauthorized"})
	}
	if p.redis == nil {
		return c.JSON(http.StatusOK, map[string]any{
			"workloads":   []queue.RedisQueueWorkload{},
			"redis_ready": false,
			"hint":        "Set Horizon Options.RedisURL when creating the plugin to enable live Redis queue depths.",
		})
	}
	p.stats.mu.RLock()
	seen := make([]string, 0, len(p.stats.PerQueue))
	for _, qs := range p.stats.PerQueue {
		seen = append(seen, qs.Name)
	}
	p.stats.mu.RUnlock()
	names := horizonQueueNames(seen)
	wl, err := queue.RedisQueueWorkloads(c.Request.Context(), p.redis, names)
	if err != nil {
		return c.JSON(http.StatusOK, map[string]any{
			"workloads":   []queue.RedisQueueWorkload{},
			"redis_ready": true,
			"error":       err.Error(),
		})
	}
	return c.JSON(http.StatusOK, map[string]any{"workloads": wl, "redis_ready": true})
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
		"total_retried":    p.stats.TotalRetried,
		"total_reclaimed":  p.stats.TotalReclaimed,
	}
	queues := make([]map[string]any, 0, len(p.stats.PerQueue))
	queueNames := make([]string, 0, len(p.stats.PerQueue))
	for _, qs := range p.stats.PerQueue {
		queueNames = append(queueNames, qs.Name)
		item := map[string]any{
			"name":       qs.Name,
			"dispatched": qs.Dispatched,
			"processed":  qs.Processed,
			"failed":     qs.Failed,
			"retried":    qs.Retried,
			"reclaimed":  qs.Reclaimed,
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

	// Live Redis depths (same client as failed-job store) when configured.
	if p.redis != nil {
		names := horizonQueueNames(queueNames)
		if wl, err := queue.RedisQueueWorkloads(context.Background(), p.redis, names); err == nil {
			out["redis_workloads"] = redisWorkloadRows(wl)
			out["redis_workloads_ok"] = true
		} else {
			out["redis_workloads"] = []map[string]any{}
			out["redis_workloads_ok"] = false
			out["redis_workloads_err"] = err.Error()
		}
	} else {
		out["redis_workloads"] = []map[string]any{}
		out["redis_workloads_ok"] = false
	}
	return out
}

func redisWorkloadRows(wl []queue.RedisQueueWorkload) []map[string]any {
	rows := make([]map[string]any, 0, len(wl))
	for _, w := range wl {
		rows = append(rows, map[string]any{
			"name":        w.Name,
			"pending":     w.Pending,
			"delayed":     w.Delayed,
			"processing":  w.Processing,
			"in_flight":   w.InFlight,
		})
	}
	return rows
}

// horizonQueueNames merges observer-seen queues with HORIZON_QUEUES (comma-separated).
func horizonQueueNames(seen []string) []string {
	extra := os.Getenv("HORIZON_QUEUES")
	if strings.TrimSpace(extra) == "" {
		if len(seen) == 0 {
			return nil
		}
		return seen
	}
	m := make(map[string]struct{})
	for _, s := range seen {
		if s != "" {
			m[s] = struct{}{}
		}
	}
	for _, part := range strings.Split(extra, ",") {
		q := strings.TrimSpace(part)
		if q != "" {
			m[q] = struct{}{}
		}
	}
	out := make([]string, 0, len(m))
	for q := range m {
		out = append(out, q)
	}
	return out
}
