package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/cli/auth"
	"github.com/CodeSyncr/nimbus/internal/version"
)

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"` // "user" | "assistant" | "system"
	Content any    `json:"content"` // string or []ContentBlock
}

// ContentBlock represents a text or tool block.
type ContentBlock struct {
	Type      string         `json:"type"` // "text" | "tool_use" | "tool_result"
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
}

// StreamHandler receives streaming delta chunks.
type StreamHandler func(delta string)

// AIClient is the interface for communicating with Nimbus Cloud AI backend.
type AIClient interface {
	Chat(ctx context.Context, prompt, model string, projCtx *ProjectContext) (string, error)
	GeneratePlan(ctx context.Context, prompt string, projCtx *ProjectContext, model string) (*PlanSummary, error)
	RegenerateStep(ctx context.Context, stepIndex int, newDesc string, currentPlan *PlanSummary, projCtx *ProjectContext, model string) (*PlanSummary, error)
	StreamExecute(ctx context.Context, prompt string, plan *PlanSummary, messages []Message, tools []ToolDefinition, projCtx *ProjectContext, onDelta StreamHandler) (*MessageResponse, error)
}

// MessageResponse holds the response from the Nimbus Cloud AI.
type MessageResponse struct {
	ID         string         `json:"id"`
	Model      string         `json:"model"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
}

func (m *MessageResponse) TextContent() string {
	var sb strings.Builder
	for _, c := range m.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String()
}

func (m *MessageResponse) ToolUseBlocks() []ContentBlock {
	var list []ContentBlock
	for _, c := range m.Content {
		if c.Type == "tool_use" {
			list = append(list, c)
		}
	}
	return list
}

// NimbusCloudClient connects the CLI to the intelligence engine hosted at nimbusgo.space.
type NimbusCloudClient struct {
	ServerURL  string
	Token      string
	HTTPClient *http.Client
}

// NewNimbusCloudClient initializes client using local authentication credentials.
func NewNimbusCloudClient(serverURL string) (*NimbusCloudClient, error) {
	if serverURL == "" {
		serverURL = auth.GetServerURL()
	}
	creds, err := auth.LoadCredentials()
	token := ""
	if err == nil && creds != nil && !creds.IsExpired() {
		token = creds.AccessToken
	}

	return &NimbusCloudClient{
		ServerURL:  strings.TrimRight(serverURL, "/"),
		Token:      token,
		HTTPClient: &http.Client{Timeout: 600 * time.Second},
	}, nil
}

// ResolveClient returns the appropriate AIClient. All intelligence is routed through nimbusgo.space.
func ResolveClient(serverURL, model string) (AIClient, error) {
	return NewNimbusCloudClient(serverURL)
}

// Chat sends a conversational query or question to Nimbus Cloud AI with project context.
func (c *NimbusCloudClient) Chat(ctx context.Context, prompt, model string, projCtx *ProjectContext) (string, error) {
	apiURL := c.ServerURL + "/api/v1/ai/generate"

	payload := map[string]any{
		"prompt":            prompt,
		"model":             model,
		"framework_version": version.Nimbus,
		"project_context":   projCtx,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(jsonBytes))
	if err != nil {
		return "", err
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("connection error to Nimbus Cloud (%s): %w", c.ServerURL, err)
	}
	defer resp.Body.Close()

	if err := c.checkAuthStatus(resp.StatusCode); err != nil {
		return "", err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", errors.New(cleanErrorMessage(resp.StatusCode, body))
	}

	var res struct {
		Success bool   `json:"success"`
		Reply   string `json:"reply"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return string(body), nil
	}
	if !res.Success && res.Error != "" {
		return "", errors.New(res.Error)
	}
	return res.Reply, nil
}

