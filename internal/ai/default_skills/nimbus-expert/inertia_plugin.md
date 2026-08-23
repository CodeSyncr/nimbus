# Inertia Plugin

Inertia.js adapter for building modern SPAs with Vue, React, or Svelte while using Nimbus as the backend.

## Installation
```bash
nimbus plugin install inertia
```

## Configuration
```go
app.Use(inertia.New(inertia.Config{
    RootView:  "app",              // Root template name
    Version:   "1.0",              // Asset version for cache busting
    SSRUrl:    "http://localhost:13714", // Optional: SSR server
}))
```

## Controller Responses
```go
func ShowDashboard(c *http.Context) error {
    return inertia.Render(c, "Dashboard", map[string]any{
        "stats":   getStats(),
        "user":    auth.UserFromContext(c.Ctx()),
    })
}
```

## Shared Data
```go
// Available to all Inertia responses
inertia.Share("auth", func(c *http.Context) any {
    return map[string]any{
        "user": auth.UserFromContext(c.Ctx()),
    }
})
```

## Lazy Props
```go
inertia.Render(c, "Users/Index", map[string]any{
    "users": users,                                    // Always included
    "stats": inertia.Lazy(func() any { return getStats() }), // Only on explicit request
})
```

## Frontend Setup (Vite + Vue example)
```javascript
import { createApp, h } from 'vue'
import { createInertiaApp } from '@inertiajs/vue3'

createInertiaApp({
    resolve: name => import(`./Pages/${name}.vue`),
    setup({ el, App, props, plugin }) {
        createApp({ render: () => h(App, props) })
            .use(plugin)
            .mount(el)
    },
})
```

## Plugin Capabilities
Implements `HasMiddleware`, `HasConfig`, `HasViews`, `HasBindings`.
