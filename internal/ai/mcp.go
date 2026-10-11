package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	sdk "github.com/CodeSyncr/nimbus/plugins/ai"
)

/*
MCP servers for the terminal agent.

A user lists the Model Context Protocol servers they want under "mcp_servers"
in a settings file (~/.nimbus/settings.json for every project, or
.nimbus/settings.local.json for one, which is the place for anything with a
key in it):

	"mcp_servers": {
	  "21st": {
	    "url": "https://21st.dev/api/mcp",
	    "headers": { "x-api-key": "${API_KEY_21ST}" }
	  },
	  "files": {
	    "command": "npx",
	    "args": ["-y", "@modelcontextprotocol/server-filesystem", "."]
	  }
	}

The key belongs to the user and stays on their machine: these servers are
called from here, not through Nimbus Cloud. ${NAME} in a header, an argument
or an env value is read from the environment, so a key need not be written
into a file at all.

Servers are connected once, before the first turn, and their tools are then
fixed for the session. Connecting later, as each server came up, would change
the list of tools partway through a conversation, and that list is part of
what a provider's cache keys on: every step after the change would be paid for
in full. A server that does not answer in time is left out and said so.

A tool of a server appears to the model as mcp__<server>__<tool>.

What a server does is not known here, so a call is assessed by what the server
says of its own tool: one it marks read-only runs; any other waits for the
user's yes in the default permission mode, unless the user has marked the
server "trust": true. What comes back is read like a fetched web page: scanned
for text that tries to instruct the agent, and capped in size.
*/

// MCPServerConfig is one server under "mcp_servers".
type MCPServerConfig struct {
	// Command, Args and Env run a server as a local process (stdio).
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	// URL and Headers reach a server over HTTP. Type "sse" selects the older
	// SSE transport; anything else is streamable HTTP.
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Type    string            `json:"type,omitempty"`
	// Tools, when set, keeps only these of the server's tools.
	Tools []string `json:"tools,omitempty"`
	// Trust runs every call to this server without asking.
	Trust bool `json:"trust,omitempty"`
	// Disabled leaves the entry in the file without connecting it.
	Disabled bool `json:"disabled,omitempty"`
}

const (
	// mcpConnectTimeout bounds one server's connection. A local server
	// started with npx may have a package to download first.
	mcpConnectTimeout = 25 * time.Second
	// mcpCallTimeout bounds one tool call.
	mcpCallTimeout = 120 * time.Second
	// mcpMaxResultChars caps what one call hands back to the model.
	mcpMaxResultChars = 48 * 1024
	// mcpMaxDescription keeps a server's own prose about a tool from
	// crowding the request: it is sent with every step.
	mcpMaxDescription = 600
	mcpToolPrefix     = "mcp__"
)

// MCPServerStatus is how one configured server stands, for /mcp.
type MCPServerStatus struct {
	Name  string
	Where string // the command or address, with nothing secret in it
	Tools []string
	Err   error
	Trust bool
	Off   bool
}

type mcpTool struct {
	server string
	remote string
	tool   *sdk.Tool
	asks   bool // not marked read-only by its server
}

// MCPSet is the MCP servers of one session and their tools.
type MCPSet struct {
	mu      sync.Mutex
	clients []*sdk.MCPClient
	tools   map[string]*mcpTool // by the name the model calls
	order   []string
	status  []MCPServerStatus
}

var mcpNameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// mcpToolName is the name the model calls a server's tool by.
func mcpToolName(server, tool string) string {
	n := mcpToolPrefix + mcpNameUnsafe.ReplaceAllString(server, "_") + "__" + mcpNameUnsafe.ReplaceAllString(tool, "_")
	if len(n) > 64 {
		n = n[:64]
	}
	return n
}

// expandEnv replaces ${NAME} with the environment's value.
func expandEnv(s string) string { return os.Expand(s, os.Getenv) }

