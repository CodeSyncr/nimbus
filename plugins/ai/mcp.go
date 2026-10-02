/*
|--------------------------------------------------------------------------
| AI SDK — MCP client
|--------------------------------------------------------------------------
|
| Give agents the tools of any Model Context Protocol server: a local
| process over stdio, or a remote server over streamable HTTP or SSE.
|
|   fs, err := ai.ConnectMCP(ctx, ai.MCPStdio("npx", "-y",
|       "@modelcontextprotocol/server-filesystem", "/srv/docs"))
|   defer fs.Close()
|
|   github, err := ai.ConnectMCP(ctx,
|       ai.MCPHTTP("https://api.githubcopilot.com/mcp/",
|           map[string]string{"Authorization": "Bearer " + token}),
|       ai.MCPPrefix("github"))
|
|   agent := ai.NewAgent("…").WithMCP(fs, github)
|
| Tools are listed once at connect time (call Refresh to reload them) and
| each call goes to the server. MCPRequireApproval(names...) or
| MCPApproveDestructive() make calls wait for approval (see approval.go).
|
*/

package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// MCPTransport says how to reach an MCP server (see MCPStdio, MCPHTTP,
// MCPSSE and MCPInProcess).
type MCPTransport struct {
	name    string
	connect func(ctx context.Context) (*mcpclient.Client, error)
	// started is true for transports whose constructor already started them.
	started bool
}

// MCPStdio runs an MCP server as a child process and talks to it over its
// stdin and stdout.
func MCPStdio(command string, args ...string) MCPTransport {
	return MCPStdioEnv(command, nil, args...)
}

// MCPStdioEnv is MCPStdio with extra environment variables ("KEY=value")
// for the child process, e.g. API keys the server needs.
func MCPStdioEnv(command string, env []string, args ...string) MCPTransport {
	return MCPTransport{
		name:    command,
		started: true,
		connect: func(context.Context) (*mcpclient.Client, error) {
			return mcpclient.NewStdioMCPClient(command, env, args...)
		},
	}
}

// MCPHTTP connects to a remote MCP server over streamable HTTP. headers are
// sent with every request (authentication, usually).
func MCPHTTP(url string, headers map[string]string) MCPTransport {
	return MCPTransport{
		name: url,
		connect: func(context.Context) (*mcpclient.Client, error) {
			var opts []transport.StreamableHTTPCOption
			if len(headers) > 0 {
				opts = append(opts, transport.WithHTTPHeaders(headers))
			}
			return mcpclient.NewStreamableHttpClient(url, opts...)
		},
	}
}

// MCPSSE connects to a remote MCP server over the older SSE transport.
func MCPSSE(url string, headers map[string]string) MCPTransport {
	return MCPTransport{
		name: url,
		connect: func(context.Context) (*mcpclient.Client, error) {
			var opts []transport.ClientOption
			if len(headers) > 0 {
				opts = append(opts, transport.WithHeaders(headers))
			}
			return mcpclient.NewSSEMCPClient(url, opts...)
		},
	}
}

// MCPInProcess talks to an mcp-go server in the same process (for tests,
// or to give an agent the tools of the app's own MCP server).
func MCPInProcess(s *mcpserver.MCPServer) MCPTransport {
	return MCPTransport{
		name: "in-process",
		connect: func(context.Context) (*mcpclient.Client, error) {
			return mcpclient.NewInProcessClient(s)
		},
	}
}

// MCPOption configures ConnectMCP.
type MCPOption func(*mcpConfig)

type mcpConfig struct {
	prefix          string
	allow           map[string]bool
	clientName      string
	needApproval    map[string]bool
	approveDestruct bool
}

// MCPPrefix prefixes the server's tool names ("github" → "github_search"),
// so tools of several servers cannot collide.
func MCPPrefix(prefix string) MCPOption {
	return func(c *mcpConfig) { c.prefix = prefix }
}

// MCPAllowTools keeps only the named tools (names as the server gives
// them, before any prefix). Give an agent only what it needs.
func MCPAllowTools(names ...string) MCPOption {
	return func(c *mcpConfig) {
		if c.allow == nil {
			c.allow = map[string]bool{}
		}
		for _, n := range names {
			c.allow[n] = true
		}
	}
}

// MCPRequireApproval makes calls to the named tools (server names) wait
// for approval (see Agent.OnApproval and Agent.Resume).
func MCPRequireApproval(names ...string) MCPOption {
	return func(c *mcpConfig) {
		if c.needApproval == nil {
			c.needApproval = map[string]bool{}
		}
		for _, n := range names {
			c.needApproval[n] = true
		}
	}
}

// MCPApproveDestructive makes calls wait for approval for every tool that
// is not marked read-only and is (per the MCP spec's defaults, possibly)
// destructive. Servers that annotate their tools carefully make this
// precise; on others it covers every tool not marked read-only.
func MCPApproveDestructive() MCPOption {
	return func(c *mcpConfig) { c.approveDestruct = true }
}

// MCPClientName is the client name sent to the server (default "nimbus").
func MCPClientName(name string) MCPOption {
	return func(c *mcpConfig) { c.clientName = name }
}

