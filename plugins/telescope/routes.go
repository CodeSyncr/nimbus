package telescope

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/metrics"
	"github.com/CodeSyncr/nimbus/router"
)

func entriesB64(entries []*Entry) string {
	if entries == nil {
		return ""
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}

func selectedFromList(c *http.Context, p *Plugin, entries []*Entry, typ EntryType) (*Entry, string) {
	selectedID := c.Request.URL.Query().Get("selected")
	if selectedID != "" {
		e := p.store.Get(selectedID)
		if e != nil && e.Type == typ {
			return e, selectedID
		}
	}
	if len(entries) == 0 {
		return nil, ""
	}
	return entries[0], entries[0].ID
}

// RegisterRoutes mounts the Telescope dashboard routes.
func (p *Plugin) RegisterRoutes(r *router.Router) {
	if os.Getenv("APP_ENV") == "production" && os.Getenv("TELESCOPE_ENABLED") != "true" {
		return
	}
	grp := r.Group(p.basePath)
	grp.Get("/api/entries", p.apiEntriesHandler)
	grp.Get("/", p.dashboardHandler)
	grp.Get("/requests", p.requestsHandler)
	grp.Get("/requests/:id", p.requestDetailHandler)
	grp.Get("/entry/:id", p.entryDetailHandler)
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

func entryTypeFromString(s string) (EntryType, bool) {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "request", "requests":
		return EntryRequest, true
	case "command", "commands":
		return EntryCommand, true
	case "schedule", "scheduler":
		return EntrySchedule, true
	case "job", "jobs":
		return EntryJob, true
	case "batch", "batches":
		return EntryBatch, true
	case "cache":
		return EntryCache, true
	case "dump", "dumps":
		return EntryDump, true
	case "event", "events":
		return EntryEvent, true
	case "exception", "exceptions":
		return EntryException, true
	case "gate", "gates":
		return EntryGate, true
	case "http_client", "http-client", "httpclient":
		return EntryHTTPClient, true
	case "log", "logs":
		return EntryLog, true
	case "mail":
		return EntryMail, true
	case "model", "models":
		return EntryModel, true
	case "notification", "notifications":
		return EntryNotification, true
	case "query", "queries":
		return EntryQuery, true
	case "redis":
		return EntryRedis, true
	case "view", "views":
		return EntryView, true
	default:
		return "", false
	}
}

func (p *Plugin) apiEntriesHandler(c *http.Context) error {
	typRaw := c.Request.URL.Query().Get("type")
	typ, ok := entryTypeFromString(typRaw)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid type"})
	}
	limit := 50
	if v := strings.TrimSpace(c.Request.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	entries := p.store.Entries(typ, limit)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	return c.JSON(http.StatusOK, map[string]any{
		"entries":   entries,
		"tagCounts": tagCounts(entries),
	})
}

func (p *Plugin) viewData(watcher string) map[string]any {
	return map[string]any{
		"watcher":  watcher,
		"basePath": p.basePath,
	}
}

func (p *Plugin) dashboardHandler(c *http.Context) error {
	entries := p.store.All(20)
	data := p.viewData("dashboard")
	data["title"] = "Dashboard"
	data["entries"] = entries
	data["entriesB64"] = entriesB64(entries)
	data["count"] = len(entries)
	data["empty"] = len(entries) == 0
	data["path"] = p.basePath
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
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)

	selectedID := c.Request.URL.Query().Get("selected")
	var selected *Entry
	if selectedID != "" {
		e := p.store.Get(selectedID)
		if e != nil && e.Type == EntryRequest {
			selected = e
		}
	}
	if selected == nil && len(entries) > 0 {
		// Default to first visible entry for the Pulse list/detail layout.
		selected = entries[0]
		selectedID = selected.ID
	}

	data := p.viewData("requests")
	data["title"] = "Requests"
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	data["selectedID"] = selectedID
	data["selectedEntry"] = selected
	data["entriesB64"] = entriesB64(entries)
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
	// Keep the old route for compatibility, but use the new Pulse list/detail screen.
	if strings.Contains(c.Request.Header.Get("Accept"), "text/html") {
		c.Redirect(http.StatusSeeOther, p.basePath+"/requests?selected="+id)
		return nil
	}
	return c.JSON(http.StatusOK, entry)
}

func (p *Plugin) entryDetailHandler(c *http.Context) error {
	id := c.Param("id")
	entry := p.store.Get(id)
	if entry == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	raw, _ := json.MarshalIndent(entry, "", "  ")
	data := p.viewData(string(entry.Type))
	data["title"] = string(entry.Type) + " · " + id
	data["entry"] = entry
	data["entryJSON"] = string(raw)
	return c.View("telescope/entry-detail", data)
}