// mcpTransport is how one configured server is reached, and a description
// of where that is with nothing secret in it.
func mcpTransport(cfg MCPServerConfig) (sdk.MCPTransport, string, error) {
	switch {
	case strings.TrimSpace(cfg.URL) != "":
		headers := map[string]string{}
		for k, v := range cfg.Headers {
			headers[k] = expandEnv(v)
		}
		url := expandEnv(strings.TrimSpace(cfg.URL))
		if strings.EqualFold(cfg.Type, "sse") {
			return sdk.MCPSSE(url, headers), url, nil
		}
		return sdk.MCPHTTP(url, headers), url, nil
	case strings.TrimSpace(cfg.Command) != "":
		args := make([]string, len(cfg.Args))
		for i, a := range cfg.Args {
			args[i] = expandEnv(a)
		}
		var env []string
		for k, v := range cfg.Env {
			env = append(env, k+"="+expandEnv(v))
		}
		sort.Strings(env)
		return sdk.MCPStdioEnv(cfg.Command, env, args...), strings.TrimSpace(cfg.Command + " " + strings.Join(cfg.Args, " ")), nil
	}
	return sdk.MCPTransport{}, "", errors.New(`needs a "url" (a remote server) or a "command" (a local one)`)
}

// connectMCP opens one connection. A test puts a server of its own here.
var connectMCP = func(ctx context.Context, cfg MCPServerConfig) (*sdk.MCPClient, error) {
	t, _, err := mcpTransport(cfg)
	if err != nil {
		return nil, err
	}
	opts := []sdk.MCPOption{sdk.MCPClientName("nimbus-vani"), sdk.MCPApproveDestructive()}
	if len(cfg.Tools) > 0 {
		opts = append(opts, sdk.MCPAllowTools(cfg.Tools...))
	}
	return sdk.ConnectMCP(ctx, t, opts...)
}

// ConnectMCPServers connects every configured server, all at once, and
// returns them with their tools. It does not fail: a server that cannot be
// reached is recorded with why and left out. Nil when none is configured.
func ConnectMCPServers(ctx context.Context, configured map[string]json.RawMessage) *MCPSet {
	if len(configured) == 0 {
		return nil
	}
	names := make([]string, 0, len(configured))
	for n := range configured {
		names = append(names, n)
	}
	sort.Strings(names)

	type result struct {
		status MCPServerStatus
		client *sdk.MCPClient
	}
	results := make([]result, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		var cfg MCPServerConfig
		if err := json.Unmarshal(configured[name], &cfg); err != nil {
			results[i].status = MCPServerStatus{Name: name, Err: fmt.Errorf("its settings are not valid: %w", err)}
			continue
		}
		_, where, terr := mcpTransport(cfg)
		results[i].status = MCPServerStatus{Name: name, Where: where, Trust: cfg.Trust, Off: cfg.Disabled, Err: terr}
		if cfg.Disabled || terr != nil {
			continue
		}
		wg.Add(1)
		go func(i int, cfg MCPServerConfig) {
			defer wg.Done()
			// The deadline covers the handshake only. Some transports bind a
			// local server's process to the context they were started with,
			// so the connection itself is opened on the caller's context and
			// abandoned, not cancelled, when it is too slow.
			type opened struct {
				c   *sdk.MCPClient
				err error
			}
			done := make(chan opened, 1)
			go func() {
				c, err := connectMCP(ctx, cfg)
				done <- opened{c, err}
			}()
			select {
			case o := <-done:
				results[i].client, results[i].status.Err = o.c, o.err
			case <-time.After(mcpConnectTimeout):
				results[i].status.Err = fmt.Errorf("did not answer within %s", mcpConnectTimeout)
				go func() {
					if o := <-done; o.c != nil {
						_ = o.c.Close()
					}
				}()
			case <-ctx.Done():
				results[i].status.Err = ctx.Err()
			}
		}(i, cfg)
	}
	wg.Wait()

	set := &MCPSet{tools: map[string]*mcpTool{}}
	for i := range results {
		r := &results[i]
		if r.client != nil {
			set.clients = append(set.clients, r.client)
			trust := r.status.Trust
			for _, t := range r.client.Tools() {
				name := mcpToolName(r.status.Name, t.Name)
				if _, taken := set.tools[name]; taken {
					continue
				}
				set.tools[name] = &mcpTool{server: r.status.Name, remote: t.Name, tool: t, asks: t.NeedsApproval && !trust}
				set.order = append(set.order, name)
				r.status.Tools = append(r.status.Tools, t.Name)
			}
		}
		set.status = append(set.status, r.status)
	}
	return set
}

