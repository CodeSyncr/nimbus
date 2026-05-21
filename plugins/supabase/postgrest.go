package supabase

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// ═══════════════════════════════════════════════════════════════════
// PostgREST — Internal query builder for Supabase REST API
// ═══════════════════════════════════════════════════════════════════
//
// This is an internal implementation detail. Application code should
// use Lucid (GORM) for database queries. The PostgREST layer is used
// internally by the plugin for Supabase-specific operations like RPC
// calls and RLS-scoped queries that bypass direct Postgres.

// queryBuilder provides a fluent interface for PostgREST queries (internal).
type queryBuilder struct {
	client     *Client
	table      string
	method     string
	columns    string
	filters    []string
	orders     []string
	limitVal   int
	offsetVal  int
	single     bool
	count      string
	body       any
	headers    map[string]string
	upsert     bool
	onConflict string
}

// newQueryBuilder starts an internal PostgREST query.
func newQueryBuilder(c *Client, table string) *queryBuilder {
	return &queryBuilder{
		client:  c,
		table:   table,
		method:  "GET",
		columns: "*",
		headers: make(map[string]string),
	}
}

func (q *queryBuilder) selectColumns(columns ...string) *queryBuilder {
	if len(columns) > 0 {
		q.columns = strings.Join(columns, ",")
	}
	q.method = "GET"
	return q
}

func (q *queryBuilder) insert(data any) *queryBuilder {
	q.method = "POST"
	q.body = data
	q.headers["Prefer"] = "return=representation"
	return q
}

func (q *queryBuilder) update(data any) *queryBuilder {
	q.method = "PATCH"
	q.body = data
	q.headers["Prefer"] = "return=representation"
	return q
}

func (q *queryBuilder) deleteRows() *queryBuilder {
	q.method = "DELETE"
	q.headers["Prefer"] = "return=representation"
	return q
}

// ── Filters ─────────────────────────────────────────────────────

func (q *queryBuilder) eq(column, value string) *queryBuilder {
	q.filters = append(q.filters, column+"=eq."+value)
	return q
}

func (q *queryBuilder) neq(column, value string) *queryBuilder {
	q.filters = append(q.filters, column+"=neq."+value)
	return q
}

func (q *queryBuilder) gt(column, value string) *queryBuilder {
	q.filters = append(q.filters, column+"=gt."+value)
	return q
}

func (q *queryBuilder) gte(column, value string) *queryBuilder {
	q.filters = append(q.filters, column+"=gte."+value)
	return q
}

func (q *queryBuilder) lt(column, value string) *queryBuilder {
	q.filters = append(q.filters, column+"=lt."+value)
	return q
}

func (q *queryBuilder) lte(column, value string) *queryBuilder {
	q.filters = append(q.filters, column+"=lte."+value)
	return q
}

func (q *queryBuilder) like(column, pattern string) *queryBuilder {
	q.filters = append(q.filters, column+"=like."+pattern)
	return q
}

func (q *queryBuilder) ilike(column, pattern string) *queryBuilder {
	q.filters = append(q.filters, column+"=ilike."+pattern)
	return q
}

func (q *queryBuilder) in(column string, values []string) *queryBuilder {
	q.filters = append(q.filters, column+"=in.("+strings.Join(values, ",")+")")
	return q
}

func (q *queryBuilder) is(column, value string) *queryBuilder {
	q.filters = append(q.filters, column+"=is."+value)
	return q
}

func (q *queryBuilder) order(column string, ascending bool) *queryBuilder {
	dir := "asc"
	if !ascending {
		dir = "desc"
	}
	q.orders = append(q.orders, column+"."+dir)
	return q
}

func (q *queryBuilder) limit(n int) *queryBuilder {
	q.limitVal = n
	return q
}

func (q *queryBuilder) offset(n int) *queryBuilder {
	q.offsetVal = n
	return q
}

func (q *queryBuilder) withToken(token string) *queryBuilder {
	q.headers["Authorization"] = "Bearer " + token
	return q
}

// execute runs the query and decodes the result into dest.
func (q *queryBuilder) execute(dest any) error {
	u := q.buildURL()

	var body io.Reader
	if q.body != nil {
		b, err := json.Marshal(q.body)
		if err != nil {
			return fmt.Errorf("supabase: postgrest marshal: %w", err)
		}
		body = strings.NewReader(string(b))
		q.headers["Content-Type"] = "application/json"
	}

	if q.count != "" {
		prefer := q.headers["Prefer"]
		if prefer != "" {
			prefer += ",count=" + q.count
		} else {
			prefer = "count=" + q.count
		}
		q.headers["Prefer"] = prefer
	}

	resp, err := q.client.do(q.method, u, body, q.headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase: postgrest %s %s: %d %s",
			q.method, q.table, resp.StatusCode, string(b))
	}

	if dest == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

func (q *queryBuilder) buildURL() string {
	u := q.client.url + "/rest/v1/" + q.table

	params := url.Values{}
	if q.method == "GET" {
		params.Set("select", q.columns)
	}
	for _, f := range q.filters {
		parts := strings.SplitN(f, "=", 2)
		if len(parts) == 2 {
			params.Add(parts[0], parts[1])
		}
	}
	if len(q.orders) > 0 {
		params.Set("order", strings.Join(q.orders, ","))
	}
	if q.limitVal > 0 {
		params.Set("limit", fmt.Sprintf("%d", q.limitVal))
	}
	if q.offsetVal > 0 {
		params.Set("offset", fmt.Sprintf("%d", q.offsetVal))
	}
	if q.onConflict != "" {
		params.Set("on_conflict", q.onConflict)
	}

	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	return u
}

// ── RPC (Remote Procedure Calls) ────────────────────────────────

// Rpc calls a Postgres function via the Supabase PostgREST RPC endpoint.
// This is the only PostgREST method exposed publicly, as RPC calls cannot
// be done through Lucid/GORM.
//
//	var result []map[string]any
//	err := client.Rpc("get_active_users", map[string]any{"min_age": 18}, &result)
func (c *Client) Rpc(fn string, params any, dest any) error {
	u := c.url + "/rest/v1/rpc/" + fn
	resp, err := c.doJSON("POST", u, params)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase: rpc %s: %d %s", fn, resp.StatusCode, string(b))
	}

	if dest == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}
