# MCP Integration - Nimbus

The `plugins/mcp` package brings Model Context Protocol (MCP) support to Nimbus, allowing AI clients (like Claude, Cursor, and others) to interact with your application via tools, resources, and prompt templates.

## Model Context Protocol (MCP)

MCP is an open standard that allows AI models to safely and efficiently access external data and tools. In Nimbus, MCP is implemented as a plugin that can be easily added to any application.

## Core Components

### MCP Server
An MCP server exposes your application's data and functionality to AI clients.
```go
import nimbusmcp "github.com/CodeSyncr/nimbus/plugins/mcp"

server := nimbusmcp.NewServer("My Server", "1.0.0")
```

### Tools
Allow AI clients to perform actions within your application.
```go
server.AddTool(mcp.NewTool("run_report", ...), handleRunReport)
```

### Resources
Expose data or documentation to AI clients as URI-addressable content.
```go
server.AddResource(mcp.NewResourceTemplate("docs://{file}", ...), handleReadDoc)
```

### Prompts
Provide reusable, templated prompts for common AI-assisted tasks.
```go
server.AddPrompt(mcp.Prompt{Name: "code-review", ...}, handleCodeReview)
```

## Transport

Nimbus uses **Streamable HTTP** transport (via POST for JSON-RPC and GET for SSE). This makes it compatible with all standard MCP clients.

## Usage

1.  Initialize the `mcp.Plugin`.
2.  Create one or more MCP servers.
3.  Register the servers within the plugin: `mcpPlugin.Web("/mcp/v1", myServer)`.
4.  Apply the plugin to the Nimbus app: `app.Use(mcpPlugin)`.

## Best Practices

1.  **Granular Permissions**: Ensure your MCP tool handlers perform proper authentication and authorization checks.
2.  **Resource Caching**: Cache expensive resources to improve AI client responsiveness.
3.  **Descriptive Schemas**: Provide clear and concise descriptions for tools and arguments to help the AI understand how to use them correctly.
4.  **Standard URIs**: Use standard URI schemes (e.g., `app://`, `docs://`) for resources.
