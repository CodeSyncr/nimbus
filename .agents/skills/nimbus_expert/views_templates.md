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

### Nested Components
Components placed in subdirectories under `components/` can be accessed using dot notation:
```html
{{-- components/field/root.nimbus --}}
<div>{{{ .slots.main }}}</div>

{{-- usage --}}
@field.root() Content @end
```

### Props API
The `$props` helper provides an AdonisJS Edge-style API to manipulate and output HTML attributes:
- **`$props.merge(dict)`**: Merges incoming attributes with defaults.
- **`$props.mergeIf(cond, dict)`**: Merges attributes conditionally.
- **`$props.mergeUnless(cond, dict)`**: Merges attributes unless condition is met.
- **`$props.except(keys...)`**: Returns all attributes except the specified ones.
- **`$props.only(keys...)`**: Returns only the specified attributes.
- **`$props.has(key)`**: Checks if an attribute exists.
- **`$props.get(key)`**: Retrieves the value of an attribute.
- **`$props.toAttrs()`**: Converts properties to a safe HTML attributes string (bypasses auto-escaping).

Example:
```html
{{-- components/button.nimbus --}}
<button {{{ $props.merge({ class: "btn-primary" }).except("title").toAttrs() }}}>
  {{{ .slots.main }}}
</button>
```

### Provide / Inject Context API
The `$context` helper enables parent components to share state down the render tree to deeply nested child components.
- **`$context.provide(key, value)`**: Shares state with children.
- **`$context.inject(key)`**: Accesses state shared by parents.

Example:
```html
{{-- components/form.nimbus (Parent) --}}
{{ $context.provide("theme", "dark") }}
<form>
  {{{ .slots.main }}}
</form>

{{-- components/button.nimbus (Deeply nested Child) --}}
{{ const t = $context.inject("theme") }}
<button class="btn btn-{{ $t }}">Submit</button>
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

## Nimbus Cloud dashboard shell (starter)

The starter's authenticated dashboard uses `resources/views/layout-dash.nimbus` with `resources/css/cloud-dashboard.css` (copied to `public/css/` by `nimbus build`) for the existing warm morning surfaces, ember accents, Instrument Serif titles and Geist body typography, shared component tokens, and responsive navigation. The overview at `cloud/dash/overview.nimbus` renders existing controller data: `User.AIUsageCount`, `AIThisPeriod`, `AIHistory` (`Period`, `Count`), `SiteCount`, `TunnelCount`, `Billing`, and `Payments`. Do not invent uptime or trend values. Empty usage/payment data have explicit empty states. `Billing.PeriodPct` is the percentage **remaining**, not elapsed.

The native dialog opened by the header search or Cmd/Ctrl+K derives links from `#dashNav`, retaining role and Cashier workspace visibility. Sidebar groups expand independently; the navigation scrolls on short windows and uses a Menu disclosure on mobile. Keep skip-to-content and keyboard focus styling intact when extending the shell. The background canvas loop was removed from this workspace.
