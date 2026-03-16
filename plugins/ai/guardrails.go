/*
|--------------------------------------------------------------------------
| AI SDK — Guardrails
|--------------------------------------------------------------------------
|
| Safety and quality guardrails for AI outputs. Validates model
| responses against configurable rules before returning them to
| the caller.
|
| Usage:
|
|   g := ai.NewGuardrails().
|       MaxLength(5000).
|       BlockPatterns("(?i)password", "(?i)credit.?card").
|       ContentFilter(ai.FilterHate | ai.FilterViolence).
|       CustomCheck(myValidator)
|
|   resp, err := ai.Generate(ctx, prompt)
|   if err := g.Validate(resp.Text); err != nil {
|       // response violated guardrails
|   }
|
|   // Or wrap a client:
|   safeClient := ai.WithGuardrails(client, g)
|
*/

package ai

import (
	"fmt"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Content filters (bitmask)
// ---------------------------------------------------------------------------

// ContentFilter is a bitmask for built-in content categories.
type ContentFilter int

const (
	FilterNone ContentFilter = 0
	FilterHate ContentFilter = 1 << iota
	FilterViolence
	FilterSexual
	FilterSelfHarm
	FilterPII
)

// ---------------------------------------------------------------------------
// Guardrails
// ---------------------------------------------------------------------------

// Guardrails validates AI outputs against safety and quality rules.
type Guardrails struct {
	maxLength int
	minLength int
	blocked   []*regexp.Regexp
	required  []string
	filters   ContentFilter
	custom    []GuardrailCheck
}

// GuardrailCheck is a custom validation function. Return an error if
// the output violates the guardrail.
type GuardrailCheck func(output string) error

// NewGuardrails creates an empty guardrails configuration.
func NewGuardrails() *Guardrails {
	return &Guardrails{}
}

// MaxLength sets the maximum allowed output length.
func (g *Guardrails) MaxLength(n int) *Guardrails {
	g.maxLength = n
	return g
}

// MinLength sets the minimum required output length.
func (g *Guardrails) MinLength(n int) *Guardrails {
	g.minLength = n
	return g
}

// BlockPatterns adds regex patterns that must NOT appear in the output.
func (g *Guardrails) BlockPatterns(patterns ...string) *Guardrails {
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		g.blocked = append(g.blocked, re)
	}
	return g
}

// RequireKeywords ensures the output contains all specified keywords.
func (g *Guardrails) RequireKeywords(keywords ...string) *Guardrails {
	g.required = append(g.required, keywords...)
	return g
}

// ContentFilter sets the content safety filter bitmask.
func (g *Guardrails) SetContentFilter(f ContentFilter) *Guardrails {
	g.filters = f
	return g
}

// CustomCheck adds a custom guardrail check.
func (g *Guardrails) CustomCheck(fn GuardrailCheck) *Guardrails {
	g.custom = append(g.custom, fn)
	return g
}

// Validate checks the output against all configured guardrails.
// Returns nil if the output passes all checks.
func (g *Guardrails) Validate(output string) error {
	// Length checks.
	if g.maxLength > 0 && len(output) > g.maxLength {
		return fmt.Errorf("ai: guardrail: output exceeds max length (%d > %d)", len(output), g.maxLength)
	}
	if g.minLength > 0 && len(output) < g.minLength {
		return fmt.Errorf("ai: guardrail: output below min length (%d < %d)", len(output), g.minLength)
	}

	// Blocked patterns.
	for _, re := range g.blocked {
		if re.MatchString(output) {
			return fmt.Errorf("ai: guardrail: output matches blocked pattern %q", re.String())
		}
	}

	// Required keywords.
	lower := strings.ToLower(output)
	for _, kw := range g.required {
		if !strings.Contains(lower, strings.ToLower(kw)) {
			return fmt.Errorf("ai: guardrail: output missing required keyword %q", kw)
		}
	}

	// Built-in content filters (keyword-based heuristics).
	if g.filters&FilterPII != 0 {
		piiPatterns := []string{
			`\b\d{3}-\d{2}-\d{4}\b`, // SSN
			`\b\d{16}\b`,            // credit card
			`\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`, // email (loose)
		}
		for _, p := range piiPatterns {
			if re, err := regexp.Compile("(?i)" + p); err == nil && re.MatchString(output) {
				return fmt.Errorf("ai: guardrail: output may contain PII")
			}
		}
	}

	// Custom checks.
	for _, fn := range g.custom {
		if err := fn(output); err != nil {
			return fmt.Errorf("ai: guardrail: custom check failed: %w", err)
		}
	}

	return nil
}

// ValidateResponse is a convenience that validates a GenerateResponse.
func (g *Guardrails) ValidateResponse(resp *GenerateResponse) error {
	if resp == nil {
		return fmt.Errorf("ai: guardrail: nil response")
	}
	return g.Validate(resp.Text)
}
