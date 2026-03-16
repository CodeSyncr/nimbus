// Package studio provides a built-in admin panel for Nimbus applications.
//
// Studio auto-discovers GORM models and generates a full CRUD admin interface
// with listing, filtering, sorting, pagination, create/edit forms, and
// relationship management — all from a single plugin registration.
//
// Usage:
//
//	app.RegisterPlugin(studio.NewPlugin(studio.Config{
//	    Path:   "/_studio",
//	    Models: []any{&models.User{}, &models.Post{}},
//	}))
package studio

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
	"gorm.io/gorm"
)

// query is a helper to get a query parameter from the context.
func query(c *nhttp.Context, key string) string {
	return c.Request.URL.Query().Get(key)
}

// bodyParse is a helper to decode the request body as JSON.
func bodyParse(c *nhttp.Context, v any) error {
	return json.NewDecoder(c.Request.Body).Decode(v)
}

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// Config configures the Studio admin panel.
type Config struct {
	// Path prefix for the admin panel (default: "/_studio").
	Path string

	// Models to register in the admin panel.
	Models []any

	// DB is the GORM database connection.
	DB *gorm.DB

	// Title of the admin panel (default: "Nimbus Studio").
	Title string

	// BrandColor for the header (default: "#6366f1").
	BrandColor string

	// Auth middleware to protect the admin panel.
	Auth router.Middleware

	// ReadOnly prevents create/update/delete operations.
	ReadOnly bool

	// PerPage default pagination size (default: 25).
	PerPage int

	// CustomActions for models.
	CustomActions map[string][]ModelAction

	// Dashboard widgets.
	Widgets []Widget
}

// ModelAction defines a custom action on a model.
type ModelAction struct {
	Name        string
	Label       string
	Icon        string
	Handler     func(db *gorm.DB, ids []uint) error
	Bulk        bool // can be applied to multiple records
	Destructive bool
}

// Widget is a dashboard widget.
type Widget struct {
	Title string
	Type  string // "count", "chart", "list", "custom"
	Model string
	Query func(db *gorm.DB) (any, error)
	Width int // 1-4 (grid columns)
}

// ---------------------------------------------------------------------------
// Model Metadata
// ---------------------------------------------------------------------------

// ModelMeta holds introspected metadata about a registered model.
type ModelMeta struct {
	Name       string       `json:"name"`
	Table      string       `json:"table"`
	Fields     []FieldMeta  `json:"fields"`
	Type       reflect.Type `json:"-"`
	Searchable []string     `json:"searchable"`
	Sortable   []string     `json:"sortable"`
	Filterable []string     `json:"filterable"`
}

// FieldMeta describes a model field.
type FieldMeta struct {
	Name       string `json:"name"`
	Column     string `json:"column"`
	Type       string `json:"type"`
	GoType     string `json:"go_type"`
	Primary    bool   `json:"primary,omitempty"`
	Required   bool   `json:"required,omitempty"`
	Unique     bool   `json:"unique,omitempty"`
	Nullable   bool   `json:"nullable,omitempty"`
	Hidden     bool   `json:"hidden,omitempty"`
	ReadOnly   bool   `json:"read_only,omitempty"`
	Sortable   bool   `json:"sortable"`
	Filterable bool   `json:"filterable"`
	Searchable bool   `json:"searchable"`
	InputType  string `json:"input_type"` // text, textarea, number, email, password, select, checkbox, date, datetime
	Relation   string `json:"relation,omitempty"`
}

// ---------------------------------------------------------------------------
// Plugin
// ---------------------------------------------------------------------------

// Plugin is the Nimbus Studio admin panel plugin.
type Plugin struct {
	config Config
	models map[string]*ModelMeta
}

// NewPlugin creates a new Studio plugin.
func NewPlugin(cfg Config) *Plugin {
	if cfg.Path == "" {
		cfg.Path = "/_studio"
	}
	if cfg.Title == "" {
		cfg.Title = "Nimbus Studio"
	}
	if cfg.BrandColor == "" {
		cfg.BrandColor = "#6366f1"
	}
	if cfg.PerPage == 0 {
		cfg.PerPage = 25
	}

	p := &Plugin{
		config: cfg,
		models: make(map[string]*ModelMeta),
	}

	// Introspect models.
	for _, model := range cfg.Models {
		meta := introspectModel(model)
		p.models[meta.Name] = meta
	}

	return p
}

func (p *Plugin) Name() string    { return "studio" }
func (p *Plugin) Version() string { return "1.0.0" }

