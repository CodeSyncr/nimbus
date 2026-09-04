package ai

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/CodeSyncr/nimbus/cli/auth"
	"github.com/CodeSyncr/nimbus/internal/version"
)

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"`    // "user" | "assistant" | "system"
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

// TurnMode selects the server-side system prompt for an agent turn.
type TurnMode string

const (
	// TurnModeAgent is the default: one continuous conversation in which the
	// model decides whether to answer, investigate, or change code. The other
	// modes drive the older staged pipeline, still used by --plan-only.
	TurnModeAgent   TurnMode = "agent"
	TurnModeExplore TurnMode = "explore"
	TurnModePlan    TurnMode = "plan"
	TurnModeExecute TurnMode = "execute"
	TurnModeChat    TurnMode = "chat"
)

// TurnRequest is one model turn of the agent loop: the server composes the
// system prompt for Mode from the project context and returns text and/or
// tool calls; the CLI executes tools locally and calls again.
type TurnRequest struct {
	Mode     TurnMode
	Model    string
	Prompt   string // the user's original request
	Messages []Message
	Tools    []ToolDefinition
	Plan     *PlanSummary
	Context  *ProjectContext
}

// ErrTurnUnsupported is returned when the cloud server predates the agent
// turn endpoint; callers fall back to the legacy plan/execute endpoints.
var ErrTurnUnsupported = errors.New("nimbus cloud server does not support agent turns (upgrade the server)")

// AIClient is the interface for communicating with Nimbus Cloud AI backend.
type AIClient interface {
	Chat(ctx context.Context, prompt, model string, projCtx *ProjectContext) (string, error)
	GeneratePlan(ctx context.Context, prompt string, projCtx *ProjectContext, model string) (*PlanSummary, error)
	RegenerateStep(ctx context.Context, stepIndex int, newDesc string, currentPlan *PlanSummary, projCtx *ProjectContext, model string) (*PlanSummary, error)
	StreamExecute(ctx context.Context, prompt string, plan *PlanSummary, messages []Message, tools []ToolDefinition, projCtx *ProjectContext, onDelta StreamHandler) (*MessageResponse, error)
	// Turn runs a single agentic model turn. Implementations that cannot
	// support it must return ErrTurnUnsupported.
	Turn(ctx context.Context, req *TurnRequest, onDelta StreamHandler) (*MessageResponse, error)
}

