# MCP Plugin for Nimbus

Model Context Protocol (MCP) support for Nimbus, inspired by [Laravel's MCP](https://laravel.com/docs/12.x/mcp). Enables AI clients (Claude, Cursor, etc.) to interact with your application through **tools**, **resources**, and **prompts**.

## Installation

```bash
go get github.com/CodeSyncr/nimbus/plugins/mcp
```

Add the plugin in your `bin/server.go`:

```go
import (
    "github.com/CodeSyncr/nimbus"
    nimbusmcp "github.com/CodeSyncr/nimbus/plugins/mcp"
)

func main() {
    app := nimbus.New()

    mcpPlugin := nimbusmcp.New()
    mcpPlugin.Web("/mcp/weather", myWeatherServer)
    app.Use(mcpPlugin)

    // ...
}
```

Note: Use the `nimbusmcp` alias to avoid conflict with `github.com/mark3labs/mcp-go/mcp`.

## Creating a Server

Create an MCP server with tools, resources, and prompts:

```go
import (
    "context"
    "fmt"

    nimbusmcp "github.com/CodeSyncr/nimbus/plugins/mcp"
    "github.com/mark3labs/mcp-go/mcp"
)

var myWeatherServer = nimbusmcp.NewServer("Weather Server", "1.0.0",
    mcp.WithInstructions("This server provides weather information and forecasts."),
)

func init() {
    // Add a tool
    myWeatherServer.AddTool(
        mcp.NewTool("get_weather",
            mcp.WithDescription("Get the current weather for a location"),
            mcp.WithString("location", mcp.Required(), mcp.Description("City or location name")),
            mcp.WithString("units", mcp.Enum("celsius", "fahrenheit"), mcp.Default("celsius")),
        ),
        handleGetWeather,
    )

    // Add a resource template
    myWeatherServer.AddResource(
        mcp.NewResourceTemplate("weather://forecast/{location}", "Weather Forecast",
            mcp.WithTemplateDescription("Weather forecast for a location"),
            mcp.WithTemplateMIMEType("application/json"),
        ),
        handleWeatherResource,
    )

    // Add a prompt
    myWeatherServer.AddPrompt(
        mcp.Prompt{
            Name:        "describe-weather",
            Description: "Generate a natural-language weather description",
            Arguments: []mcp.PromptArgument{
                {Name: "location", Description: "The location", Required: true},
                {Name: "tone", Description: "Description tone (formal, casual)", Required: false},
            },
        },
        handleDescribeWeatherPrompt,
    )
}

func handleGetWeather(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    location, _ := req.RequireString("location")
    units := req.GetString("units", "celsius")
    // Fetch weather...
    return mcp.NewToolResultText(fmt.Sprintf("Weather in %s: 72°%s, sunny", location, units)), nil
}

func handleWeatherResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
    uri := req.Params.URI // e.g. "weather://forecast/london"
    // Extract template vars from URI, fetch data, return content...
    return []mcp.ResourceContents{{
        URI:       uri,
        MIMEType:  "application/json",
        Text:      `{"temp":72,"conditions":"sunny"}`,
    }}, nil
}

func handleDescribeWeatherPrompt(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
    location := req.Params.Arguments["location"]
    tone := req.Params.Arguments["tone"]
    if tone == "" {
        tone = "casual"
    }
    return &mcp.GetPromptResult{
        Messages: []mcp.PromptMessage{
            {Role: mcp.RoleUser, Content: mcp.TextContent{Type: "text", Text: fmt.Sprintf("Describe the weather in %s in a %s tone.", location, tone)}},
        },
    }, nil
}
```

## Tools

Tools let AI clients perform actions. Define a tool with `mcp.NewTool` and options like `WithDescription`, `WithString`, `WithNumber`, `WithRequired`, etc.

```go
mcp.NewTool("calculate",
    mcp.WithDescription("Perform basic arithmetic"),
    mcp.WithString("operation", mcp.Required(), mcp.Enum("add", "subtract", "multiply", "divide")),
    mcp.WithNumber("x", mcp.Required()),
    mcp.WithNumber("y", mcp.Required()),
)
```

## Resources

Resources expose data to AI clients. Use `NewResource` for static URIs or `NewResourceTemplate` for dynamic URIs with placeholders:

```go
mcp.NewResourceTemplate("docs://readme/{file}", "Documentation",
    mcp.WithTemplateDescription("Project documentation files"),
    mcp.WithTemplateMIMEType("text/markdown"),
)
```

## Prompts

Prompts provide reusable prompt templates:

```go
mcp.Prompt{
    Name:        "code-review",
    Description: "Review code for best practices",
    Arguments: []mcp.PromptArgument{
        {Name: "language", Description: "Programming language", Required: true},
        {Name: "code", Description: "Code to review", Required: true},
    },
}
```

## Transport

The plugin uses **Streamable HTTP** transport (MCP spec). AI clients connect via HTTP POST (JSON-RPC) and GET (SSE for streaming). Compatible with Cursor, Claude Desktop, and other MCP clients.

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_PREFIX` | Base path prefix (optional) | — |

## References

- [MCP Specification](https://modelcontextprotocol.io/)
- [Laravel MCP](https://laravel.com/docs/12.x/mcp)
- [MCP-Go](https://github.com/mark3labs/mcp-go)
