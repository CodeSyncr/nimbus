# Nimbus app templates

These files are **copied into every new app** when you run `nimbus new <name>`.

- **views/** – `.nimbus` view templates (layout + welcome page). Edit these to change what new apps get.
- Templates are embedded in the CLI binary, so after editing run `go install ./cmd/nimbus` to pick up changes.
