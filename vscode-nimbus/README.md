# Nimbus Syntax – VS Code / Cursor Extension

Adds **syntax highlighting** and **editor support** for `.nimbus` template files (Nimbus framework for Go). Works in **VS Code** and **Cursor** (Cursor uses the same extension system).

## Features

- **Syntax highlighting** for Nimbus directives and expressions:
  - `@layout('name')` – layout directive
  - `@if(condition)` … `@else` … `@endif` – conditionals
  - `@each(list)` … `@endeach` – loops
  - `{{ variable }}` – output expressions
- **HTML highlighting** inside `.nimbus` files (embedded HTML grammar)
- **Language configuration**: bracket matching, auto-closing pairs for `{{ }}`, quotes, and tags

## Installation

### Cursor (and VS Code)

**Option 1 – Load the extension folder**

1. In Cursor: **File → Open Folder** and open the `vscode-nimbus` folder inside the Nimbus repo.
2. Press **F5** (or **Run → Start Debugging**). A new Cursor/VS Code window opens with the extension loaded.
3. Open any `.nimbus` file to see highlighting.

**Option 2 – Copy into extensions directory**

Copy the entire `vscode-nimbus` folder to:

- **Cursor:** `~/.cursor/extensions/nimbus-syntax-0.1.0` (macOS/Linux) or `%USERPROFILE%\.cursor\extensions\nimbus-syntax-0.1.0` (Windows)
- **VS Code:** `~/.vscode/extensions/nimbus-syntax-0.1.0`

Restart Cursor (or VS Code). `.nimbus` files will use Nimbus syntax automatically.

**Option 3 – Install from .vsix**

1. Install vsce and package: `npm install -g @vscode/vsce` then `cd vscode-nimbus && vsce package`
2. In Cursor: **Extensions** (sidebar) → **...** → **Install from VSIX** → select the generated `.vsix` file.

## File association

Files with the `.nimbus` extension are automatically associated with the Nimbus language. You can also select **Nimbus** from the language picker (bottom-right of the editor) for the current file.

## Scope names (for themes)

If you are customizing a theme, these scopes are used:

| Scope | Usage |
|-------|--------|
| `keyword.control.directive.nimbus` | `@layout(...)` |
| `keyword.control.conditional.nimbus` | `@if`, `@else`, `@endif`, `@end` |
| `keyword.control.loop.nimbus` | `@each`, `@endeach` |
| `meta.embedded.expression.nimbus` | `{{ ... }}` block |
| `variable.other.nimbus` | Variable names inside `{{ }}` |

## Links

- [Nimbus framework](https://github.com/CodeSyncr/nimbus)