func (p *Plugin) Register(app interface{}) error { return nil }
func (p *Plugin) Boot(app interface{}) error     { return nil }

// RegisterRoutes mounts the Studio admin panel routes.
func (p *Plugin) RegisterRoutes(r *router.Router) {
	prefix := strings.TrimSuffix(p.config.Path, "/")

	// Dashboard.
	r.Get(prefix, p.handleDashboard)

	// Model API endpoints.
	r.Get(prefix+"/api/models", p.handleListModels)
	r.Get(prefix+"/api/models/:model", p.handleModelMeta)
	r.Get(prefix+"/api/models/:model/records", p.handleListRecords)
	r.Get(prefix+"/api/models/:model/records/:id", p.handleGetRecord)

	if !p.config.ReadOnly {
		r.Post(prefix+"/api/models/:model/records", p.handleCreateRecord)
		r.Put(prefix+"/api/models/:model/records/:id", p.handleUpdateRecord)
		r.Delete(prefix+"/api/models/:model/records/:id", p.handleDeleteRecord)
		r.Post(prefix+"/api/models/:model/actions/:action", p.handleAction)
	}

	// Stats.
	r.Get(prefix+"/api/stats", p.handleStats)

	// UI routes.
	r.Get(prefix+"/models/:model", p.handleModelPage)
	r.Get(prefix+"/models/:model/new", p.handleModelPage)
	r.Get(prefix+"/models/:model/:id", p.handleModelPage)
	r.Get(prefix+"/models/:model/:id/edit", p.handleModelPage)
}

// ---------------------------------------------------------------------------
// API Handlers
// ---------------------------------------------------------------------------

func (p *Plugin) handleListModels(c *nhttp.Context) error {
	var models []map[string]any
	for name, meta := range p.models {
		var count int64
		if p.config.DB != nil {
			p.config.DB.Table(meta.Table).Count(&count)
		}
		models = append(models, map[string]any{
			"name":   name,
			"table":  meta.Table,
			"fields": len(meta.Fields),
			"count":  count,
		})
	}
	return c.JSON(200, map[string]any{"models": models})
}

func (p *Plugin) handleModelMeta(c *nhttp.Context) error {
	name := c.Param("model")
	meta, ok := p.models[name]
	if !ok {
		return c.JSON(404, map[string]string{"error": "Model not found"})
	}
	return c.JSON(200, meta)
}

func (p *Plugin) handleListRecords(c *nhttp.Context) error {
	name := c.Param("model")
	meta, ok := p.models[name]
	if !ok {
		return c.JSON(404, map[string]string{"error": "Model not found"})
	}
	if p.config.DB == nil {
		return c.JSON(500, map[string]string{"error": "Database not configured"})
	}

	dbQ := p.config.DB.Table(meta.Table)

	// Pagination.
	page, _ := strconv.Atoi(query(c, "page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(query(c, "per_page"))
	if perPage < 1 {
		perPage = p.config.PerPage
	}

	// Search.
	if search := query(c, "search"); search != "" && len(meta.Searchable) > 0 {
		var conditions []string
		var args []any
		for _, field := range meta.Searchable {
			conditions = append(conditions, fmt.Sprintf("%s LIKE ?", field))
			args = append(args, "%"+search+"%")
		}
		dbQ = dbQ.Where(strings.Join(conditions, " OR "), args...)
	}

	// Filters.
	for _, field := range meta.Filterable {
		if val := query(c, "filter_"+field); val != "" {
			dbQ = dbQ.Where(fmt.Sprintf("%s = ?", field), val)
		}
	}

	// Sort.
	sortField := query(c, "sort")
	sortDir := query(c, "dir")
	if sortField == "" {
		sortField = "id"
	}
	if sortDir == "" {
		sortDir = "desc"
	}
	// Validate sort field.
	validSort := false
	for _, f := range meta.Sortable {
		if f == sortField {
			validSort = true
			break
		}
	}
	if validSort {
		dbQ = dbQ.Order(fmt.Sprintf("%s %s", sortField, sortDir))
	} else {
		dbQ = dbQ.Order("id DESC")
	}

	// Count total.
	var total int64
	dbQ.Count(&total)

	// Fetch records.
	var records []map[string]any
	offset := (page - 1) * perPage
	if err := dbQ.Offset(offset).Limit(perPage).Find(&records).Error; err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]any{
		"records":  records,
		"total":    total,
		"page":     page,
		"per_page": perPage,
		"pages":    (total + int64(perPage) - 1) / int64(perPage),
	})
}