// Status is how each configured server stands.
func (s *MCPSet) Status() []MCPServerStatus {
	if s == nil {
		return nil
	}
	return append([]MCPServerStatus(nil), s.status...)
}

// Summary is one line for the screen on start-up, or "" with nothing to say.
func (s *MCPSet) Summary() string {
	if s == nil {
		return ""
	}
	var up, down []string
	for _, st := range s.status {
		switch {
		case st.Off:
		case st.Err != nil:
			down = append(down, st.Name)
		default:
			up = append(up, fmt.Sprintf("%s (%d)", st.Name, len(st.Tools)))
		}
	}
	var parts []string
	if len(up) > 0 {
		parts = append(parts, "MCP: "+strings.Join(up, ", "))
	}
	if len(down) > 0 {
		parts = append(parts, "not connected: "+strings.Join(down, ", ")+" (see /mcp)")
	}
	return strings.Join(parts, " · ")
}

// Has reports whether name is a tool of one of the servers.
func (s *MCPSet) Has(name string) bool {
	if s == nil {
		return false
	}
	_, ok := s.tools[name]
	return ok
}

// Definitions are the servers' tools as the model is offered them, in a
// fixed order.
func (s *MCPSet) Definitions() []ToolDefinition {
	if s == nil {
		return nil
	}
	defs := make([]ToolDefinition, 0, len(s.order))
	for _, name := range s.order {
		t := s.tools[name]
		schema := map[string]any{}
		if len(t.tool.Parameters) > 0 {
			_ = json.Unmarshal(t.tool.Parameters, &schema)
		}
		if schema["type"] == nil {
			schema["type"] = "object"
		}
		desc := strings.Join(strings.Fields(t.tool.Description), " ")
		if len(desc) > mcpMaxDescription {
			desc = desc[:mcpMaxDescription] + "…"
		}
		defs = append(defs, ToolDefinition{
			Name:        name,
			Description: fmt.Sprintf("[%s, an MCP server the user connected] %s", t.server, desc),
			InputSchema: schema,
		})
	}
	return defs
}

// asks reports whether a call to name waits for the user's yes, and the
// server it belongs to.
func (s *MCPSet) asks(name string) (bool, string) {
	if s == nil {
		return false, ""
	}
	t, ok := s.tools[name]
	if !ok {
		return false, ""
	}
	return t.asks, t.server
}

// Call runs one of the servers' tools and returns its result as text, with
// the server it came from.
func (s *MCPSet) Call(ctx context.Context, name string, args map[string]any) (string, string, error) {
	t, ok := s.tools[name]
	if !ok {
		return "", "", fmt.Errorf("unknown tool: %s", name)
	}
	if args == nil {
		args = map[string]any{}
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return "", t.server, err
	}
	cctx, cancel := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel()
	out, err := t.tool.Execute(cctx, raw)
	if err != nil {
		return "", t.server, fmt.Errorf("%s (MCP server %s): %w", t.remote, t.server, err)
	}
	// text comes back as a JSON string, structured results as they are
	var text string
	if json.Unmarshal(out, &text) != nil {
		text = string(out)
	}
	if len(text) > mcpMaxResultChars {
		text = text[:mcpMaxResultChars] + "\n… [truncated]"
	}
	if strings.TrimSpace(text) == "" {
		text = "(no output)"
	}
	return text, t.server, nil
}

// Close ends every connection, stopping the local servers' processes.
func (s *MCPSet) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	clients := s.clients
	s.clients = nil
	s.mu.Unlock()
	for _, c := range clients {
		_ = c.Close()
	}
}
