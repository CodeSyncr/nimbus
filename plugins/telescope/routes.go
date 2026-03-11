package telescope

import (
	"os"
	"strings"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/metrics"
	"github.com/CodeSyncr/nimbus/router"
)

// RegisterRoutes mounts the Telescope dashboard routes.
func (p *Plugin) RegisterRoutes(r *router.Router) {
	if os.Getenv("APP_ENV") == "production" && os.Getenv("TELESCOPE_ENABLED") != "true" {
		return
	}
	path := "/telescope"
	if pth := os.Getenv("TELESCOPE_PATH"); pth != "" {
		path = pth
	}
	grp := r.Group(path)
	grp.Get("/", p.dashboardHandler)
	grp.Get("/requests", p.requestsHandler)
	grp.Get("/requests/:id", p.requestDetailHandler)
	grp.Get("/commands", p.commandsHandler)
	grp.Get("/schedule", p.scheduleHandler)
	grp.Get("/jobs", p.jobsHandler)
	grp.Get("/batches", p.batchesHandler)
	grp.Get("/cache", p.cacheHandler)
	grp.Get("/dumps", p.dumpsHandler)
	grp.Get("/events", p.eventsHandler)
	grp.Get("/exceptions", p.exceptionsHandler)
	grp.Get("/gates", p.gatesHandler)
	grp.Get("/http-client", p.httpClientHandler)
	grp.Get("/logs", p.logsHandler)
	grp.Get("/mail", p.mailHandler)
	grp.Get("/models", p.modelsHandler)
	grp.Get("/notifications", p.notificationsHandler)
	grp.Get("/queries", p.queriesHandler)
	grp.Get("/redis", p.redisHandler)
	grp.Get("/views", p.viewsHandler)
	grp.Post("/clear", p.clearHandler)
}

func (p *Plugin) viewData(watcher string) map[string]any {
	return map[string]any{"watcher": watcher}
}

func (p *Plugin) dashboardHandler(c *http.Context) error {
	entries := p.store.All(20)
	data := p.viewData("dashboard")
	data["entries"] = entries
	data["count"] = len(entries)
	data["empty"] = len(entries) == 0
	data["path"] = "/telescope"
	// Flatten runtime stats into a map so templates can use index with string keys.
	rs := metrics.ReadRuntimeStats()
	data["runtime"] = map[string]any{
		"goroutines": rs.Goroutines,
		"num_gc":     rs.NumGC,
		"heap_alloc": rs.HeapAlloc,
	}
	return c.View("telescope/dashboard", data)
}

func (p *Plugin) requestsHandler(c *http.Context) error {
	entries := p.store.Entries(EntryRequest, 50)
	data := p.viewData("requests")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	return c.View("telescope/requests", data)
}

func (p *Plugin) requestDetailHandler(c *http.Context) error {
	id := c.Param("id")
	entry := p.store.Get(id)
	if entry == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	if entry.Type != EntryRequest {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not a request entry"})
	}
	data := p.viewData("requests")
	data["title"] = "Request " + id
	data["entry"] = entry
	return c.View("telescope/request-detail", data)
}

func (p *Plugin) commandsHandler(c *http.Context) error {
	data := p.viewData("commands")
	data["title"] = "Commands"
	data["message"] = "CLI command watcher. Records nimbus make:*, db:migrate, etc. Coming soon."
	return c.View("telescope/placeholder", data)
}

func (p *Plugin) scheduleHandler(c *http.Context) error {
	data := p.viewData("schedule")
	data["title"] = "Schedule"
	data["message"] = "Scheduled task watcher. Coming soon."
	return c.View("telescope/placeholder", data)
}

func (p *Plugin) jobsHandler(c *http.Context) error {
	data := p.viewData("jobs")
	data["title"] = "Jobs"
	data["message"] = "Queued job watcher. Coming soon."
	return c.View("telescope/placeholder", data)
}

func (p *Plugin) batchesHandler(c *http.Context) error {
	data := p.viewData("batches")
	data["title"] = "Batches"
	data["message"] = "Job batch watcher. Coming soon."
	return c.View("telescope/placeholder", data)
}

func (p *Plugin) cacheHandler(c *http.Context) error {
	data := p.viewData("cache")
	data["title"] = "Cache"
	data["message"] = "Cache operation watcher (get, set, delete). Coming soon."
	return c.View("telescope/placeholder", data)
}

func (p *Plugin) dumpsHandler(c *http.Context) error {
	entries := p.store.Entries(EntryDump, 50)
	data := p.viewData("dumps")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Dumps"
	return c.View("telescope/dumps", data)
}

func (p *Plugin) eventsHandler(c *http.Context) error {
	data := p.viewData("events")
	data["title"] = "Events"
	data["message"] = "Event dispatcher watcher. Coming soon."
	return c.View("telescope/placeholder", data)
}

func (p *Plugin) exceptionsHandler(c *http.Context) error {
	entries := p.store.Entries(EntryException, 50)
	data := p.viewData("exceptions")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	return c.View("telescope/exceptions", data)
}

func (p *Plugin) gatesHandler(c *http.Context) error {
	data := p.viewData("gates")
	data["title"] = "Gates"
	data["message"] = "Authorization gate watcher. Coming soon."
	return c.View("telescope/placeholder", data)
}

func (p *Plugin) httpClientHandler(c *http.Context) error {
	data := p.viewData("http_client")
	data["title"] = "HTTP Client"
	data["message"] = "Outgoing HTTP request watcher. Coming soon."
	return c.View("telescope/placeholder", data)
}

func (p *Plugin) logsHandler(c *http.Context) error {
	entries := p.store.Entries(EntryLog, 50)
	data := p.viewData("logs")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	return c.View("telescope/logs", data)
}

func (p *Plugin) mailHandler(c *http.Context) error {
	data := p.viewData("mail")
	data["title"] = "Mail"
	data["message"] = "Mail watcher. Coming soon."
	return c.View("telescope/placeholder", data)
}

func (p *Plugin) modelsHandler(c *http.Context) error {
	entries := p.store.Entries(EntryModel, 50)
	data := p.viewData("models")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Models"
	return c.View("telescope/models", data)
}

func (p *Plugin) notificationsHandler(c *http.Context) error {
	data := p.viewData("notifications")
	data["title"] = "Notifications"
	data["message"] = "Notification watcher. Coming soon."
	return c.View("telescope/placeholder", data)
}

func (p *Plugin) queriesHandler(c *http.Context) error {
	entries := p.store.Entries(EntryQuery, 50)
	data := p.viewData("queries")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	return c.View("telescope/queries", data)
}

func (p *Plugin) redisHandler(c *http.Context) error {
	data := p.viewData("redis")
	data["title"] = "Redis"
	data["message"] = "Redis command watcher. Coming soon."
	return c.View("telescope/placeholder", data)
}

func (p *Plugin) viewsHandler(c *http.Context) error {
	entries := p.store.Entries(EntryView, 50)
	data := p.viewData("views")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Views"
	return c.View("telescope/views", data)
}

func (p *Plugin) clearHandler(c *http.Context) error {
	p.store.Clear()
	if strings.Contains(c.Request.Header.Get("Accept"), "text/html") {
		c.Redirect(http.StatusSeeOther, "/telescope")
		return nil
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "cleared"})
}
