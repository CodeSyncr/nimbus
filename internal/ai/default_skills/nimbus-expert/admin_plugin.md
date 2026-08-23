# Admin Panel Plugin (CRUD) - Nimbus

`plugins/admin` is a resource-based CRUD admin panel modeled on Laravel Nova / Filament. You describe models as **Resources** (fields + display rules) and the panel auto-generates list, create, edit, delete screens with a Tailwind UI. It is **reflection-driven**, operates on your existing tables, and declares **no migrations of its own**.

> Distinct from `studio/` (a raw DB browser). The admin plugin is a curated, per-model CRUD panel.

## Directory Layout

```
plugins/admin/
  admin.go     // Plugin, Config{RoutePrefix, BrandName, Middleware}, New(db, cfg), AddResource, Panel, container "admin.panel", ViewsFS
  resource.go  // Resource, Field, field constructors, reflection (get/set/infer), renderInput
  routes.go    // dashboard + CRUD handlers (index/create/store/edit/update/destroy)
  views/       // layout.nimbus, dashboard.nimbus, list.nimbus, form.nimbus
```

## Setup

```go
import "github.com/CodeSyncr/nimbus/plugins/admin"

panel := admin.New(db, admin.Config{
    BrandName:   "Acme Admin",
    RoutePrefix: "/admin",                                   // default "/admin"
    Middleware:  []router.Middleware{auth.RequireAuth(guard)}, // GATE IT — else public
})
panel.AddResource(admin.Resource{
    Model:  &models.Post{},
    Fields: []admin.Field{
        admin.Text("Title").AsSortable(),
        admin.Textarea("Body"),
        admin.Boolean("Published"),
    },
})
app.Use(panel)
```

Routes: `/admin` (dashboard with counts), `GET /admin/:slug` (paginated list), `/admin/:slug/create`, `POST /admin/:slug` (store), `GET /admin/:slug/:id/edit`, `POST /admin/:slug/:id` (update), `POST /admin/:slug/:id/delete`. Slug/singular/plural default from the model type name; `PerPage` defaults to 15.

## Fields

Constructors: `Text`, `Textarea`, `Number`, `Boolean`, `Email`, `Password` (write-only, blank on edit = keep existing), `Date`, `Select(name, Option{Value,Label}...)`.

Chainable modifiers: `.WithLabel(s)`, `.AsSortable()`, `.AsReadonly()`, `.HideFromIndex()`, `.HideFromForm()`.

**Zero-config:** with no `Fields`, `inferFields` derives them from the struct — bool→checkbox, name contains email/password→that input, body/description/content→textarea, int/float kinds→number, `time.Time`→date. Embedded `database.Model` fields (ID/CreatedAt/UpdatedAt/DeletedAt) are skipped.

## How it works (reflection)

- `Resource.normalize()` caches the struct type, fills slug/labels, infers fields.
- List: `db.Model(newPtr()).Order("id DESC").Limit/Offset.Find(newSlicePtr())`, then reflect each element into display cells via `fieldStringValue` (bools → "Yes"/"No", times formatted).
- Store/update: `setField(model, name, formValue)` converts strings to the field's kind (string/bool/int/uint/float); `db.Create` / `db.Save`. Blank password on update is skipped.
- Form inputs are pre-rendered to `template.HTML` by `renderInput` and injected with `{{ raw (index . "html") }}` (avoids nested template directives). CSRF injected via Shield when enabled.

## Views

Namespaced under `admin/` via `view.RegisterPluginViews("admin", ViewsFS())` in `Register`. Child views (`admin/dashboard`, `admin/list`, `admin/form`) use `@layout('admin/layout')`; the layout renders `{{ .embed }}` and builds the sidebar from `.nav`. Inside `@each` loops, access root data with `$.` (e.g. `$.prefix`, `$.csrfField`).

**Gotcha:** the view engine escapes `{{ }}` inside `<code>`/`<pre>` blocks (treats them as literal code) — never put template actions inside those tags in plugin views.

**Tests:** `plugins/admin/admin_test.go` — field inference, humanize, setField conversions, renderInput per type, a CRUD round-trip, and `TestViewsRender` (renders all three templates to catch `$`-scope / directive mistakes).