// GeneratePlan calls POST /api/v1/ai/plan on Nimbus Cloud.
func (c *NimbusCloudClient) GeneratePlan(ctx context.Context, prompt string, projCtx *ProjectContext, model string) (*PlanSummary, error) {
	apiURL := c.ServerURL + "/api/v1/ai/plan"

	payload := map[string]any{
		"prompt":            prompt,
		"model":             model,
		"framework_version": version.Nimbus,
		"project_context":   projCtx,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection error to Nimbus Cloud (%s): %w", c.ServerURL, err)
	}
	defer resp.Body.Close()

	if err := c.checkAuthStatus(resp.StatusCode); err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(cleanErrorMessage(resp.StatusCode, body))
	}

	var res struct {
		Success bool         `json:"success"`
		Plan    *PlanSummary `json:"plan"`
		Error   string       `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("invalid plan response from %s: %s", c.ServerURL, string(body))
	}
	if !res.Success || res.Plan == nil {
		if res.Error != "" {
			return nil, errors.New(res.Error)
		}
		return nil, errors.New("server failed to generate architectural plan")
	}

	return res.Plan, nil
}

// RegenerateStep calls POST /api/v1/ai/plan/regenerate on Nimbus Cloud.
func (c *NimbusCloudClient) RegenerateStep(ctx context.Context, stepIndex int, newDesc string, currentPlan *PlanSummary, projCtx *ProjectContext, model string) (*PlanSummary, error) {
	if currentPlan == nil {
		return nil, errors.New("no active plan")
	}
	apiURL := c.ServerURL + "/api/v1/ai/plan/regenerate"

	payload := map[string]any{
		"prompt":            newDesc,
		"step_index":        stepIndex,
		"new_description":   newDesc,
		"current_plan":      currentPlan,
		"project_context":   projCtx,
		"model":             model,
		"framework_version": version.Nimbus,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection error to Nimbus Cloud (%s): %w", c.ServerURL, err)
	}
	defer resp.Body.Close()

	if err := c.checkAuthStatus(resp.StatusCode); err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(cleanErrorMessage(resp.StatusCode, body))
	}

	var res struct {
		Success bool         `json:"success"`
		Plan    *PlanSummary `json:"plan"`
		Error   string       `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("invalid response from %s: %s", c.ServerURL, string(body))
	}
	if !res.Success || res.Plan == nil {
		if res.Error != "" {
			return nil, errors.New(res.Error)
		}
		return nil, errors.New("server failed to regenerate step")
	}

	return res.Plan, nil
}

