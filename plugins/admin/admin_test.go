package admin

import (
	"reflect"
	"strings"
	"testing"

	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/lucid"
	"github.com/CodeSyncr/nimbus/view"
	"gorm.io/driver/sqlite"
)

type Post struct {
	database.Model
	Title     string
	Body      string
	Published bool
	Views     int
}

func newDB(t *testing.T) *lucid.DB {
	t.Helper()
	db, err := lucid.Open(sqlite.Open("file::memory:?cache=shared"), &lucid.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Post{}); err != nil {
		t.Fatal(err)
	}
	// Clean slate (shared in-memory cache persists across tests).
	db.Exec("DELETE FROM posts")
	return db
}

func TestResource_InferFieldsAndNormalize(t *testing.T) {
	r := Resource{Model: &Post{}}
	r.normalize()

	if r.Slug != "posts" || r.Singular != "Post" || r.Label != "Posts" {
		t.Fatalf("normalize defaults: slug=%q singular=%q label=%q", r.Slug, r.Singular, r.Label)
	}
	// ID/CreatedAt/UpdatedAt/DeletedAt must be skipped; 4 business fields remain.
	if len(r.Fields) != 4 {
		names := make([]string, len(r.Fields))
		for i, f := range r.Fields {
			names[i] = f.Name
		}
		t.Fatalf("inferred fields = %v, want 4 business fields", names)
	}
	byName := map[string]Field{}
	for _, f := range r.Fields {
		byName[f.Name] = f
	}
	if byName["Body"].Type != TypeTextarea {
		t.Errorf("Body should infer textarea, got %s", byName["Body"].Type)
	}
	if byName["Published"].Type != TypeBoolean {
		t.Errorf("Published should infer boolean, got %s", byName["Published"].Type)
	}
	if byName["Views"].Type != TypeNumber {
		t.Errorf("Views should infer number, got %s", byName["Views"].Type)
	}
}

func TestHumanize(t *testing.T) {
	cases := map[string]string{
		"Title":       "Title",
		"PublishedAt": "Published At",
		"user_id":     "User id",
	}
	for in, want := range cases {
		if got := humanize(in); got != want {
			t.Errorf("humanize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetFieldConversions(t *testing.T) {
	p := &Post{}
	v := reflect.ValueOf(p)
	if err := setField(v, "Title", "Hello"); err != nil {
		t.Fatal(err)
	}
	if err := setField(v, "Views", "42"); err != nil {
		t.Fatal(err)
	}
	if err := setField(v, "Published", "1"); err != nil {
		t.Fatal(err)
	}
	if p.Title != "Hello" || p.Views != 42 || !p.Published {
		t.Fatalf("setField results: %+v", p)
	}
	// Bad number surfaces an error.
	if err := setField(v, "Views", "notnum"); err == nil {
		t.Fatal("expected parse error on non-numeric Views")
	}
}

func TestRenderInput_Types(t *testing.T) {
	if h := string(renderInput(Boolean("Published"), "Yes")); !strings.Contains(h, "checkbox") || !strings.Contains(h, "checked") {
		t.Errorf("boolean render: %s", h)
	}
	if h := string(renderInput(Password("Secret"), "hunter2")); strings.Contains(h, "hunter2") {
		t.Error("password value must not be echoed back")
	}
	sel := renderInput(Select("Status", Option{"draft", "Draft"}, Option{"live", "Live"}), "live")
	if !strings.Contains(string(sel), `value="live" selected`) {
		t.Errorf("select should mark current option: %s", sel)
	}
}

// TestViewsRender ensures the bundled templates parse and execute with the
// data shapes the handlers pass (catches @each/$-scope/directive mistakes).
func TestViewsRender(t *testing.T) {
	pl := New(nil, Config{}).AddResource(Resource{Model: &Post{}})
	view.RegisterPluginViews("admin", pl.ViewsFS())
	r := pl.Panel().resources["posts"]

	base := func(extra map[string]any) map[string]any {
		extra["brand"] = "Test Admin"
		extra["prefix"] = "/admin"
		extra["nav"] = pl.Panel().nav()
		extra["csrfField"] = ""
		return extra
	}

	cases := []struct {
		name string
		data map[string]any
	}{
		{"admin/dashboard", base(map[string]any{"title": "Dashboard",
			"cards": []map[string]any{{"label": "Posts", "slug": "posts", "count": int64(3)}}})},
		{"admin/list", base(map[string]any{"title": "Posts", "slug": "posts", "singular": "Post",
			"columns": []string{"Title", "Published", "Views"},
			"rows":    []map[string]any{{"id": "1", "cells": []string{"Hi", "Yes", "5"}}},
			"page":    1, "hasPrev": false, "hasNext": true, "prevPage": 0, "nextPage": 2, "total": int64(1)})},
		{"admin/form", base(map[string]any{"title": "New Post", "slug": "posts", "singular": "Post",
			"action": "/admin/posts", "isEdit": false,
			"fields": pl.formFieldViews(r, reflect.ValueOf(r.newPtr()))})},
	}
	for _, tc := range cases {
		out, err := view.Render(tc.name, tc.data)
		if err != nil {
			t.Fatalf("render %s: %v", tc.name, err)
		}
		if !strings.Contains(out, "Test Admin") {
			t.Errorf("render %s missing brand chrome", tc.name)
		}
	}
}

func TestPanel_CRUDRoundTrip(t *testing.T) {
	db := newDB(t)
	p := New(db, Config{}).AddResource(Resource{Model: &Post{}})
	panel := p.Panel()

	r, ok := panel.resources["posts"]
	if !ok {
		t.Fatal("posts resource not registered")
	}

	// Create via reflection (mirrors the store handler).
	model := reflect.ValueOf(r.newPtr())
	_ = setField(model, "Title", "First")
	_ = setField(model, "Views", "5")
	_ = setField(model, "Published", "1")
	if err := db.Create(model.Interface()).Error; err != nil {
		t.Fatal(err)
	}

	var count int64
	db.Model(r.newPtr()).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 post, got %d", count)
	}

	// Read back and confirm displayed values.
	got := r.newPtr()
	if err := db.First(got, "title = ?", "First").Error; err != nil {
		t.Fatal(err)
	}
	if v := fieldStringValue(reflect.ValueOf(got), "Published"); v != "Yes" {
		t.Errorf("Published display = %q, want Yes", v)
	}

	// Index visibility: Body is a textarea → hidden from index; 3 index cols.
	if len(r.indexFields()) != 3 {
		t.Fatalf("index fields = %d, want 3 (Body hidden)", len(r.indexFields()))
	}
}
