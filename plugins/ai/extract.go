/*
|--------------------------------------------------------------------------
| AI SDK — Structured Output (LLM Extractor)
|--------------------------------------------------------------------------
|
| Extract typed Go structs from unstructured text using LLMs. The
| framework generates a JSON schema from the target type, instructs the
| model to respond with JSON, validates the output, and retries on
| failure.
|
| Usage:
|
|   type Invoice struct {
|       Merchant string  `json:"merchant"`
|       Amount   float64 `json:"amount"`
|       Date     string  `json:"date"`
|   }
|
|   invoice, err := ai.Extract[Invoice](ctx, receiptText)
|
|   // With options:
|   invoice, err := ai.Extract[Invoice](ctx, text,
|       ai.WithModel("gpt-4o"),
|       ai.WithMaxRetries(3),
|   )
|
*/

package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
)

// ---------------------------------------------------------------------------
// Extraction options
// ---------------------------------------------------------------------------

// ExtractOption configures the extraction process.
type ExtractOption func(*extractConfig)

type extractConfig struct {
	model      string
	maxRetries int
	system     string
}

// WithExtractModel sets the model for extraction.
func WithExtractModel(model string) ExtractOption {
	return func(c *extractConfig) { c.model = model }
}

// WithMaxRetries sets the number of retry attempts on malformed JSON.
func WithMaxRetries(n int) ExtractOption {
	return func(c *extractConfig) { c.maxRetries = n }
}

// WithExtractSystem overrides the system prompt for extraction.
func WithExtractSystem(system string) ExtractOption {
	return func(c *extractConfig) { c.system = system }
}

// ---------------------------------------------------------------------------
// Extract function (generic)
// ---------------------------------------------------------------------------

// Extract asks the AI model to extract structured data from text and
// return it as a typed Go value. Uses JSON schema to constrain the
// model's output and retries on parse failure.
func Extract[T any](ctx context.Context, text string, opts ...ExtractOption) (*T, error) {
	cfg := &extractConfig{
		maxRetries: 2,
		system:     "You are a data extraction assistant. Extract the requested information from the text and respond with valid JSON only. No explanation, no markdown, just the JSON object.",
	}
	for _, opt := range opts {
		opt(cfg)
	}

	var target T
	schema := structToJSONSchema(reflect.TypeOf(target))

	prompt := fmt.Sprintf(
		"Extract the following structure from this text.\n\nJSON Schema:\n```json\n%s\n```\n\nText:\n%s\n\nRespond with only the JSON object.",
		string(schema), text,
	)

	client := GetClient()

	var lastErr error
	for attempt := 0; attempt <= cfg.maxRetries; attempt++ {
		genOpts := []GenerateOption{
			WithSystem(cfg.system),
			WithSchema(schema),
		}
		if cfg.model != "" {
			genOpts = append(genOpts, WithModel(cfg.model))
		}

		resp, err := client.Generate(ctx, prompt, genOpts...)
		if err != nil {
			lastErr = fmt.Errorf("ai: extract attempt %d: %w", attempt, err)
			continue
		}

		// Try to parse the JSON response.
		cleaned := cleanJSON(resp.Text)
		var result T
		if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
			lastErr = fmt.Errorf("ai: extract attempt %d: invalid JSON: %w\nResponse: %s", attempt, err, resp.Text)
			// Add the failed attempt to the prompt for retry.
			prompt = fmt.Sprintf(
				"%s\n\nYour previous response was invalid JSON: %v\nPlease try again with valid JSON only.",
				prompt, err,
			)
			continue
		}

		return &result, nil
	}

	return nil, fmt.Errorf("ai: extract failed after %d attempts: %w", cfg.maxRetries+1, lastErr)
}

// ---------------------------------------------------------------------------
// ExtractSlice extracts a slice of structs from text.
// ---------------------------------------------------------------------------

// ExtractSlice extracts multiple items of the given type from text.
func ExtractSlice[T any](ctx context.Context, text string, opts ...ExtractOption) ([]T, error) {
	cfg := &extractConfig{
		maxRetries: 2,
		system:     "You are a data extraction assistant. Extract all matching items from the text and respond with a JSON array only. No explanation, no markdown, just the JSON array.",
	}
	for _, opt := range opts {
		opt(cfg)
	}

	var target T
	schema := structToJSONSchema(reflect.TypeOf(target))

	prompt := fmt.Sprintf(
		"Extract all items matching this structure from the text.\n\nItem JSON Schema:\n```json\n%s\n```\n\nText:\n%s\n\nRespond with only a JSON array of objects.",
		string(schema), text,
	)

	client := GetClient()

	var lastErr error
	for attempt := 0; attempt <= cfg.maxRetries; attempt++ {
		genOpts := []GenerateOption{
			WithSystem(cfg.system),
		}
		if cfg.model != "" {
			genOpts = append(genOpts, WithModel(cfg.model))
		}

		resp, err := client.Generate(ctx, prompt, genOpts...)
		if err != nil {
			lastErr = err
			continue
		}

		cleaned := cleanJSON(resp.Text)
		var results []T
		if err := json.Unmarshal([]byte(cleaned), &results); err != nil {
			lastErr = fmt.Errorf("ai: extract-slice attempt %d: invalid JSON: %w", attempt, err)
			continue
		}

		return results, nil
	}

	return nil, fmt.Errorf("ai: extract-slice failed after %d attempts: %w", cfg.maxRetries+1, lastErr)
}

// ---------------------------------------------------------------------------
// Classify — structured classification
// ---------------------------------------------------------------------------

// Classify asks the model to choose one of the given labels for the text.
func Classify(ctx context.Context, text string, labels []string, opts ...ExtractOption) (string, error) {
	cfg := &extractConfig{
		maxRetries: 1,
		system:     "You are a classification assistant. Classify the text into exactly one of the given categories. Respond with only the category label, nothing else.",
	}
	for _, opt := range opts {
		opt(cfg)
	}

	prompt := fmt.Sprintf(
		"Classify the following text into one of these categories: %v\n\nText: %s\n\nRespond with only the category label.",
		labels, text,
	)

	client := GetClient()
	genOpts := []GenerateOption{WithSystem(cfg.system)}
	if cfg.model != "" {
		genOpts = append(genOpts, WithModel(cfg.model))
	}

	resp, err := client.Generate(ctx, prompt, genOpts...)
	if err != nil {
		return "", err
	}

	return resp.Text, nil
}

// ---------------------------------------------------------------------------
// JSON cleaning helper
// ---------------------------------------------------------------------------

// cleanJSON strips markdown code fences and surrounding whitespace
// from model output that is supposed to be JSON.
func cleanJSON(s string) string {
	// Trim whitespace.
	b := []byte(s)

	// Remove ```json ... ``` wrappers.
	if len(b) > 7 && string(b[:7]) == "```json" {
		b = b[7:]
	} else if len(b) > 3 && string(b[:3]) == "```" {
		b = b[3:]
	}
	if len(b) > 3 && string(b[len(b)-3:]) == "```" {
		b = b[:len(b)-3]
	}

	// Trim surrounding whitespace/newlines.
	start := 0
	for start < len(b) && (b[start] == ' ' || b[start] == '\n' || b[start] == '\r' || b[start] == '\t') {
		start++
	}
	end := len(b)
	for end > start && (b[end-1] == ' ' || b[end-1] == '\n' || b[end-1] == '\r' || b[end-1] == '\t') {
		end--
	}

	return string(b[start:end])
}
