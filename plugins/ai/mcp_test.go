package ai

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func testMCPServer() *mcpserver.MCPServer {
	s := mcpserver.NewMCPServer("weather-svc", "1.0.0", mcpserver.WithToolCapabilities(false))
	s.AddTool(mcp.NewTool("get_weather",
		mcp.WithDescription("Current weather for a city"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("city", mcp.Required(), mcp.Description("City name")),
	), func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		city, err := req.RequireString("city")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText("Sunny, 21°C in " + city), nil
	})
	s.AddTool(mcp.NewTool("delete.file",
		mcp.WithDescription("Delete a file"),
		mcp.WithString("path", mcp.Required()),
		mcp.WithDestructiveHintAnnotation(true),
	), func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultError("permission denied"), nil
	})
	s.AddTool(mcp.NewTool("stats",
		mcp.WithDescription("Structured stats"),
	), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultStructured(map[string]int{"users": 7}, "7 users"), nil
	})
	return s
}

func TestMCPClientAdaptsServerTools(t *testing.T) {
	ctx := context.Background()
	c, err := ConnectMCP(ctx, MCPInProcess(testMCPServer()), MCPPrefix("wx"), MCPApproveDestructive())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.ServerName() != "weather-svc" {
		t.Fatalf("server name = %q", c.ServerName())
	}

	byName := map[string]*Tool{}
	for _, tool := range c.Tools() {
		byName[tool.Name] = tool
	}
	weather, ok := byName["wx_get_weather"]
	if !ok {
		t.Fatalf("tools = %v", byName)
	}
	var schema map[string]any
	if err := json.Unmarshal(weather.Schema(), &schema); err != nil {
		t.Fatal(err)
	}
	if props, _ := schema["properties"].(map[string]any); props["city"] == nil {
		t.Fatalf("schema lost the city parameter: %s", weather.Schema())
	}

	out, err := weather.Execute(ctx, json.RawMessage(`{"city":"Lisbon"}`))
	if err != nil || !strings.Contains(string(out), "Lisbon") {
		t.Fatalf("Execute = %s, %v", out, err)
	}

	// Unsafe characters are replaced; with MCPApproveDestructive, tools not
	// marked read-only need approval and read-only ones do not.
	del, ok := byName["wx_delete_file"]
	if !ok || !del.NeedsApproval || weather.NeedsApproval {
		t.Fatalf("delete.file=%+v get_weather approval=%v", del, weather.NeedsApproval)
	}
	if _, err := del.Execute(ctx, json.RawMessage(`{"path":"/x"}`)); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("an MCP error result should become an error, got %v", err)
	}

	// Structured content is passed through as JSON.
	out, err = byName["wx_stats"].Execute(ctx, nil)
	if err != nil || string(out) != `{"users":7}` {
		t.Fatalf("stats = %s, %v", out, err)
	}
}

func TestMCPAllowTools(t *testing.T) {
	c, err := ConnectMCP(context.Background(), MCPInProcess(testMCPServer()), MCPAllowTools("get_weather"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if tools := c.Tools(); len(tools) != 1 || tools[0].Name != "get_weather" {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestAgentUsesMCPTools(t *testing.T) {
	ctx := context.Background()
	c, err := ConnectMCP(ctx, MCPInProcess(testMCPServer()), MCPAllowTools("get_weather"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	fake := NewFake(FakeToolCall("get_weather", `{"city":"Pune"}`), FakeText("It is sunny in Pune."))
	resp, err := NewAgent("weather bot").WithClient(fake.Client()).WithMCP(c).Prompt(ctx, "Weather in Pune?")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "It is sunny in Pune." {
		t.Fatalf("reply = %q", resp.Text)
	}
	msgs := fake.Requests()[1].Messages
	if got := msgs[len(msgs)-1].Content; !strings.Contains(got, "Sunny, 21°C in Pune") {
		t.Fatalf("tool result fed back = %q", got)
	}
	if len(fake.Requests()[0].Tools) != 1 {
		t.Fatalf("tools sent = %+v", fake.Requests()[0].Tools)
	}
}

func TestMCPOverStreamableHTTP(t *testing.T) {
	srv := httptest.NewServer(mcpserver.NewStreamableHTTPServer(testMCPServer()))
	defer srv.Close()
	ctx := context.Background()
	c, err := ConnectMCP(ctx, MCPHTTP(srv.URL+"/mcp", map[string]string{"Authorization": "Bearer t"}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var weather *Tool
	for _, tool := range c.Tools() {
		if tool.Name == "get_weather" {
			weather = tool
		}
	}
	if weather == nil {
		t.Fatalf("tools = %+v", c.Tools())
	}
	out, err := weather.Execute(ctx, json.RawMessage(`{"city":"Oslo"}`))
	if err != nil || !strings.Contains(string(out), "Oslo") {
		t.Fatalf("Execute over HTTP = %s, %v", out, err)
	}
}

func TestToolNameSanitising(t *testing.T) {
	if got := toolName("my srv", "a.b/c"); got != "my_srv_a_b_c" {
		t.Fatalf("toolName = %q", got)
	}
	if got := toolName("", strings.Repeat("x", 80)); len(got) != 64 {
		t.Fatalf("long name not truncated: %d", len(got))
	}
}

func TestMCPApprovalIsOptIn(t *testing.T) {
	c, err := ConnectMCP(context.Background(), MCPInProcess(testMCPServer()))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, tool := range c.Tools() {
		if tool.NeedsApproval {
			t.Fatalf("%s needs approval without asking for it", tool.Name)
		}
	}
	c2, _ := ConnectMCP(context.Background(), MCPInProcess(testMCPServer()), MCPRequireApproval("stats"))
	defer c2.Close()
	for _, tool := range c2.Tools() {
		if (tool.Name == "stats") != tool.NeedsApproval {
			t.Fatalf("%s approval = %v", tool.Name, tool.NeedsApproval)
		}
	}
}
