/*
|--------------------------------------------------------------------------
| AI SDK — Model Evaluation & Benchmarking
|--------------------------------------------------------------------------
|
| Tools for evaluating AI model quality, comparing providers, and
| running benchmark suites. Supports custom metrics, automated
| scoring, and structured reports.
|
| Usage:
|
|   suite := ai.NewEvalSuite("qa-quality").
|       AddCase("greeting", "Say hello", ai.ExpectContains("hello")).
|       AddCase("math", "What is 2+2?", ai.ExpectContains("4"))
|
|   report := suite.Run(ctx)
|   fmt.Println(report.Summary())
|
*/

package ai

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Evaluation types
// ---------------------------------------------------------------------------

// EvalCase is a single test case for model evaluation.
type EvalCase struct {
	Name     string            `json:"name"`
	Prompt   string            `json:"prompt"`
	System   string            `json:"system,omitempty"`
	Options  []GenerateOption  `json:"-"`
	Checks   []EvalCheck       `json:"-"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// EvalCheck is a function that scores an AI response.
// Returns a score between 0.0 and 1.0, and an explanation.
type EvalCheck func(response string) EvalScore

// EvalScore is the result of one evaluation check.
type EvalScore struct {
	Name    string  `json:"name"`
	Score   float64 `json:"score"`   // 0.0 to 1.0
	Passed  bool    `json:"passed"`  // true if score >= threshold
	Details string  `json:"details"` // human-readable explanation
}

// EvalResult is the result of evaluating one case.
type EvalResult struct {
	Case     string        `json:"case"`
	Prompt   string        `json:"prompt"`
	Response string        `json:"response"`
	Model    string        `json:"model"`
	Provider string        `json:"provider"`
	Scores   []EvalScore   `json:"scores"`
	AvgScore float64       `json:"avg_score"`
	Passed   bool          `json:"passed"`
	Latency  time.Duration `json:"latency"`
	Tokens   int           `json:"tokens"`
	Cost     float64       `json:"cost"`
	Error    string        `json:"error,omitempty"`
}

// EvalReport is the full report from running a benchmark suite.
type EvalReport struct {
	Suite      string        `json:"suite"`
	Model      string        `json:"model"`
	Provider   string        `json:"provider"`
	Results    []EvalResult  `json:"results"`
	TotalCases int           `json:"total_cases"`
	Passed     int           `json:"passed"`
	Failed     int           `json:"failed"`
	Errors     int           `json:"errors"`
	AvgScore   float64       `json:"avg_score"`
	AvgLatency time.Duration `json:"avg_latency"`
	TotalCost  float64       `json:"total_cost"`
	Duration   time.Duration `json:"duration"`
	Timestamp  time.Time     `json:"timestamp"`
}

// Summary returns a human-readable summary of the evaluation report.
func (r *EvalReport) Summary() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Evaluation Report: %s\n", r.Suite))
	sb.WriteString(fmt.Sprintf("Model: %s | Provider: %s\n", r.Model, r.Provider))
	sb.WriteString(fmt.Sprintf("Duration: %s\n", r.Duration))
	sb.WriteString(strings.Repeat("─", 60) + "\n")
	sb.WriteString(fmt.Sprintf("Total: %d | Passed: %d | Failed: %d | Errors: %d\n",
		r.TotalCases, r.Passed, r.Failed, r.Errors))
	sb.WriteString(fmt.Sprintf("Avg Score: %.2f | Avg Latency: %s | Total Cost: $%.4f\n",
		r.AvgScore, r.AvgLatency, r.TotalCost))
	sb.WriteString(strings.Repeat("─", 60) + "\n")

	for _, res := range r.Results {
		icon := "✓"
		if !res.Passed {
			icon = "✗"
		}
		if res.Error != "" {
			icon = "!"
		}
		sb.WriteString(fmt.Sprintf("[%s] %-30s score=%.2f latency=%s\n",
			icon, res.Case, res.AvgScore, res.Latency))
		for _, s := range res.Scores {
			sb.WriteString(fmt.Sprintf("    %-20s %.2f  %s\n", s.Name, s.Score, s.Details))
		}
	}
	return sb.String()
}

// ComparisonReport compares evaluation results across models.
type ComparisonReport struct {
	Suite   string       `json:"suite"`
	Reports []EvalReport `json:"reports"`
}

// Summary returns a comparison summary.
func (cr *ComparisonReport) Summary() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Model Comparison: %s\n", cr.Suite))
	sb.WriteString(strings.Repeat("═", 70) + "\n")
	sb.WriteString(fmt.Sprintf("%-25s %8s %8s %10s %10s\n", "Model", "Score", "Pass%", "Latency", "Cost"))
	sb.WriteString(strings.Repeat("─", 70) + "\n")

	for _, r := range cr.Reports {
		passPct := float64(r.Passed) / float64(r.TotalCases) * 100
		sb.WriteString(fmt.Sprintf("%-25s %7.2f%% %7.1f%% %10s $%.4f\n",
			r.Model, r.AvgScore*100, passPct, r.AvgLatency, r.TotalCost))
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Built-in evaluation checks
// ---------------------------------------------------------------------------

// ExpectContains checks that the response contains the expected substring.
func ExpectContains(substr string) EvalCheck {
	return func(response string) EvalScore {
		contains := strings.Contains(strings.ToLower(response), strings.ToLower(substr))
		score := 0.0
		if contains {
			score = 1.0
		}
		return EvalScore{
			Name:    "contains:" + substr,
			Score:   score,
			Passed:  contains,
			Details: fmt.Sprintf("expected to contain %q", substr),
		}
	}
}

// ExpectNotContains checks the response does NOT contain.
func ExpectNotContains(substr string) EvalCheck {
	return func(response string) EvalScore {
		notContains := !strings.Contains(strings.ToLower(response), strings.ToLower(substr))
		score := 0.0
		if notContains {
			score = 1.0
		}
		return EvalScore{
			Name:    "not_contains:" + substr,
			Score:   score,
			Passed:  notContains,
			Details: fmt.Sprintf("expected NOT to contain %q", substr),
		}
	}
}

// ExpectMinLength checks the response is at least n characters.
func ExpectMinLength(n int) EvalCheck {
	return func(response string) EvalScore {
		ok := len(response) >= n
		score := 0.0
		if ok {
			score = 1.0
		} else if len(response) > 0 {
			score = float64(len(response)) / float64(n)
		}
		return EvalScore{
			Name:    fmt.Sprintf("min_length:%d", n),
			Score:   score,
			Passed:  ok,
			Details: fmt.Sprintf("length=%d, expected>=%d", len(response), n),
		}
	}
}

// ExpectMaxLength checks the response is at most n characters.
func ExpectMaxLength(n int) EvalCheck {
	return func(response string) EvalScore {
		ok := len(response) <= n
		score := 0.0
		if ok {
			score = 1.0
		} else {
			score = float64(n) / float64(len(response))
		}
		return EvalScore{
			Name:    fmt.Sprintf("max_length:%d", n),
			Score:   score,
			Passed:  ok,
			Details: fmt.Sprintf("length=%d, expected<=%d", len(response), n),
		}
	}
}

// ExpectJSON checks that the response is valid JSON.
func ExpectJSON() EvalCheck {
	return func(response string) EvalScore {
		cleaned := strings.TrimSpace(response)
		isJSON := (strings.HasPrefix(cleaned, "{") && strings.HasSuffix(cleaned, "}")) ||
			(strings.HasPrefix(cleaned, "[") && strings.HasSuffix(cleaned, "]"))
		score := 0.0
		if isJSON {
			score = 1.0
		}
		return EvalScore{
			Name:    "valid_json",
			Score:   score,
			Passed:  isJSON,
			Details: "expected valid JSON response",
		}
	}
}

// ExpectSimilarity uses the AI model itself to judge response quality.
// The expected string is compared semantically (costs an extra API call).
func ExpectSimilarity(expected string, threshold float64) EvalCheck {
	return func(response string) EvalScore {
		// Simple word overlap as a heuristic (no extra API call).
		expectedWords := strings.Fields(strings.ToLower(expected))
		responseWords := strings.Fields(strings.ToLower(response))

		if len(expectedWords) == 0 {
			return EvalScore{Name: "similarity", Score: 1.0, Passed: true, Details: "empty expected"}
		}

		responseSet := make(map[string]bool)
		for _, w := range responseWords {
			responseSet[w] = true
		}

		matches := 0
		for _, w := range expectedWords {
			if responseSet[w] {
				matches++
			}
		}
		score := float64(matches) / float64(len(expectedWords))
		return EvalScore{
			Name:    "similarity",
			Score:   score,
			Passed:  score >= threshold,
			Details: fmt.Sprintf("word overlap=%.2f, threshold=%.2f", score, threshold),
		}
	}
}

// LLMJudge uses a separate LLM call to evaluate the response quality.
func LLMJudge(criteria string) EvalCheck {
	return func(response string) EvalScore {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		prompt := fmt.Sprintf(
			"Rate the following AI response on a scale of 0-10 based on this criteria: %s\n\n"+
				"Response to evaluate:\n%s\n\n"+
				"Reply with ONLY a number 0-10, nothing else.",
			criteria, response,
		)

		resp, err := Generate(ctx, prompt,
			WithMaxTokens(10),
			WithTemperature(0),
		)
		if err != nil {
			return EvalScore{Name: "llm_judge", Score: 0.5, Passed: false, Details: "judge error: " + err.Error()}
		}

		// Parse the numeric score.
		scoreText := strings.TrimSpace(resp.Text)
		var numScore float64
		_, _ = fmt.Sscanf(scoreText, "%f", &numScore)
		normalizedScore := numScore / 10.0
		if normalizedScore > 1.0 {
			normalizedScore = 1.0
		}
		if normalizedScore < 0 {
			normalizedScore = 0
		}

		return EvalScore{
			Name:    "llm_judge",
			Score:   normalizedScore,
			Passed:  normalizedScore >= 0.7,
			Details: fmt.Sprintf("criteria=%q, raw_score=%s", criteria, scoreText),
		}
	}
}

// CustomCheck creates an EvalCheck from a custom scoring function.
func CustomCheck(name string, fn func(response string) (float64, string)) EvalCheck {
	return func(response string) EvalScore {
		score, details := fn(response)
		return EvalScore{
			Name:    name,
			Score:   score,
			Passed:  score >= 0.5,
			Details: details,
		}
	}
}

// ---------------------------------------------------------------------------
// EvalSuite
// ---------------------------------------------------------------------------

// EvalSuite is a collection of evaluation cases.
type EvalSuite struct {
	name     string
	cases    []EvalCase
	model    string
	options  []GenerateOption
	parallel int
}

// NewEvalSuite creates a new evaluation suite.
func NewEvalSuite(name string) *EvalSuite {
	return &EvalSuite{
		name:     name,
		parallel: 1,
	}
}

// AddCase adds a test case to the suite.
func (s *EvalSuite) AddCase(name, prompt string, checks ...EvalCheck) *EvalSuite {
	s.cases = append(s.cases, EvalCase{
		Name:   name,
		Prompt: prompt,
		Checks: checks,
	})
	return s
}

// AddCaseWithSystem adds a test case with a system prompt.
func (s *EvalSuite) AddCaseWithSystem(name, system, prompt string, checks ...EvalCheck) *EvalSuite {
	s.cases = append(s.cases, EvalCase{
		Name:   name,
		Prompt: prompt,
		System: system,
		Checks: checks,
	})
	return s
}

// WithModel sets the model to evaluate.
func (s *EvalSuite) WithModel(model string) *EvalSuite {
	s.model = model
	return s
}

// WithOptions sets default options for all cases.
func (s *EvalSuite) WithOptions(opts ...GenerateOption) *EvalSuite {
	s.options = opts
	return s
}

// Parallel sets the concurrency level for running cases.
func (s *EvalSuite) Parallel(n int) *EvalSuite {
	s.parallel = n
	return s
}

// Run executes all cases and returns the report.
func (s *EvalSuite) Run(ctx context.Context) *EvalReport {
	startTime := time.Now()

	report := &EvalReport{
		Suite:      s.name,
		Model:      s.model,
		TotalCases: len(s.cases),
		Timestamp:  startTime,
	}

	// Determine provider + model from client.
	client := GetClient()
	if report.Model == "" {
		report.Model = client.config.Model
	}
	report.Provider = client.config.Provider

	results := make([]EvalResult, len(s.cases))

	if s.parallel <= 1 {
		for i, ec := range s.cases {
			results[i] = s.runCase(ctx, ec)
		}
	} else {
		sem := make(chan struct{}, s.parallel)
		var wg sync.WaitGroup
		for i, ec := range s.cases {
			wg.Add(1)
			go func(idx int, c EvalCase) {
				defer wg.Done()
				sem <- struct{}{}
				results[idx] = s.runCase(ctx, c)
				<-sem
			}(i, ec)
		}
		wg.Wait()
	}

	var totalScore float64
	var totalLatency time.Duration
	for _, r := range results {
		report.Results = append(report.Results, r)
		totalScore += r.AvgScore
		totalLatency += r.Latency
		report.TotalCost += r.Cost
		if r.Error != "" {
			report.Errors++
		} else if r.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
	}

	if len(results) > 0 {
		report.AvgScore = totalScore / float64(len(results))
		report.AvgLatency = totalLatency / time.Duration(len(results))
	}
	report.Duration = time.Since(startTime)

	return report
}

func (s *EvalSuite) runCase(ctx context.Context, ec EvalCase) EvalResult {
	opts := make([]GenerateOption, 0, len(s.options)+len(ec.Options)+2)
	opts = append(opts, s.options...)
	opts = append(opts, ec.Options...)
	if s.model != "" {
		opts = append(opts, WithModel(s.model))
	}
	if ec.System != "" {
		opts = append(opts, WithSystem(ec.System))
	}

	start := time.Now()
	resp, err := Generate(ctx, ec.Prompt, opts...)
	latency := time.Since(start)

	result := EvalResult{
		Case:    ec.Name,
		Prompt:  ec.Prompt,
		Model:   s.model,
		Latency: latency,
	}

	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Response = resp.Text
	if resp.Usage != nil {
		result.Tokens = resp.Usage.TotalTokens
		pricing := DefaultPricing[resp.Model]
		if pricing.PromptPer1K == 0 {
			pricing = DefaultPricing["_default"]
		}
		result.Cost = float64(resp.Usage.PromptTokens)/1000.0*pricing.PromptPer1K +
			float64(resp.Usage.CompletionTokens)/1000.0*pricing.CompletionPer1K
	}
	result.Model = resp.Model
	result.Provider = GetClient().config.Provider

	// Run checks.
	allPassed := true
	var totalScore float64
	for _, check := range ec.Checks {
		score := check(resp.Text)
		result.Scores = append(result.Scores, score)
		totalScore += score.Score
		if !score.Passed {
			allPassed = false
		}
	}

	if len(ec.Checks) > 0 {
		result.AvgScore = totalScore / float64(len(ec.Checks))
	} else {
		result.AvgScore = 1.0
	}
	result.Passed = allPassed

	return result
}

// ---------------------------------------------------------------------------
// Model comparison
// ---------------------------------------------------------------------------

// CompareModels runs the same eval suite across multiple models and
// returns a comparison report.
func CompareModels(ctx context.Context, suite *EvalSuite, models ...string) *ComparisonReport {
	cr := &ComparisonReport{Suite: suite.name}
	for _, model := range models {
		s := *suite // copy
		s.model = model
		report := s.Run(ctx)
		cr.Reports = append(cr.Reports, *report)
	}
	return cr
}