func (p *Plugin) commandsHandler(c *http.Context) error {
	entries := p.store.Entries(EntryCommand, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("commands")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Commands"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntryCommand)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func (p *Plugin) scheduleHandler(c *http.Context) error {
	entries := p.store.Entries(EntrySchedule, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("schedule")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Schedule"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntrySchedule)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func (p *Plugin) jobsHandler(c *http.Context) error {
	entries := p.store.Entries(EntryJob, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("jobs")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Jobs"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntryJob)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func (p *Plugin) batchesHandler(c *http.Context) error {
	entries := p.store.Entries(EntryBatch, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("batches")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Batches"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntryBatch)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func (p *Plugin) cacheHandler(c *http.Context) error {
	entries := p.store.Entries(EntryCache, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("cache")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Cache"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntryCache)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func (p *Plugin) dumpsHandler(c *http.Context) error {
	entries := p.store.Entries(EntryDump, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("dumps")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Dumps"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntryDump)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func (p *Plugin) eventsHandler(c *http.Context) error {
	entries := p.store.Entries(EntryEvent, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("events")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Events"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntryEvent)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func (p *Plugin) exceptionsHandler(c *http.Context) error {
	entries := p.store.Entries(EntryException, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("exceptions")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Exceptions"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntryException)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func (p *Plugin) gatesHandler(c *http.Context) error {
	entries := p.store.Entries(EntryGate, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("gates")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Gates"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntryGate)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func (p *Plugin) httpClientHandler(c *http.Context) error {
	entries := p.store.Entries(EntryHTTPClient, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("http_client")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "HTTP Client"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntryHTTPClient)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func (p *Plugin) logsHandler(c *http.Context) error {
	entries := p.store.Entries(EntryLog, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("logs")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Logs"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntryLog)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func (p *Plugin) mailHandler(c *http.Context) error {
	entries := p.store.Entries(EntryMail, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("mail")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Mail"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntryMail)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func (p *Plugin) modelsHandler(c *http.Context) error {
	entries := p.store.Entries(EntryModel, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("models")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Models"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntryModel)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func (p *Plugin) notificationsHandler(c *http.Context) error {
	entries := p.store.Entries(EntryNotification, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("notifications")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Notifications"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntryNotification)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func (p *Plugin) queriesHandler(c *http.Context) error {
	entries := p.store.Entries(EntryQuery, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("queries")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Queries"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntryQuery)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func (p *Plugin) redisHandler(c *http.Context) error {
	entries := p.store.Entries(EntryRedis, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("redis")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Redis"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntryRedis)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func (p *Plugin) viewsHandler(c *http.Context) error {
	entries := p.store.Entries(EntryView, 50)
	q := c.Request.URL.Query().Get("q")
	entries = filterEntries(entries, q)
	tag := c.Request.URL.Query().Get("tag")
	entries = filterByTag(entries, tag)
	data := p.viewData("views")
	data["entries"] = entries
	data["empty"] = len(entries) == 0
	data["title"] = "Views"
	data["q"] = q
	data["tag"] = tag
	data["tagCounts"] = tagCounts(entries)
	sel, selID := selectedFromList(c, p, entries, EntryView)
	data["selectedEntry"] = sel
	data["selectedID"] = selID
	data["entriesB64"] = entriesB64(entries)
	return c.View("telescope/entries", data)
}

func filterEntries(entries []*Entry, q string) []*Entry {
	q = strings.TrimSpace(strings.ToLower(q))
	if q == "" {
		return entries
	}
	out := make([]*Entry, 0, len(entries))
	for _, e := range entries {
		if e == nil {
			continue
		}
		if strings.Contains(strings.ToLower(e.ID), q) || strings.Contains(strings.ToLower(string(e.Type)), q) {
			out = append(out, e)
			continue
		}
		b, _ := json.Marshal(e.Content)
		if bytes.Contains(bytes.ToLower(b), []byte(q)) {
			out = append(out, e)
		}
	}
	return out
}

func filterByTag(entries []*Entry, tag string) []*Entry {
	tag = strings.TrimSpace(strings.ToLower(tag))
	if tag == "" {
		return entries
	}
	out := make([]*Entry, 0, len(entries))
	for _, e := range entries {
		if e == nil {
			continue
		}
		for _, t := range e.Tags {
			if strings.ToLower(t) == tag {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

func tagCounts(entries []*Entry) map[string]int {
	out := map[string]int{}
	for _, e := range entries {
		if e == nil {
			continue
		}
		for _, t := range e.Tags {
			if t == "" {
				continue
			}
			out[t]++
		}
	}
	return out
}

func (p *Plugin) clearHandler(c *http.Context) error {
	p.store.Clear()
	if strings.Contains(c.Request.Header.Get("Accept"), "text/html") {
		// Redirect back to the current Telescope panel when possible (e.g. clear logs stays on /logs).
		ref := strings.TrimSpace(c.Request.Referer())
		if ref != "" && strings.Contains(ref, p.basePath) {
			c.Redirect(http.StatusSeeOther, ref)
			return nil
		}
		c.Redirect(http.StatusSeeOther, p.basePath)
		return nil
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "cleared"})
}
