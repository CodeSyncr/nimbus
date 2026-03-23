package mcp

import (
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server represents an MCP server with tools, resources, and prompts.
// Create with NewServer, add capabilities, then pass to Web().
type Server struct {
	name         string
	version      string
	instructions string
	mcpServer    *server.MCPServer
	httpHandler  http.Handler
}

// NewServer creates a new MCP server with the given name and version.
func NewServer(name, version string, opts ...ServerOption) *Server {
	s := &Server{
		name:         name,
		version:      version,
		instructions: "",
	}
	for _, opt := range opts {
		opt(s)
	}
	serverOpts := []server.ServerOption{
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
	}
	if s.instructions != "" {
		serverOpts = append(serverOpts, server.WithInstructions(s.instructions))
	}
	s.mcpServer = server.NewMCPServer(name, version, serverOpts...)
	s.httpHandler = server.NewStreamableHTTPServer(s.mcpServer)
	return s
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithInstructions sets the server instructions (help text for AI clients).
func WithInstructions(instructions string) ServerOption {
	return func(s *Server) {
		s.instructions = instructions
	}
}

// AddTool registers a tool with its handler.
func (s *Server) AddTool(tool mcp.Tool, handler server.ToolHandlerFunc) {
	s.mcpServer.AddTool(tool, handler)
}

// AddResource registers a resource template with its handler.
func (s *Server) AddResource(template mcp.ResourceTemplate, handler server.ResourceTemplateHandlerFunc) {
	s.mcpServer.AddResourceTemplate(template, handler)
}

// AddStaticResource registers a static resource (no template) with its handler.
func (s *Server) AddStaticResource(resource mcp.Resource, handler server.ResourceHandlerFunc) {
	s.mcpServer.AddResource(resource, handler)
}

// AddPrompt registers a prompt with its handler.
func (s *Server) AddPrompt(prompt mcp.Prompt, handler server.PromptHandlerFunc) {
	s.mcpServer.AddPrompt(prompt, handler)
}

// Handler returns the http.Handler for mounting on a router.
func (s *Server) Handler() http.Handler {
	return s.httpHandler
}
