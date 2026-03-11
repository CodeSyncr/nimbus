# Inertia.js Plugin for Nimbus

Integrates [Inertia.js](https://inertiajs.com) with Nimbus, enabling you to build single-page apps using **Vue**, **React**, or **Svelte** without building an API.

## Installation

The plugin uses [petaki/inertia-go](https://github.com/petaki/inertia-go) as the server adapter:

```bash
go get github.com/petaki/inertia-go
```

## Setup

### 1. Register the plugin

```go
// bin/server.go
import "github.com/CodeSyncr/nimbus/plugins/inertia"

app.Use(inertia.New(inertia.Config{
    URL:          "http://localhost:3000",
    RootTemplate: "resources/views/app.html", // or leave empty for embedded default
    Version:      "1",
}))
```

### 2. Create the root template

Create `resources/views/app.html` (or use the embedded default):

```html
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>My App</title>
  <link href="/css/app.css" rel="stylesheet">
</head>
<body>
  <div id="app" data-page="{{ marshal .page }}"></div>
  <script src="/js/app.js"></script>
</body>
</html>
```

### 3. Set up the frontend

Install Inertia and your framework (Vue, React, or Svelte):

```bash
npm install @inertiajs/vue3 vue  # or @inertiajs/react, @inertiajs/svelte
```

Create `resources/js/app.js`:

```js
import { createApp } from 'vue'
import { createInertiaApp } from '@inertiajs/vue3'

createInertiaApp({
  resolve: name => {
    const pages = import.meta.glob('./Pages/**/*.vue')
    return pages[`./Pages/${name}.vue`]()
  },
  setup({ el, App, props, plugin }) {
    createApp(App).use(plugin).mount(el)
  },
})
```

### 4. Render Inertia pages in handlers

```go
func (c *HomeController) Index(ctx *http.Context) error {
    users := loadUsers()
    return inertia.Render(ctx, "Home/Index", map[string]any{
        "users": users,
    })
}
```

The component name (`Home/Index`) maps to `Pages/Home/Index.vue` in your frontend.

## Configuration

| Option        | Description                                      | Default                    |
|---------------|--------------------------------------------------|----------------------------|
| URL           | Application URL for redirects                    | `http://localhost:3000`   |
| RootTemplate  | Path to root HTML template                       | Embedded default           |
| TemplateFS    | Optional `embed.FS` for root template             | -                          |
| Version       | Asset version for cache busting                  | `1`                        |
| SSRURL        | Node SSR server URL (optional)                   | -                          |

## Server-Side Rendering (SSR)

For SSR with Vue/React, run a Node SSR server and set `SSRURL`:

```go
app.Use(inertia.New(inertia.Config{
    URL:     "http://localhost:3000",
    Version: "1",
    SSRURL:  "http://127.0.0.1:13714",
}))
```

## Notes

- **Inertia vs Unpoly**: Use Inertia when you want a Vue/React/Svelte SPA with server-driven routing. Use Unpoly for server-rendered HTML with progressive enhancement.
- The plugin wraps the HTTP server handler, so it runs before the router for every request.
- Component names use `Path/To/Component` format, matching your frontend page structure.