// MessageResponse holds the response from the Nimbus Cloud AI.
type MessageResponse struct {
	ID         string         `json:"id"`
	Model      string         `json:"model"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      *TokenUsage    `json:"usage,omitempty"`
}

// TokenUsage reports what a request consumed. It is populated when the server
// sends a "usage" field (JSON) or a usage SSE event; older servers omit it and
// the CLI simply reports no usage rather than guessing.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	// CostUSD is what the turn cost, when the server prices it. The CLI does
	// not price locally: the server picks the model behind "optimal", so a
	// client-side guess would put a wrong number against real money.
	CostUSD float64 `json:"cost_usd,omitempty"`
}

// Total returns all tokens billed for the request.
func (u *TokenUsage) Total() int {
	if u == nil {
		return 0
	}
	return u.InputTokens + u.OutputTokens
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

	// OnRetry is called before each retry so the UI can explain the pause
	// instead of appearing to hang. See retry.go.
	OnRetry retryNotifier
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
		HTTPClient: newHTTPClient(),
	}, nil
}

// RetryReporter is implemented by clients that can announce transient
// retries, so the UI can explain a pause instead of looking frozen.
type RetryReporter interface {
	SetRetryHook(func(attempt int, reason string))
}

// SetRetryHook implements RetryReporter.
func (c *NimbusCloudClient) SetRetryHook(fn func(attempt int, reason string)) {
	c.OnRetry = fn
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

	resp, err := c.postJSON(ctx, apiURL, jsonBytes)
	if err != nil {
		return "", err
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

	resp, err := c.postJSON(ctx, apiURL, jsonBytes)
	if err != nil {
		return nil, err
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

	resp, err := c.postJSON(ctx, apiURL, jsonBytes)
	if err != nil {
		return nil, err
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

// Turn calls POST /api/v1/ai/turn: one agentic model turn with native tools.
func (c *NimbusCloudClient) Turn(ctx context.Context, tr *TurnRequest, onDelta StreamHandler) (*MessageResponse, error) {
	apiURL := c.ServerURL + "/api/v1/ai/turn"

	payload := map[string]any{
		"mode":              string(tr.Mode),
		"model":             tr.Model,
		"prompt":            tr.Prompt,
		"messages":          compactMessagesForWire(tr.Messages),
		"tools":             tr.Tools,
		"plan":              tr.Plan,
		"project_context":   tr.Context,
		"framework_version": version.Nimbus,
		"stream":            true,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	resp, err := c.postJSONStream(ctx, apiURL, jsonBytes)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return nil, ErrTurnUnsupported
	}
	if err := c.checkAuthStatus(resp.StatusCode); err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, errors.New(cleanErrorMessage(resp.StatusCode, body))
	}

	return parseMessageResponse(resp, onDelta)
}

// StreamExecute streams step execution and tool guidance from Nimbus Cloud.
func (c *NimbusCloudClient) StreamExecute(ctx context.Context, prompt string, plan *PlanSummary, messages []Message, tools []ToolDefinition, projCtx *ProjectContext, onDelta StreamHandler) (*MessageResponse, error) {
	apiURL := c.ServerURL + "/api/v1/ai/execute"

	// Compact message history so payload size stays bounded across many
	// iterations while recent tool output stays fully visible to the model.
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

	resp, err := c.postJSONStream(ctx, apiURL, jsonBytes)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.checkAuthStatus(resp.StatusCode); err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, errors.New(cleanErrorMessage(resp.StatusCode, body))
	}

	return parseMessageResponse(resp, onDelta)
}

// parseMessageResponse decodes either an SSE stream or a JSON body into a
// MessageResponse. SSE events: {"type":"text","text":…} / {"text":…},
// {"type":"tool_use","id","name","input"}, {"type":"error","error":…}, [DONE].
func parseMessageResponse(resp *http.Response, onDelta StreamHandler) (*MessageResponse, error) {
	contentType := resp.Header.Get("Content-Type")

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

			var event struct {
				Type       string         `json:"type"`
				ID         string         `json:"id"`
				Name       string         `json:"name"`
				Input      map[string]any `json:"input"`
				Text       string         `json:"text"`
				Error      string         `json:"error"`
				StopReason string         `json:"stop_reason"`
				Delta      *struct {
					Text string `json:"text"`
				} `json:"delta"`
				Usage *TokenUsage `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			// Usage may ride along on any event; the server typically sends
			// it with "done". Keep the last non-empty report.
			if event.Usage != nil && event.Usage.Total() > 0 {
				response.Usage = event.Usage
			}

			switch {
			case event.Type == "usage":
				// usage-only event: already captured above
			case event.Type == "error" || (event.Error != "" && event.Type == ""):
				if event.Error == "" {
					event.Error = "unknown server error"
				}
				return nil, errors.New(event.Error)
			case event.Type == "tool_use" || (event.Name != "" && event.Input != nil):
				response.Content = append(response.Content, ContentBlock{
					Type:  "tool_use",
					ID:    event.ID,
					Name:  event.Name,
					Input: event.Input,
				})
			case event.Type == "done":
				response.StopReason = event.StopReason
			default:
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
		Success    bool           `json:"success"`
		Reply      string         `json:"reply"`
		Content    []ContentBlock `json:"content"`
		StopReason string         `json:"stop_reason"`
		Usage      *TokenUsage    `json:"usage,omitempty"`
		Error      string         `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &jsonResp); err == nil {
		if !jsonResp.Success && jsonResp.Error != "" {
			return nil, errors.New(jsonResp.Error)
		}
		if len(jsonResp.Content) > 0 {
			if onDelta != nil {
				for _, b := range jsonResp.Content {
					if b.Type == "text" && b.Text != "" {
						onDelta(b.Text)
					}
				}
			}
			return &MessageResponse{
				Role:       "assistant",
				Usage:      jsonResp.Usage,
				Content:    jsonResp.Content,
				StopReason: jsonResp.StopReason,
			}, nil
		}
		if jsonResp.Reply != "" {
			if onDelta != nil {
				onDelta(jsonResp.Reply)
			}
			return &MessageResponse{
				Role:  "assistant",
				Usage: jsonResp.Usage,
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

// History compaction limits. Recent tool output must reach the model intact
// (a read_file result cut to 100 characters made the agent effectively
// blind); only older output is elided, and the model can re-run a tool.
const (
	keepFullToolResults    = 8
	maxToolResultChars     = 48 * 1024
	elidedToolResultChars  = 240
	keepWriteContentTurns  = 2
	elidedWriteContentNote = "[content omitted from history — the file was written to disk; read_file to see it]"
)

// compactMessagesForWire bounds payload growth across iterations while
// preserving what the model needs: the newest tool results in full, older
// ones as short previews, and write_file bodies only for the last few turns.
func compactMessagesForWire(messages []Message) []Message {
	// Index tool results from newest to oldest, and assistant turns likewise.
	resultRank := map[int]map[int]int{} // msgIdx -> blockIdx -> rank (0 = newest)
	rank := 0
	for i := len(messages) - 1; i >= 0; i-- {
		blocks, ok := messages[i].Content.([]ContentBlock)
		if !ok {
			continue
		}
		for j := len(blocks) - 1; j >= 0; j-- {
			if blocks[j].Type == "tool_result" {
				if resultRank[i] == nil {
					resultRank[i] = map[int]int{}
				}
				resultRank[i][j] = rank
				rank++
			}
		}
	}
	assistantRank := map[int]int{}
	arank := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			assistantRank[i] = arank
			arank++
		}
	}

	compacted := make([]Message, len(messages))
	for i, msg := range messages {
		compacted[i] = msg
		blocks, ok := msg.Content.([]ContentBlock)
		if !ok {
			continue
		}
		stripped := make([]ContentBlock, len(blocks))
		for j, cb := range blocks {
			stripped[j] = cb
			name := strings.ToLower(cb.Name)
			switch {
			case cb.Type == "tool_use" && (name == "write_file" || name == "create_file" || name == "create" || name == "write"):
				if assistantRank[i] < keepWriteContentTurns {
					continue
				}
				newInput := map[string]any{}
				if p, ok := cb.Input["path"].(string); ok {
					newInput["path"] = p
				}
				newInput["content"] = elidedWriteContentNote
				stripped[j] = ContentBlock{Type: cb.Type, ID: cb.ID, Name: cb.Name, Input: newInput}
			case cb.Type == "tool_result":
				result := cb.Content
				isSkill := strings.HasPrefix(result, "# Skill:") || strings.HasPrefix(result, "# Skill Query:")
				if resultRank[i][j] < keepFullToolResults || isSkill {
					// Skill content is never elided: the legacy server reads
					// loaded skills back out of the history on every turn.
					if len(result) > maxToolResultChars {
						result = result[:maxToolResultChars] + "\n… [truncated]"
					}
				} else if len(result) > elidedToolResultChars {
					result = result[:elidedToolResultChars] + "\n… [earlier output elided; run the tool again if you need it]"
				}
				stripped[j] = ContentBlock{Type: cb.Type, ToolUseID: cb.ToolUseID, Content: result, IsError: cb.IsError}
			}
		}
		compacted[i] = Message{Role: msg.Role, Content: stripped}
	}
	return compacted
}

// ---------------------------------------------------------------------------
// Image generation
// ---------------------------------------------------------------------------

// GeneratedImage is one image returned by Nimbus Cloud.
type GeneratedImage struct {
	Data  []byte // decoded bytes, ready to write to disk
	Model string // the model that drew it
}

// GenerateImage asks Nimbus Cloud for an image.
//
// The provider keys live on the server, so the CLI never talks to an image
// provider directly: it posts a prompt and receives bytes. Which model draws
// the picture is server-side configuration, and changing it does not require a
// new CLI.
func (c *NimbusCloudClient) GenerateImage(ctx context.Context, prompt, size, model string) (*GeneratedImage, error) {
	payload := map[string]any{"prompt": prompt}
	if size != "" {
		payload["size"] = size
	}
	if model != "" {
		payload["model"] = model
	}
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	resp, err := c.postJSON(ctx, c.ServerURL+"/api/v1/ai/image", jsonBytes)
	if err != nil {
		return nil, err
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
		Success bool   `json:"success"`
		Model   string `json:"model"`
		Error   string `json:"error,omitempty"`
		Images  []struct {
			B64JSON string `json:"b64_json"`
			URL     string `json:"url"`
		} `json:"images"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("invalid image response from %s: %s", c.ServerURL, string(body))
	}
	if !res.Success || len(res.Images) == 0 {
		if res.Error != "" {
			return nil, errors.New(res.Error)
		}
		return nil, errors.New("the server returned no image")
	}

	raw := res.Images[0].B64JSON
	if raw == "" {
		return nil, errors.New("the server returned an image with no data")
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("could not decode the generated image: %w", err)
	}
	return &GeneratedImage{Data: decoded, Model: res.Model}, nil
}
