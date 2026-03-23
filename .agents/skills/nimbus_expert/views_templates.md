# Views & Templates - Nimbus

Nimbus features a powerful template engine inspired by AdonisJS Edge, using `.nimbus` files to build dynamic HTML views.

## Syntax Overview

-   **Variables**: `{{ .variable }}` for escaped output, `{{{ .raw }}}` for unescaped HTML.
-   **Conditionals**: `@if`, `@else if`, `@else`, `@endif`.
-   **Loops**: `@each(item in .list) ... @endeach`.
-   **Includes**: `@include('path/to/partial')`.
-   **Comments**: `{{-- Comment --}}`.

## Layouts & Components

### Layouts
Define a base shell for your pages and include content using `{{{ .Content }}}`.
```html
{{-- layout.nimbus --}}
<html>
  <body>{{{ .Content }}}</body>
</html>

{{-- home.nimbus --}}
@layout('layout')
<h1>Home</h1>
```

### Components
Reusable UI blocks with slots for dynamic content.
```html
{{-- components/button.nimbus --}}
<button class="btn">{{{ .slots.main }}}</button>

{{-- usage --}}
@button() Click Me @end
```

## Global Variables

-   **CSRF**: `{{{ .csrfField }}}` is auto-injected into views when using the Shield plugin.
-   **Auth**: The authenticated user is often available as `.user`.

## Controller Integration

Render views from your controllers using `ctx.View(templateName, data)`.

```go
return ctx.View("home", map[string]any{
    "title": "Welcome Home",
})
```

## Best Practices

1.  **Logic-Free Templates**: Keep complex logic in controllers or service classes.
2.  **Atomic Components**: Break down your UI into small, reusable components.
3.  **Layout Inheritance**: Use layouts to maintain consistency across your application.
4.  **Automatic Escaping**: Always use `{{ .var }}` unless you are certain the content is safe HTML.