func (p *Plugin) handleGetRecord(c *nhttp.Context) error {
	name := c.Param("model")
	meta, ok := p.models[name]
	if !ok {
		return c.JSON(404, map[string]string{"error": "Model not found"})
	}
	if p.config.DB == nil {
		return c.JSON(500, map[string]string{"error": "Database not configured"})
	}

	id := c.Param("id")
	var record map[string]any
	if err := p.config.DB.Table(meta.Table).Where("id = ?", id).First(&record).Error; err != nil {
		return c.JSON(404, map[string]string{"error": "Record not found"})
	}

	return c.JSON(200, record)
}

func (p *Plugin) handleCreateRecord(c *nhttp.Context) error {
	name := c.Param("model")
	meta, ok := p.models[name]
	if !ok {
		return c.JSON(404, map[string]string{"error": "Model not found"})
	}
	if p.config.DB == nil {
		return c.JSON(500, map[string]string{"error": "Database not configured"})
	}

	var body map[string]any
	if err := bodyParse(c, &body); err != nil {
		return c.JSON(400, map[string]string{"error": "Invalid request body"})
	}

	// Remove read-only fields.
	for _, f := range meta.Fields {
		if f.ReadOnly {
			delete(body, f.Column)
		}
	}

	// Set timestamps.
	now := time.Now()
	body["created_at"] = now
	body["updated_at"] = now

	if err := p.config.DB.Table(meta.Table).Create(&body).Error; err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(201, body)
}

func (p *Plugin) handleUpdateRecord(c *nhttp.Context) error {
	name := c.Param("model")
	meta, ok := p.models[name]
	if !ok {
		return c.JSON(404, map[string]string{"error": "Model not found"})
	}
	if p.config.DB == nil {
		return c.JSON(500, map[string]string{"error": "Database not configured"})
	}

	id := c.Param("id")
	var body map[string]any
	if err := bodyParse(c, &body); err != nil {
		return c.JSON(400, map[string]string{"error": "Invalid request body"})
	}

	// Remove read-only fields.
	for _, f := range meta.Fields {
		if f.ReadOnly || f.Primary {
			delete(body, f.Column)
		}
	}

	body["updated_at"] = time.Now()

	if err := p.config.DB.Table(meta.Table).Where("id = ?", id).Updates(body).Error; err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]string{"message": "Updated"})
}

func (p *Plugin) handleDeleteRecord(c *nhttp.Context) error {
	name := c.Param("model")
	meta, ok := p.models[name]
	if !ok {
		return c.JSON(404, map[string]string{"error": "Model not found"})
	}
	if p.config.DB == nil {
		return c.JSON(500, map[string]string{"error": "Database not configured"})
	}

	id := c.Param("id")
	if err := p.config.DB.Table(meta.Table).Where("id = ?", id).Delete(nil).Error; err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]string{"message": "Deleted"})
}

func (p *Plugin) handleAction(c *nhttp.Context) error {
	modelName := c.Param("model")
	actionName := c.Param("action")

	actions, ok := p.config.CustomActions[modelName]
	if !ok {
		return c.JSON(404, map[string]string{"error": "No actions for model"})
	}

	var action *ModelAction
	for i := range actions {
		if actions[i].Name == actionName {
			action = &actions[i]
			break
		}
	}
	if action == nil {
		return c.JSON(404, map[string]string{"error": "Action not found"})
	}

	var body struct {
		IDs []uint `json:"ids"`
	}
	if err := bodyParse(c, &body); err != nil {
		return c.JSON(400, map[string]string{"error": "Invalid request"})
	}

	if err := action.Handler(p.config.DB, body.IDs); err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]string{"message": "Action completed"})
}

func (p *Plugin) handleStats(c *nhttp.Context) error {
	stats := make(map[string]any)
	if p.config.DB != nil {
		for name, meta := range p.models {
			var count int64
			p.config.DB.Table(meta.Table).Count(&count)
			stats[name] = map[string]any{
				"count": count,
				"table": meta.Table,
			}
		}
	}

	// Execute widget queries.
	var widgets []map[string]any
	for _, w := range p.config.Widgets {
		widget := map[string]any{
			"title": w.Title,
			"type":  w.Type,
			"width": w.Width,
		}
		if w.Query != nil && p.config.DB != nil {
			data, err := w.Query(p.config.DB)
			if err == nil {
				widget["data"] = data
			}
		}
		widgets = append(widgets, widget)
	}

	return c.JSON(200, map[string]any{
		"models":  stats,
		"widgets": widgets,
	})
}

// ---------------------------------------------------------------------------
// UI Handlers
// ---------------------------------------------------------------------------