// StreamExecute streams step execution and tool guidance from Nimbus Cloud.
func (c *NimbusCloudClient) StreamExecute(ctx context.Context, prompt string, plan *PlanSummary, messages []Message, tools []ToolDefinition, projCtx *ProjectContext, onDelta StreamHandler) (*MessageResponse, error) {
	apiURL := c.ServerURL + "/api/v1/ai/execute"

	// Compact message history: strip file content from past write_file tool_use blocks
	// to prevent payload bloat across many iterations. The server only needs input.path
	// to track which files have been written — not the full file content.
	compactedMessages := compactMessagesForWire(messages)

	payload := map[string]any{
		"prompt":            prompt,
		"approved_plan":     plan,
		"messages":          compactedMessages,
		"tools":             tools,
		"project_context":   projCtx,
		"framework_version": version.Nimbus,
		"stream":            true,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection error to Nimbus Cloud (%s): %w", c.ServerURL, err)
	}
	defer resp.Body.Close()

	if err := c.checkAuthStatus(resp.StatusCode); err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, errors.New(cleanErrorMessage(resp.StatusCode, body))
	}

	contentType := resp.Header.Get("Content-Type")

	// If streaming SSE
	if strings.Contains(contentType, "text/event-stream") {
		scanner := bufio.NewScanner(resp.Body)
		// Allocate generous 20MB buffer for large code file transfers
		buf := make([]byte, 1024*1024)
		scanner.Buffer(buf, 20*1024*1024)

		response := &MessageResponse{
			Role:    "assistant",
			Content: make([]ContentBlock, 0),
		}

		var currentText strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			// Check for tool_use event
			var toolEvent struct {
				Type  string         `json:"type"`
				ID    string         `json:"id"`
				Name  string         `json:"name"`
				Input map[string]any `json:"input"`
			}
			if err := json.Unmarshal([]byte(data), &toolEvent); err == nil && (toolEvent.Type == "tool_use" || toolEvent.Name != "") {
				if toolEvent.Type == "" {
					toolEvent.Type = "tool_use"
				}
				if toolEvent.Name != "" && toolEvent.Input != nil {
					response.Content = append(response.Content, ContentBlock{
						Type:  "tool_use",
						ID:    toolEvent.ID,
						Name:  toolEvent.Name,
						Input: toolEvent.Input,
					})
					continue
				}
			}

			// Check for text delta
			var event struct {
				Text  string `json:"text"`
				Delta *struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &event); err == nil {
				txt := event.Text
				if txt == "" && event.Delta != nil {
					txt = event.Delta.Text
				}
				if txt != "" {
					currentText.WriteString(txt)
					if onDelta != nil {
						onDelta(txt)
					}
				}
			}
		}

		if scanErr := scanner.Err(); scanErr != nil {
			return nil, fmt.Errorf("error reading stream from Nimbus Cloud: %w", scanErr)
		}

		if currentText.Len() > 0 {
			response.Content = append([]ContentBlock{{Type: "text", Text: currentText.String()}}, response.Content...)
		}

		return response, nil
	}

	// Non-streaming JSON response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var jsonResp struct {
		Success bool           `json:"success"`
		Reply   string         `json:"reply"`
		Content []ContentBlock `json:"content"`
		Error   string         `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &jsonResp); err == nil {
		if !jsonResp.Success && jsonResp.Error != "" {
			return nil, errors.New(jsonResp.Error)
		}
		if len(jsonResp.Content) > 0 {
			return &MessageResponse{
				Role:    "assistant",
				Content: jsonResp.Content,
			}, nil
		}
		if jsonResp.Reply != "" {
			if onDelta != nil {
				onDelta(jsonResp.Reply)
			}
			return &MessageResponse{
				Role: "assistant",
				Content: []ContentBlock{
					{Type: "text", Text: jsonResp.Reply},
				},
			}, nil
		}
	}

	return &MessageResponse{
		Role: "assistant",
		Content: []ContentBlock{
			{Type: "text", Text: string(body)},
		},
	}, nil
}

func (c *NimbusCloudClient) setHeaders(req *http.Request) {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "nimbus-cli/"+version.Nimbus)
}

func (c *NimbusCloudClient) checkAuthStatus(statusCode int) error {
	if statusCode == http.StatusUnauthorized {
		return errors.New("unauthorized: session expired. Run 'nimbus login' to authenticate with nimbusgo.space")
	}
	if statusCode == http.StatusPaymentRequired {
		return fmt.Errorf("active Nimbus AI Copilot subscription required. Upgrade at %s/pricing", c.ServerURL)
	}
	return nil
}

// cleanErrorMessage strips HTML tags and produces a clean, friendly error message.
func cleanErrorMessage(statusCode int, body []byte) string {
	raw := string(body)
	if strings.Contains(raw, "<!DOCTYPE") || strings.Contains(raw, "<html") {
		// Extract title if present
		reTitle := regexp.MustCompile("(?i)<title>(.*?)</title>")
		if m := reTitle.FindStringSubmatch(raw); len(m) == 2 {
			return fmt.Sprintf("server error (%d): %s", statusCode, strings.TrimSpace(m[1]))
		}
		return fmt.Sprintf("server returned HTTP %d", statusCode)
	}
	if len(raw) > 200 {
		return raw[:200] + "..."
	}
	return raw
}

// compactMessagesForWire strips large file content from write_file tool_use blocks
// compactMessagesForWire strips large file content and raw skill dumps from message history
// before sending to the server. The server only needs input metadata to track state.
// This prevents exponential payload bloat across multi-turn tool loops.
func compactMessagesForWire(messages []Message) []Message {
	compacted := make([]Message, len(messages))
	for i, msg := range messages {
		compacted[i] = msg
		switch content := msg.Content.(type) {
		case []ContentBlock:
			stripped := make([]ContentBlock, len(content))
			for j, cb := range content {
				stripped[j] = cb
				name := strings.ToLower(cb.Name)
				if cb.Type == "tool_use" && (name == "write_file" || name == "create_file" || name == "create" || name == "write") {
					// Keep only path, drop content to avoid payload bloat
					newInput := map[string]any{}
					if p, ok := cb.Input["path"].(string); ok {
						newInput["path"] = p
					}
					stripped[j] = ContentBlock{
						Type:  cb.Type,
						ID:    cb.ID,
						Name:  cb.Name,
						Input: newInput,
					}
				} else if cb.Type == "tool_use" && (name == "load_skill" || name == "read_skill" || name == "query_skill") {
					newInput := map[string]any{}
					if s, ok := cb.Input["skill_name"].(string); ok {
						newInput["skill_name"] = s
					} else if s, ok := cb.Input["name"].(string); ok {
						newInput["skill_name"] = s
					}
					if q, ok := cb.Input["query"].(string); ok {
						newInput["query"] = q
					}
					stripped[j] = ContentBlock{
						Type:  cb.Type,
						ID:    cb.ID,
						Name:  cb.Name,
						Input: newInput,
					}
				} else if cb.Type == "tool_result" {
					// Trim verbose tool result content (skills & file writes)
					result := cb.Content
					if strings.Contains(result, "# Skill:") || strings.Contains(result, "# Skill Query:") {
						result = "[Skill content mounted into active system frame]"
					} else if len(result) > 120 {
						result = result[:120] + "... (truncated)"
					}
					stripped[j] = ContentBlock{
						Type:      cb.Type,
						ToolUseID: cb.ToolUseID,
						Content:   result,
						IsError:   cb.IsError,
					}
				}
			}
			compacted[i] = Message{Role: msg.Role, Content: stripped}
		}
	}
	return compacted
}