// MCPClient is a connection to one MCP server.
type MCPClient struct {
	cfg    mcpConfig
	client *mcpclient.Client
	info   mcp.Implementation

	mu    sync.RWMutex
	tools []*Tool
}

// ConnectMCP connects to an MCP server, completes the protocol handshake
// and lists its tools.
func ConnectMCP(ctx context.Context, t MCPTransport, opts ...MCPOption) (*MCPClient, error) {
	if t.connect == nil {
		return nil, errors.New("ai: MCP transport is not set")
	}
	cfg := mcpConfig{clientName: "nimbus"}
	for _, o := range opts {
		o(&cfg)
	}
	c, err := t.connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("ai: connect to MCP server %s: %w", t.name, err)
	}
	if !t.started {
		if err := c.Start(ctx); err != nil {
			_ = c.Close()
			return nil, fmt.Errorf("ai: start MCP transport %s: %w", t.name, err)
		}
	}
	init := mcp.InitializeRequest{}
	init.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	init.Params.ClientInfo = mcp.Implementation{Name: cfg.clientName, Version: "1"}
	res, err := c.Initialize(ctx, init)
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("ai: MCP handshake with %s: %w", t.name, err)
	}
	m := &MCPClient{cfg: cfg, client: c, info: res.ServerInfo}
	if err := m.Refresh(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return m, nil
}

// ServerName is the name the server reported in the handshake.
func (m *MCPClient) ServerName() string { return m.info.Name }

// Refresh reloads the server's tool list.
func (m *MCPClient) Refresh(ctx context.Context) error {
	res, err := m.client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("ai: list MCP tools of %s: %w", m.info.Name, err)
	}
	var tools []*Tool
	for _, mt := range res.Tools {
		if m.cfg.allow != nil && !m.cfg.allow[mt.Name] {
			continue
		}
		tool, err := m.adapt(mt)
		if err != nil {
			return err
		}
		tools = append(tools, tool)
	}
	m.mu.Lock()
	m.tools = tools
	m.mu.Unlock()
	return nil
}

// Tools returns the server's tools as agent tools.
func (m *MCPClient) Tools() []*Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]*Tool(nil), m.tools...)
}

// Close ends the connection (and stops a stdio server's process).
func (m *MCPClient) Close() error { return m.client.Close() }

var toolNameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// toolName makes an MCP tool name acceptable to every provider:
// [a-zA-Z0-9_-], at most 64 characters.
func toolName(prefix, name string) string {
	n := toolNameUnsafe.ReplaceAllString(name, "_")
	if prefix != "" {
		n = toolNameUnsafe.ReplaceAllString(prefix, "_") + "_" + n
	}
	if len(n) > 64 {
		n = n[:64]
	}
	return n
}

func (m *MCPClient) adapt(mt mcp.Tool) (*Tool, error) {
	schema := mt.RawInputSchema
	if len(schema) == 0 {
		b, err := json.Marshal(mt.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("ai: MCP tool %q schema: %w", mt.Name, err)
		}
		schema = b
	}
	remote := mt.Name
	tool, err := NewRawTool(toolName(m.cfg.prefix, mt.Name), mt.Description, schema,
		func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
			var params map[string]any
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("arguments are not a JSON object: %w", err)
			}
			req := mcp.CallToolRequest{}
			req.Params.Name = remote
			req.Params.Arguments = params
			res, err := m.client.CallTool(ctx, req)
			if err != nil {
				return nil, err
			}
			text := mcpResultText(res)
			if res.IsError {
				return nil, errors.New(text)
			}
			if res.StructuredContent != nil {
				if b, err := json.Marshal(res.StructuredContent); err == nil {
					return b, nil
				}
			}
			return json.Marshal(text)
		})
	if err != nil {
		return nil, err
	}
	readOnly := mt.Annotations.ReadOnlyHint != nil && *mt.Annotations.ReadOnlyHint
	destructive := mt.Annotations.DestructiveHint == nil || *mt.Annotations.DestructiveHint
	if m.cfg.needApproval[mt.Name] || (m.cfg.approveDestruct && !readOnly && destructive) {
		tool.NeedsApproval = true
	}
	return tool, nil
}

// mcpResultText flattens a tool result's content for the model.
func mcpResultText(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		switch v := c.(type) {
		case mcp.TextContent:
			parts = append(parts, v.Text)
		case mcp.ImageContent:
			parts = append(parts, "[image: "+v.MIMEType+"]")
		case mcp.AudioContent:
			parts = append(parts, "[audio: "+v.MIMEType+"]")
		case mcp.EmbeddedResource:
			if t, ok := v.Resource.(mcp.TextResourceContents); ok {
				parts = append(parts, t.Text)
			} else {
				parts = append(parts, "[resource]")
			}
		case mcp.ResourceLink:
			parts = append(parts, "[resource: "+v.URI+"]")
		}
	}
	return strings.Join(parts, "\n")
}

// WithMCP gives the agent the tools of MCP servers.
func (a *Agent) WithMCP(clients ...*MCPClient) *Agent {
	for _, c := range clients {
		if c != nil {
			a.tools = append(a.tools, c.Tools()...)
		}
	}
	return a
}