func (p *Plugin) handleDashboard(c *nhttp.Context) error {
	c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	html := p.renderDashboardHTML()
	_, err := c.Response.Write([]byte(html))
	return err
}

func (p *Plugin) handleModelPage(c *nhttp.Context) error {
	c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	html := p.renderDashboardHTML()
	_, err := c.Response.Write([]byte(html))
	return err
}

// ---------------------------------------------------------------------------
// Model Introspection
// ---------------------------------------------------------------------------

func introspectModel(model any) *ModelMeta {
	t := reflect.TypeOf(model)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	meta := &ModelMeta{
		Name:  t.Name(),
		Table: guessTableName(t.Name()),
		Type:  t,
	}

	// Check if model implements TableName() string.
	if tn, ok := model.(interface{ TableName() string }); ok {
		meta.Table = tn.TableName()
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		// Handle embedded gorm.Model or anonymous structs.
		if field.Anonymous {
			embeddedType := field.Type
			for embeddedType.Kind() == reflect.Ptr {
				embeddedType = embeddedType.Elem()
			}
			if embeddedType.Kind() == reflect.Struct {
				for j := 0; j < embeddedType.NumField(); j++ {
					ef := embeddedType.Field(j)
					if ef.IsExported() {
						fm := buildFieldMeta(ef)
						meta.Fields = append(meta.Fields, fm)
						addToLists(meta, fm)
					}
				}
			}
			continue
		}

		fm := buildFieldMeta(field)
		meta.Fields = append(meta.Fields, fm)
		addToLists(meta, fm)
	}

	return meta
}

func buildFieldMeta(field reflect.StructField) FieldMeta {
	fm := FieldMeta{
		Name:   field.Name,
		Column: toDBColumn(field),
		GoType: field.Type.String(),
	}

	// Determine type category.
	ft := field.Type
	for ft.Kind() == reflect.Ptr {
		fm.Nullable = true
		ft = ft.Elem()
	}

	switch ft.Kind() {
	case reflect.String:
		fm.Type = "string"
		fm.InputType = "text"
		fm.Searchable = true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		fm.Type = "integer"
		fm.InputType = "number"
		fm.Filterable = true
	case reflect.Float32, reflect.Float64:
		fm.Type = "number"
		fm.InputType = "number"
	case reflect.Bool:
		fm.Type = "boolean"
		fm.InputType = "checkbox"
		fm.Filterable = true
	case reflect.Struct:
		if ft == reflect.TypeOf(time.Time{}) {
			fm.Type = "datetime"
			fm.InputType = "datetime"
			fm.Sortable = true
		} else {
			fm.Type = "object"
			fm.InputType = "text"
		}
	case reflect.Slice:
		fm.Type = "array"
		fm.InputType = "text"
	default:
		fm.Type = "string"
		fm.InputType = "text"
	}

	fm.Sortable = true

	// Parse GORM tags.
	gormTag := field.Tag.Get("gorm")
	if strings.Contains(gormTag, "primaryKey") || strings.Contains(gormTag, "primarykey") {
		fm.Primary = true
		fm.ReadOnly = true
	}
	if strings.Contains(gormTag, "uniqueIndex") || strings.Contains(gormTag, "unique") {
		fm.Unique = true
	}

	// Parse JSON tags.
	jsonTag := field.Tag.Get("json")
	if jsonTag == "-" {
		fm.Hidden = true
	}
	if strings.Contains(jsonTag, ",omitempty") {
		fm.Nullable = true
	}

	// Password fields.
	lower := strings.ToLower(field.Name)
	if lower == "password" || lower == "passwordhash" || lower == "hashedpassword" {
		fm.InputType = "password"
		fm.Hidden = true
		fm.Searchable = false
	}

	// Email fields.
	if lower == "email" {
		fm.InputType = "email"
	}

	// Text fields.
	if lower == "body" || lower == "content" || lower == "description" || lower == "bio" || lower == "notes" || lower == "text" {
		fm.InputType = "textarea"
	}

	// Detect relations.
	if strings.HasSuffix(field.Name, "ID") && field.Name != "ID" {
		fm.Relation = strings.TrimSuffix(field.Name, "ID")
	}

	// Validate tag.
	validate := field.Tag.Get("validate")
	if strings.Contains(validate, "required") {
		fm.Required = true
	}

	// Studio tag for custom configuration.
	studioTag := field.Tag.Get("studio")
	if studioTag != "" {
		parseStudioTag(&fm, studioTag)
	}

	return fm
}

