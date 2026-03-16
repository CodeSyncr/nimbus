/*
|--------------------------------------------------------------------------
| AI SDK — Prompt Templates
|--------------------------------------------------------------------------
|
| Reusable, composable prompt templates with variable interpolation.
| Uses Go's text/template under the hood but provides a simpler API
| for common cases.
|
| Usage:
|
|   // Simple interpolation ({{.key}} syntax)
|   prompt := ai.Template("Summarize: {{.text}}")
|   result, err := prompt.Format(map[string]any{"text": article})
|
|   // Generate directly
|   resp, err := prompt.Generate(ctx, map[string]any{"text": article})
|
|   // Compose templates
|   system := ai.Template("You are a {{.role}} expert.")
|   user := ai.Template("Explain {{.topic}} in {{.style}} style.")
|
|   resp, err := ai.Chain(system, user).Generate(ctx, map[string]any{
|       "role":  "Go programming",
|       "topic": "channels",
|       "style": "beginner-friendly",
|   })
|
*/

package ai

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
)

// ---------------------------------------------------------------------------
// PromptTemplate
// ---------------------------------------------------------------------------

// PromptTemplate is a reusable prompt with variable interpolation.
type PromptTemplate struct {
	raw  string
	tmpl *template.Template
	role string // "system", "user", or "assistant"
}

// Template creates a new prompt template from the given string.
// Uses Go text/template syntax ({{.varName}}).
func Template(s string) *PromptTemplate {
	return &PromptTemplate{
		raw:  s,
		role: RoleUser,
	}
}

// SystemTemplate creates a system-role prompt template.
func SystemTemplate(s string) *PromptTemplate {
	return &PromptTemplate{
		raw:  s,
		role: RoleSystem,
	}
}

// Role sets the message role for this template.
func (p *PromptTemplate) Role(role string) *PromptTemplate {
	p.role = role
	return p
}

// Format renders the template with the given variables.
func (p *PromptTemplate) Format(vars map[string]any) (string, error) {
	if err := p.compile(); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := p.tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("ai: template: %w", err)
	}
	return buf.String(), nil
}

// MustFormat renders or panics. Useful in init() or tests.
func (p *PromptTemplate) MustFormat(vars map[string]any) string {
	s, err := p.Format(vars)
	if err != nil {
		panic(err)
	}
	return s
}

// Generate renders the template and sends it to the AI.
func (p *PromptTemplate) Generate(ctx context.Context, vars map[string]any, opts ...GenerateOption) (*GenerateResponse, error) {
	text, err := p.Format(vars)
	if err != nil {
		return nil, err
	}

	if p.role == RoleSystem {
		opts = append([]GenerateOption{WithSystem(text)}, opts...)
		return GetClient().Generate(ctx, "", opts...)
	}

	return GetClient().Generate(ctx, text, opts...)
}

// compile lazily parses the template.
func (p *PromptTemplate) compile() error {
	if p.tmpl != nil {
		return nil
	}
	var err error
	p.tmpl, err = template.New("prompt").Parse(p.raw)
	if err != nil {
		return fmt.Errorf("ai: template parse: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Chain — composable prompt sequence
// ---------------------------------------------------------------------------

// PromptChain composes multiple templates into a single generation call.
type PromptChain struct {
	templates []*PromptTemplate
}

// Chain creates a prompt chain from one or more templates.
func Chain(templates ...*PromptTemplate) *PromptChain {
	return &PromptChain{templates: templates}
}

// Generate renders all templates and builds a multi-message request.
func (c *PromptChain) Generate(ctx context.Context, vars map[string]any, opts ...GenerateOption) (*GenerateResponse, error) {
	var system string
	var msgs []Message

	for _, t := range c.templates {
		text, err := t.Format(vars)
		if err != nil {
			return nil, err
		}
		if t.role == RoleSystem {
			system = text
		} else {
			msgs = append(msgs, Message{Role: t.role, Content: text})
		}
	}

	genOpts := []GenerateOption{}
	if system != "" {
		genOpts = append(genOpts, WithSystem(system))
	}
	if len(msgs) > 0 {
		genOpts = append(genOpts, WithMessages(msgs))
	}
	genOpts = append(genOpts, opts...)

	return GetClient().Generate(ctx, "", genOpts...)
}

// ---------------------------------------------------------------------------
// FewShot — few-shot prompt builder
// ---------------------------------------------------------------------------

// Example represents a single few-shot example.
type Example struct {
	Input  string
	Output string
}

// FewShotTemplate builds a prompt with few-shot examples.
type FewShotTemplate struct {
	instruction string
	examples    []Example
	separator   string
}

// FewShot creates a few-shot prompt builder.
func FewShot(instruction string) *FewShotTemplate {
	return &FewShotTemplate{
		instruction: instruction,
		separator:   "\n---\n",
	}
}

// Add adds a few-shot example.
func (f *FewShotTemplate) Add(input, output string) *FewShotTemplate {
	f.examples = append(f.examples, Example{Input: input, Output: output})
	return f
}

// Separator sets the separator between examples.
func (f *FewShotTemplate) Separator(sep string) *FewShotTemplate {
	f.separator = sep
	return f
}

// Format builds the prompt string.
func (f *FewShotTemplate) Format(input string) string {
	var b strings.Builder
	b.WriteString(f.instruction)
	b.WriteString("\n\n")

	for _, ex := range f.examples {
		b.WriteString("Input: ")
		b.WriteString(ex.Input)
		b.WriteString("\nOutput: ")
		b.WriteString(ex.Output)
		b.WriteString(f.separator)
	}

	b.WriteString("Input: ")
	b.WriteString(input)
	b.WriteString("\nOutput:")

	return b.String()
}

// Generate formats and generates.
func (f *FewShotTemplate) Generate(ctx context.Context, input string, opts ...GenerateOption) (*GenerateResponse, error) {
	return GetClient().Generate(ctx, f.Format(input), opts...)
}
