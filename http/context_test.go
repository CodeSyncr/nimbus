package http

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestContextParamAndStore(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(MethodGet, "/", nil)
	c := New(rec, req, map[string]string{"id": "abc"})
	if c.Param("id") != "abc" {
		t.Fatalf("Param: %q", c.Param("id"))
	}
	c.Set("k", 99)
	v, ok := c.Get("k")
	if !ok || v.(int) != 99 {
		t.Fatalf("Get k: %v %v", v, ok)
	}
	if _, err := c.Require("missing"); err == nil {
		t.Fatal("expected Require error")
	}
}

func TestContextJSON(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(MethodGet, "/", nil)
	c := New(rec, req, nil)
	if err := c.JSON(StatusCreated, map[string]int{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if rec.Code != StatusCreated {
		t.Fatalf("code %d", rec.Code)
	}
	var body map[string]int
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["n"] != 1 {
		t.Fatalf("body %#v", body)
	}
}

func TestContextStringRedirect(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(MethodGet, "/", nil)
	c := New(rec, req, nil)
	c.String(418, "x")
	if rec.Code != 418 {
		t.Fatalf("code %d", rec.Code)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(MethodGet, "/", nil)
	c2 := New(rec2, req2, nil)
	c2.Redirect(StatusFound, "/there")
	if rec2.Code != StatusFound {
		t.Fatalf("redirect code %d", rec2.Code)
	}
}

func TestContextQueryInt(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(MethodGet, "/?page=3&bad=x", nil)
	c := New(rec, req, nil)
	if c.QueryInt("page", 0) != 3 {
		t.Fatalf("page")
	}
	if c.QueryInt("bad", 7) != 7 {
		t.Fatalf("bad should default")
	}
	if c.QueryInt("missing", 2) != 2 {
		t.Fatalf("missing default")
	}
}
