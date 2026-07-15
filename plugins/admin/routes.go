package admin

import (
	"reflect"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// RegisterRoutes mounts the admin panel under the configured prefix, behind any
// configured gate middleware:
//
//	GET  /admin                    dashboard
//	GET  /admin/:slug              list (paginated)
//	GET  /admin/:slug/create       create form
//	POST /admin/:slug              store
//	GET  /admin/:slug/:id/edit     edit form
//	POST /admin/:slug/:id          update
//	POST /admin/:slug/:id/delete   destroy
func (p *Plugin) RegisterRoutes(r *router.Router) {
	g := r.Group(p.panel.cfg.RoutePrefix, p.panel.cfg.Middleware...)
	g.Get("", p.dashboard)
	g.Get("/:slug", p.index)
	g.Get("/:slug/create", p.createForm)
	g.Post("/:slug", p.store)
	g.Get("/:slug/:id/edit", p.editForm)
	g.Post("/:slug/:id", p.update)
	g.Post("/:slug/:id/delete", p.destroy)
}

func (p *Plugin) dashboard(c *nhttp.Context) error {
	cards := make([]map[string]any, 0, len(p.panel.order))
	for _, slug := range p.panel.order {
		r := p.panel.resources[slug]
		var count int64
		p.panel.db.WithContext(c.Ctx()).Model(r.newPtr()).Count(&count)
		cards = append(cards, map[string]any{"label": r.Label, "slug": r.Slug, "count": count})
	}
	return c.View("admin/dashboard", p.base(c, map[string]any{
		"title": "Dashboard",
		"cards": cards,
	}))
}

func (p *Plugin) index(c *nhttp.Context) error {
	r, ok := p.panel.resources[c.Param("slug")]
	if !ok {
		return p.notFound(c)
	}
	page := c.QueryInt("page", 1)
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * r.PerPage

	var total int64
	p.panel.db.WithContext(c.Ctx()).Model(r.newPtr()).Count(&total)

	slice := r.newSlicePtr()
	p.panel.db.WithContext(c.Ctx()).Model(r.newPtr()).
		Order("id DESC").Limit(r.PerPage).Offset(offset).Find(slice)

	cols := r.indexFields()
	colLabels := make([]string, len(cols))
	for i, f := range cols {
		colLabels[i] = f.Label
	}

	sv := reflect.ValueOf(slice).Elem()
	rows := make([]map[string]any, 0, sv.Len())
	for i := 0; i < sv.Len(); i++ {
		el := sv.Index(i)
		cells := make([]string, len(cols))
		for j, f := range cols {
			cells[j] = fieldStringValue(el, f.Name)
		}
		rows = append(rows, map[string]any{"id": fieldStringValue(el, "ID"), "cells": cells})
	}

	hasNext := int64(offset+r.PerPage) < total
	return c.View("admin/list", p.base(c, map[string]any{
		"title":    r.Label,
		"slug":     r.Slug,
		"singular": r.Singular,
		"columns":  colLabels,
		"rows":     rows,
		"page":     page,
		"prevPage": page - 1,
		"nextPage": page + 1,
		"hasPrev":  page > 1,
		"hasNext":  hasNext,
		"total":    total,
	}))
}

func (p *Plugin) createForm(c *nhttp.Context) error {
	r, ok := p.panel.resources[c.Param("slug")]
	if !ok {
		return p.notFound(c)
	}
	return c.View("admin/form", p.base(c, map[string]any{
		"title":    "New " + r.Singular,
		"slug":     r.Slug,
		"singular": r.Singular,
		"action":   p.panel.cfg.RoutePrefix + "/" + r.Slug,
		"fields":   p.formFieldViews(r, reflect.ValueOf(r.newPtr())),
		"isEdit":   false,
	}))
}

func (p *Plugin) store(c *nhttp.Context) error {
	r, ok := p.panel.resources[c.Param("slug")]
	if !ok {
		return p.notFound(c)
	}
	_ = c.Request.ParseForm()
	model := reflect.ValueOf(r.newPtr())
	for _, f := range r.formFields() {
		if f.Readonly {
			continue
		}
		if err := setField(model, f.Name, c.Request.Form.Get(f.Name)); err != nil {
			return c.JSON(nhttp.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		}
	}
	if err := p.panel.db.WithContext(c.Ctx()).Create(model.Interface()).Error; err != nil {
		return c.JSON(nhttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	c.Redirect(nhttp.StatusFound, p.panel.cfg.RoutePrefix+"/"+r.Slug)
	return nil
}

func (p *Plugin) editForm(c *nhttp.Context) error {
	r, ok := p.panel.resources[c.Param("slug")]
	if !ok {
		return p.notFound(c)
	}
	model := reflect.ValueOf(r.newPtr())
	if err := p.panel.db.WithContext(c.Ctx()).First(model.Interface(), "id = ?", c.Param("id")).Error; err != nil {
		return p.notFound(c)
	}
	return c.View("admin/form", p.base(c, map[string]any{
		"title":    "Edit " + r.Singular,
		"slug":     r.Slug,
		"singular": r.Singular,
		"action":   p.panel.cfg.RoutePrefix + "/" + r.Slug + "/" + c.Param("id"),
		"fields":   p.formFieldViews(r, model),
		"isEdit":   true,
		"id":       c.Param("id"),
	}))
}

func (p *Plugin) update(c *nhttp.Context) error {
	r, ok := p.panel.resources[c.Param("slug")]
	if !ok {
		return p.notFound(c)
	}
	model := reflect.ValueOf(r.newPtr())
	if err := p.panel.db.WithContext(c.Ctx()).First(model.Interface(), "id = ?", c.Param("id")).Error; err != nil {
		return p.notFound(c)
	}
	_ = c.Request.ParseForm()
	for _, f := range r.formFields() {
		if f.Readonly {
			continue
		}
		// Blank password fields leave the stored value untouched.
		raw := c.Request.Form.Get(f.Name)
		if f.Type == TypePassword && raw == "" {
			continue
		}
		if err := setField(model, f.Name, raw); err != nil {
			return c.JSON(nhttp.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		}
	}
	if err := p.panel.db.WithContext(c.Ctx()).Save(model.Interface()).Error; err != nil {
		return c.JSON(nhttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	c.Redirect(nhttp.StatusFound, p.panel.cfg.RoutePrefix+"/"+r.Slug)
	return nil
}

func (p *Plugin) destroy(c *nhttp.Context) error {
	r, ok := p.panel.resources[c.Param("slug")]
	if !ok {
		return p.notFound(c)
	}
	if err := p.panel.db.WithContext(c.Ctx()).Delete(r.newPtr(), "id = ?", c.Param("id")).Error; err != nil {
		return c.JSON(nhttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	c.Redirect(nhttp.StatusFound, p.panel.cfg.RoutePrefix+"/"+r.Slug)
	return nil
}

// ── view helpers ───────────────────────────────────────────────────

// base merges per-page data with the sidebar/brand chrome and a CSRF field
// slot (populated by Shield when enabled).
func (p *Plugin) base(c *nhttp.Context, data map[string]any) map[string]any {
	data["brand"] = p.panel.cfg.BrandName
	data["prefix"] = p.panel.cfg.RoutePrefix
	data["nav"] = p.panel.nav()
	if _, ok := data["csrfField"]; !ok {
		data["csrfField"] = ""
	}
	return data
}

// formFieldViews builds the label + pre-rendered input HTML for each form field.
func (p *Plugin) formFieldViews(r *Resource, model reflect.Value) []map[string]any {
	fields := r.formFields()
	out := make([]map[string]any, 0, len(fields))
	for _, f := range fields {
		val := fieldStringValue(model, f.Name)
		out = append(out, map[string]any{
			"label": f.Label,
			"name":  f.Name,
			"html":  renderInput(f, val),
		})
	}
	return out
}

func (p *Plugin) notFound(c *nhttp.Context) error {
	return c.Status(nhttp.StatusNotFound).View("admin/list", p.base(c, map[string]any{
		"title":   "Not found",
		"slug":    "",
		"columns": []string{},
		"rows":    []map[string]any{},
		"total":   int64(0),
	}))
}
