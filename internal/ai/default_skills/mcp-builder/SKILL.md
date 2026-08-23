---
name: mcp-builder
description: Model Context Protocol (MCP) server development — JSON-RPC tool declarations, prompt templates, resource providers, and AI agent integration.
---

# MCP Server Builder

Guide for developing Model Context Protocol (MCP) servers and tools in Go.

## Core Concepts

1. **Server Initialization**:
   - Initialize an MCP server instance with tool handlers:
     ```go
     s := mcp.NewServer("nimbus-mcp", "1.0.0")
     ```

2. **Tool Registration**:
   - Define JSON schemas and execute handlers:
     ```go
     s.AddTool(mcp.Tool{
         Name:        "get_weather",
         Description: "Fetch weather data for location",
         InputSchema: mcp.ToolInputSchema{
             Type: "object",
             Properties: map[string]any{
                 "city": map[string]string{"type": "string"},
             },
             Required: []string{"city"},
         },
     }, handleGetWeather)
     ```

3. **Resources & Prompts**:
   - Expose read-only dynamic data sources via `s.AddResource()`.
   - Expose parameterized prompt templates via `s.AddPrompt()`.