func parseStudioTag(fm *FieldMeta, tag string) {
	parts := strings.Split(tag, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		switch {
		case part == "hidden":
			fm.Hidden = true
		case part == "readonly":
			fm.ReadOnly = true
		case part == "required":
			fm.Required = true
		case strings.HasPrefix(part, "input:"):
			fm.InputType = part[6:]
		case strings.HasPrefix(part, "label:"):
			fm.Name = part[6:]
		}
	}
}

func addToLists(meta *ModelMeta, fm FieldMeta) {
	if fm.Searchable {
		meta.Searchable = append(meta.Searchable, fm.Column)
	}
	if fm.Sortable {
		meta.Sortable = append(meta.Sortable, fm.Column)
	}
	if fm.Filterable {
		meta.Filterable = append(meta.Filterable, fm.Column)
	}
}

func toDBColumn(field reflect.StructField) string {
	// Check gorm tag first.
	gormTag := field.Tag.Get("gorm")
	for _, part := range strings.Split(gormTag, ";") {
		if strings.HasPrefix(part, "column:") {
			return part[7:]
		}
	}
	// Check json tag.
	jsonTag := field.Tag.Get("json")
	if jsonTag != "" && jsonTag != "-" {
		parts := strings.Split(jsonTag, ",")
		if parts[0] != "" {
			return parts[0]
		}
	}
	// Default: snake_case.
	return camelToSnake(field.Name)
}

func camelToSnake(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteRune('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

func guessTableName(name string) string {
	snake := camelToSnake(name)
	// Simple pluralize.
	if strings.HasSuffix(snake, "s") {
		return snake + "es"
	}
	if strings.HasSuffix(snake, "y") {
		return snake[:len(snake)-1] + "ies"
	}
	return snake + "s"
}

// ---------------------------------------------------------------------------
// Dashboard HTML — Single Page App
// ---------------------------------------------------------------------------

func (p *Plugin) renderDashboardHTML() string {
	prefix := strings.TrimSuffix(p.config.Path, "/")
	modelsJSON, _ := json.Marshal(p.getModelsList())

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>
<style>
:root {
  --brand: %s;
  --bg: #0f172a;
  --sidebar: #1e293b;
  --surface: #1e293b;
  --surface2: #334155;
  --text: #e2e8f0;
  --text-dim: #94a3b8;
  --accent: %s;
  --green: #22c55e;
  --red: #ef4444;
  --yellow: #eab308;
  --mono: 'SF Mono', 'Cascadia Code', 'Fira Code', monospace;
}
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: Inter, system-ui, sans-serif; background: var(--bg); color: var(--text); display: flex; min-height: 100vh; }

/* Sidebar */
.sidebar { width: 260px; background: var(--sidebar); border-right: 1px solid var(--surface2); display: flex; flex-direction: column; }
.sidebar-brand { padding: 20px; font-size: 18px; font-weight: 800; color: var(--accent); border-bottom: 1px solid var(--surface2); letter-spacing: -0.5px; }
.sidebar-nav { flex: 1; padding: 12px 0; overflow-y: auto; }
.nav-section { padding: 8px 20px; font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: 1px; color: var(--text-dim); }
.nav-item { display: flex; align-items: center; gap: 10px; padding: 10px 20px; color: var(--text-dim); cursor: pointer; font-size: 14px; border-left: 3px solid transparent; transition: all 0.15s; }
.nav-item:hover { background: rgba(99,102,241,0.08); color: var(--text); }
.nav-item.active { border-left-color: var(--accent); color: var(--text); background: rgba(99,102,241,0.12); }
.nav-item .count { margin-left: auto; font-size: 12px; background: var(--surface2); padding: 1px 8px; border-radius: 10px; color: var(--text-dim); }

/* Main */
.main { flex: 1; display: flex; flex-direction: column; overflow-x: hidden; }
.topbar { height: 60px; border-bottom: 1px solid var(--surface2); display: flex; align-items: center; padding: 0 24px; gap: 16px; }
.topbar-title { font-size: 18px; font-weight: 700; }
.topbar-actions { margin-left: auto; display: flex; gap: 8px; }
.btn { padding: 8px 16px; border-radius: 6px; font-size: 13px; font-weight: 600; cursor: pointer; border: none; transition: all 0.15s; }
.btn-primary { background: var(--accent); color: white; }
.btn-primary:hover { filter: brightness(1.1); }
.btn-danger { background: var(--red); color: white; }
.btn-ghost { background: transparent; color: var(--text-dim); border: 1px solid var(--surface2); }
.btn-ghost:hover { color: var(--text); border-color: var(--text-dim); }

/* Content */
.content { flex: 1; padding: 24px; overflow-y: auto; }

/* Dashboard */
.stats-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 16px; margin-bottom: 24px; }
.stat-card { background: var(--surface); border: 1px solid var(--surface2); border-radius: 12px; padding: 20px; }
.stat-card .label { font-size: 13px; color: var(--text-dim); margin-bottom: 4px; }
.stat-card .value { font-size: 28px; font-weight: 800; color: var(--text); }

/* Table */
.table-container { background: var(--surface); border: 1px solid var(--surface2); border-radius: 12px; overflow: hidden; }
.table-toolbar { padding: 16px; display: flex; gap: 12px; border-bottom: 1px solid var(--surface2); }
.search-input { background: var(--bg); border: 1px solid var(--surface2); border-radius: 6px; padding: 8px 12px; color: var(--text); font-size: 13px; flex: 1; max-width: 320px; outline: none; }
.search-input:focus { border-color: var(--accent); }
table { width: 100%%; border-collapse: collapse; }
th { text-align: left; padding: 10px 16px; font-size: 12px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.5px; color: var(--text-dim); background: rgba(0,0,0,0.2); cursor: pointer; user-select: none; border-bottom: 1px solid var(--surface2); }
th:hover { color: var(--text); }
td { padding: 10px 16px; font-size: 13px; border-bottom: 1px solid rgba(51,65,85,0.5); }
tr:hover td { background: rgba(99,102,241,0.04); }
.cell-id { font-family: var(--mono); font-size: 12px; color: var(--text-dim); }
.cell-bool-true { color: var(--green); }
.cell-bool-false { color: var(--text-dim); }
.cell-null { color: var(--text-dim); font-style: italic; font-size: 12px; }
.cell-actions { display: flex; gap: 6px; }
.cell-actions button { background: none; border: none; cursor: pointer; padding: 4px; border-radius: 4px; color: var(--text-dim); }
.cell-actions button:hover { background: var(--surface2); color: var(--text); }
.pagination { padding: 12px 16px; display: flex; align-items: center; justify-content: space-between; border-top: 1px solid var(--surface2); font-size: 13px; color: var(--text-dim); }
.pagination button { background: var(--surface2); border: none; color: var(--text); padding: 6px 12px; border-radius: 4px; cursor: pointer; font-size: 13px; }
.pagination button:disabled { opacity: 0.4; cursor: not-allowed; }

/* Form */
.form-container { background: var(--surface); border: 1px solid var(--surface2); border-radius: 12px; padding: 24px; max-width: 640px; }
.form-group { margin-bottom: 16px; }
.form-group label { display: block; font-size: 13px; font-weight: 600; color: var(--text-dim); margin-bottom: 6px; }
.form-group input, .form-group textarea, .form-group select { width: 100%%; background: var(--bg); border: 1px solid var(--surface2); border-radius: 6px; padding: 8px 12px; color: var(--text); font-size: 13px; outline: none; font-family: inherit; }
.form-group input:focus, .form-group textarea:focus { border-color: var(--accent); }
.form-group textarea { min-height: 100px; resize: vertical; }
.form-group .checkbox-wrap { display: flex; align-items: center; gap: 8px; }
.form-group .checkbox-wrap input { width: auto; }
.form-actions { display: flex; gap: 8px; margin-top: 24px; }

/* Responsive */
@media (max-width: 768px) {
  .sidebar { width: 60px; }
  .sidebar .nav-item span, .sidebar .nav-section, .sidebar .nav-item .count { display: none; }
  .sidebar-brand { font-size: 14px; padding: 16px; text-align: center; }
}
</style>
</head>
<body>
<div class="sidebar">
  <div class="sidebar-brand">%s</div>
  <nav class="sidebar-nav">
    <div class="nav-section">Dashboard</div>
    <div class="nav-item active" onclick="showDashboard()">
      <span>Overview</span>
    </div>
    <div class="nav-section">Models</div>
    <div id="model-nav"></div>
  </nav>
</div>
<div class="main">
  <div class="topbar">
    <div class="topbar-title" id="page-title">Dashboard</div>
    <div class="topbar-actions" id="page-actions"></div>
  </div>
  <div class="content" id="content">
    <div class="stats-grid" id="stats"></div>
  </div>
</div>

<script>
const PREFIX = '%s';
const models = %s;
let currentModel = null;
let currentPage = 1;
let currentSort = 'id';
let currentDir = 'desc';
let currentSearch = '';

// Build navigation.
const nav = document.getElementById('model-nav');
models.forEach(m => {
  const item = document.createElement('div');
  item.className = 'nav-item';
  item.innerHTML = '<span>' + m.name + '</span><span class="count">' + (m.count||0) + '</span>';
  item.onclick = () => showModel(m.name);
  nav.appendChild(item);
});

async function api(path, opts) {
  const res = await fetch(PREFIX + '/api' + path, {
    headers: {'Content-Type': 'application/json'},
    ...opts
  });
  return res.json();
}

async function showDashboard() {
  document.getElementById('page-title').textContent = 'Dashboard';
  document.getElementById('page-actions').innerHTML = '';
  currentModel = null;

  const data = await api('/stats');
  let html = '<div class="stats-grid">';
  for (const [name, info] of Object.entries(data.models || {})) {
    html += '<div class="stat-card"><div class="label">' + name + '</div><div class="value">' + (info.count || 0) + '</div></div>';
  }
  html += '</div>';
  document.getElementById('content').innerHTML = html;
  setActiveNav(-1);
}

async function showModel(name) {
  currentModel = name;
  currentPage = 1;
  currentSearch = '';
  document.getElementById('page-title').textContent = name;
  document.getElementById('page-actions').innerHTML = %v
    ? ''
    : '<button class="btn btn-primary" onclick="showCreateForm()">+ Create</button>';
  setActiveNav(models.findIndex(m => m.name === name));
  await loadRecords();
}

async function loadRecords() {
  const meta = await api('/models/' + currentModel);
  const params = new URLSearchParams({
    page: currentPage, sort: currentSort, dir: currentDir, search: currentSearch
  });
  const data = await api('/models/' + currentModel + '/records?' + params);

  let html = '<div class="table-container">';
  html += '<div class="table-toolbar"><input class="search-input" placeholder="Search..." value="' + currentSearch + '" oninput="debounceSearch(this.value)"></div>';
  html += '<table><thead><tr>';

  const fields = (meta.fields || []).filter(f => !f.hidden);
  fields.forEach(f => {
    const arrow = currentSort === f.column ? (currentDir === 'asc' ? ' ↑' : ' ↓') : '';
    html += '<th onclick="sortBy(\'' + f.column + '\')">' + f.name + arrow + '</th>';
  });
  html += '<th>Actions</th></tr></thead><tbody>';

  (data.records || []).forEach(r => {
    html += '<tr>';
    fields.forEach(f => {
      const v = r[f.column];
      if (v === null || v === undefined) html += '<td><span class="cell-null">null</span></td>';
      else if (f.type === 'boolean') html += '<td class="' + (v ? 'cell-bool-true' : 'cell-bool-false') + '">' + (v ? '✓' : '✗') + '</td>';
      else if (f.primary) html += '<td class="cell-id">' + v + '</td>';
      else html += '<td>' + String(v).substring(0, 100) + '</td>';
    });
    html += '<td class="cell-actions">';
    html += '<button onclick="showEditForm(' + r.id + ')" title="Edit">✏</button>';
    %s
    html += '</td></tr>';
  });

  html += '</tbody></table>';
  html += '<div class="pagination">';
  html += '<span>Page ' + data.page + ' of ' + data.pages + ' (' + data.total + ' records)</span>';
  html += '<div>';
  html += '<button onclick="goPage(' + (data.page-1) + ')" ' + (data.page<=1?'disabled':'') + '>← Prev</button> ';
  html += '<button onclick="goPage(' + (data.page+1) + ')" ' + (data.page>=data.pages?'disabled':'') + '>Next →</button>';
  html += '</div></div></div>';

  document.getElementById('content').innerHTML = html;
}

function sortBy(col) {
  if (currentSort === col) currentDir = currentDir === 'asc' ? 'desc' : 'asc';
  else { currentSort = col; currentDir = 'asc'; }
  loadRecords();
}

function goPage(p) { currentPage = p; loadRecords(); }

let searchTimeout;
function debounceSearch(val) {
  clearTimeout(searchTimeout);
  searchTimeout = setTimeout(() => { currentSearch = val; currentPage = 1; loadRecords(); }, 300);
}

async function showCreateForm() {
  const meta = await api('/models/' + currentModel);
  const fields = (meta.fields || []).filter(f => !f.hidden && !f.read_only && !f.primary);
  let html = '<div class="form-container"><h3 style="margin-bottom:16px">Create ' + currentModel + '</h3>';
  fields.forEach(f => {
    html += '<div class="form-group"><label>' + f.name + (f.required ? ' *' : '') + '</label>';
    if (f.input_type === 'textarea') html += '<textarea id="f_' + f.column + '"></textarea>';
    else if (f.input_type === 'checkbox') html += '<div class="checkbox-wrap"><input type="checkbox" id="f_' + f.column + '"><span>Enabled</span></div>';
    else html += '<input type="' + (f.input_type||'text') + '" id="f_' + f.column + '">';
    html += '</div>';
  });
  html += '<div class="form-actions"><button class="btn btn-primary" onclick="submitCreate()">Create</button><button class="btn btn-ghost" onclick="loadRecords()">Cancel</button></div></div>';
  document.getElementById('content').innerHTML = html;
}

async function showEditForm(id) {
  const meta = await api('/models/' + currentModel);
  const record = await api('/models/' + currentModel + '/records/' + id);
  const fields = (meta.fields || []).filter(f => !f.hidden && !f.primary);
  let html = '<div class="form-container"><h3 style="margin-bottom:16px">Edit ' + currentModel + ' #' + id + '</h3>';
  fields.forEach(f => {
    const v = record[f.column] || '';
    const ro = f.read_only ? 'disabled' : '';
    html += '<div class="form-group"><label>' + f.name + '</label>';
    if (f.input_type === 'textarea') html += '<textarea id="f_' + f.column + '" ' + ro + '>' + v + '</textarea>';
    else if (f.input_type === 'checkbox') html += '<div class="checkbox-wrap"><input type="checkbox" id="f_' + f.column + '" ' + (v ? 'checked' : '') + ' ' + ro + '><span>Enabled</span></div>';
    else html += '<input type="' + (f.input_type||'text') + '" id="f_' + f.column + '" value="' + v + '" ' + ro + '>';
    html += '</div>';
  });
  html += '<div class="form-actions"><button class="btn btn-primary" onclick="submitUpdate(' + id + ')">Save</button><button class="btn btn-ghost" onclick="loadRecords()">Cancel</button></div></div>';
  document.getElementById('content').innerHTML = html;
}

async function submitCreate() {
  const meta = await api('/models/' + currentModel);
  const fields = (meta.fields || []).filter(f => !f.hidden && !f.read_only && !f.primary);
  const body = {};
  fields.forEach(f => {
    const el = document.getElementById('f_' + f.column);
    if (!el) return;
    if (f.input_type === 'checkbox') body[f.column] = el.checked;
    else if (f.type === 'integer' || f.type === 'number') body[f.column] = Number(el.value);
    else body[f.column] = el.value;
  });
  await api('/models/' + currentModel + '/records', { method: 'POST', body: JSON.stringify(body) });
  loadRecords();
}

async function submitUpdate(id) {
  const meta = await api('/models/' + currentModel);
  const fields = (meta.fields || []).filter(f => !f.hidden && !f.read_only && !f.primary);
  const body = {};
  fields.forEach(f => {
    const el = document.getElementById('f_' + f.column);
    if (!el) return;
    if (f.input_type === 'checkbox') body[f.column] = el.checked;
    else if (f.type === 'integer' || f.type === 'number') body[f.column] = Number(el.value);
    else body[f.column] = el.value;
  });
  await api('/models/' + currentModel + '/records/' + id, { method: 'PUT', body: JSON.stringify(body) });
  loadRecords();
}

async function deleteRecord(id) {
  if (!confirm('Delete record #' + id + '?')) return;
  await api('/models/' + currentModel + '/records/' + id, { method: 'DELETE' });
  loadRecords();
}

function setActiveNav(idx) {
  document.querySelectorAll('.nav-item').forEach((el, i) => {
    el.classList.toggle('active', i === idx + 1); // +1 for dashboard item
  });
}

showDashboard();
</script>
</body>
</html>`,
		p.config.Title,
		p.config.BrandColor,
		p.config.BrandColor,
		p.config.Title,
		prefix,
		string(modelsJSON),
		p.config.ReadOnly,
		func() string {
			if p.config.ReadOnly {
				return ""
			}
			return `'<button onclick="deleteRecord(' + r.id + ')" title="Delete">🗑</button>';`
		}(),
	)
}

func (p *Plugin) getModelsList() []map[string]any {
	var list []map[string]any
	for name, meta := range p.models {
		var count int64
		if p.config.DB != nil {
			p.config.DB.Table(meta.Table).Count(&count)
		}
		list = append(list, map[string]any{
			"name":   name,
			"table":  meta.Table,
			"fields": len(meta.Fields),
			"count":  count,
		})
	}
	return list
}
