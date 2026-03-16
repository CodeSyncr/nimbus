/*
|--------------------------------------------------------------------------
| AI SDK — Cost Tracking Dashboard
|--------------------------------------------------------------------------
|
| Tracks per-model, per-provider costs with configurable pricing.
| Provides real-time cost breakdowns, budget alerts, and a
| dashboard-ready data API.
|
| Usage:
|
|   ai.EnableCostTracking(ai.CostConfig{
|       MonthlyBudget: 500.00, // $500 monthly budget
|       OnBudgetAlert: func(usage ai.CostSummary) {
|           log.Printf("AI budget at %.0f%%", usage.BudgetPercent)
|       },
|   })
|
|   // Get cost dashboard data
|   dashboard := ai.GetCostDashboard()
|
*/

package ai

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Pricing models
// ---------------------------------------------------------------------------

// ModelPricing holds per-1K token prices for a model.
type ModelPricing struct {
	PromptPer1K     float64 `json:"prompt_per_1k"`
	CompletionPer1K float64 `json:"completion_per_1k"`
}

// DefaultPricing is the built-in pricing table (USD per 1K tokens).
// Override or extend with SetModelPricing.
var DefaultPricing = map[string]ModelPricing{
	// OpenAI
	"gpt-4o":        {PromptPer1K: 0.0025, CompletionPer1K: 0.010},
	"gpt-4o-mini":   {PromptPer1K: 0.00015, CompletionPer1K: 0.0006},
	"gpt-4-turbo":   {PromptPer1K: 0.010, CompletionPer1K: 0.030},
	"gpt-4":         {PromptPer1K: 0.030, CompletionPer1K: 0.060},
	"gpt-3.5-turbo": {PromptPer1K: 0.0005, CompletionPer1K: 0.0015},
	"o1":            {PromptPer1K: 0.015, CompletionPer1K: 0.060},
	"o1-mini":       {PromptPer1K: 0.003, CompletionPer1K: 0.012},
	"o3-mini":       {PromptPer1K: 0.0011, CompletionPer1K: 0.0044},
	// Anthropic
	"claude-3-5-sonnet-20241022": {PromptPer1K: 0.003, CompletionPer1K: 0.015},
	"claude-3-5-haiku-20241022":  {PromptPer1K: 0.0008, CompletionPer1K: 0.004},
	"claude-3-opus-20240229":     {PromptPer1K: 0.015, CompletionPer1K: 0.075},
	"claude-sonnet-4-20250514":   {PromptPer1K: 0.003, CompletionPer1K: 0.015},
	"claude-opus-4-20250514":     {PromptPer1K: 0.015, CompletionPer1K: 0.075},
	// Google
	"gemini-2.0-flash": {PromptPer1K: 0.0001, CompletionPer1K: 0.0004},
	"gemini-1.5-pro":   {PromptPer1K: 0.00125, CompletionPer1K: 0.005},
	"gemini-1.5-flash": {PromptPer1K: 0.000075, CompletionPer1K: 0.0003},
	// Mistral
	"mistral-large-latest": {PromptPer1K: 0.002, CompletionPer1K: 0.006},
	"mistral-small-latest": {PromptPer1K: 0.0002, CompletionPer1K: 0.0006},
	// Cohere
	"command-r-plus": {PromptPer1K: 0.003, CompletionPer1K: 0.015},
	"command-r":      {PromptPer1K: 0.0005, CompletionPer1K: 0.0015},
	// xAI
	"grok-2":      {PromptPer1K: 0.002, CompletionPer1K: 0.010},
	"grok-2-mini": {PromptPer1K: 0.0002, CompletionPer1K: 0.001},
	// Default fallback
	"_default": {PromptPer1K: 0.001, CompletionPer1K: 0.002},
}

// SetModelPricing sets or updates pricing for a model.
func SetModelPricing(model string, pricing ModelPricing) {
	DefaultPricing[model] = pricing
}

// ---------------------------------------------------------------------------
// Cost configuration
// ---------------------------------------------------------------------------

// CostConfig configures cost tracking.
type CostConfig struct {
	// MonthlyBudget is the monthly budget in USD. 0 = unlimited.
	MonthlyBudget float64

	// AlertThresholds are budget percentages that trigger alerts.
	// Default: [50, 75, 90, 100].
	AlertThresholds []float64

	// OnBudgetAlert is called when a threshold is crossed.
	OnBudgetAlert func(CostSummary)

	// CustomPricing overrides or extends default pricing.
	CustomPricing map[string]ModelPricing
}

// ---------------------------------------------------------------------------
// Cost tracker
// ---------------------------------------------------------------------------

// CostEntry records a single API call's cost.
type CostEntry struct {
	Timestamp        time.Time `json:"timestamp"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	PromptCost       float64   `json:"prompt_cost"`
	CompletionCost   float64   `json:"completion_cost"`
	TotalCost        float64   `json:"total_cost"`
}

// CostSummary is the aggregate cost data for the dashboard.
type CostSummary struct {
	// Totals
	TotalCost       float64 `json:"total_cost"`
	TotalRequests   int64   `json:"total_requests"`
	TotalTokens     int64   `json:"total_tokens"`
	MonthlyBudget   float64 `json:"monthly_budget"`
	BudgetPercent   float64 `json:"budget_percent"`
	BudgetRemaining float64 `json:"budget_remaining"`

	// Per-model breakdown
	CostByModel map[string]*ModelCostSummary `json:"cost_by_model"`

	// Per-provider breakdown
	CostByProvider map[string]*ProviderCostSummary `json:"cost_by_provider"`

	// Time-series (hourly buckets for the current day)
	HourlyCosts [24]float64 `json:"hourly_costs"`

	// Recent entries for detailed view
	RecentEntries []CostEntry `json:"recent_entries"`
}

// ModelCostSummary tracks costs for a specific model.
type ModelCostSummary struct {
	Model            string  `json:"model"`
	Requests         int64   `json:"requests"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalCost        float64 `json:"total_cost"`
	AvgCostPerReq    float64 `json:"avg_cost_per_request"`
}

// ProviderCostSummary tracks costs for a specific provider.
type ProviderCostSummary struct {
	Provider  string  `json:"provider"`
	Requests  int64   `json:"requests"`
	TotalCost float64 `json:"total_cost"`
}

type costTracker struct {
	config          CostConfig
	mu              sync.RWMutex
	entries         []CostEntry
	maxEntries      int
	totalCost       float64
	totalRequests   int64
	totalTokens     int64
	costByModel     map[string]*ModelCostSummary
	costByProvider  map[string]*ProviderCostSummary
	hourlyCosts     [24]float64
	lastHourlyReset time.Time
	alertedAt       map[int]bool // threshold index -> alerted
}

var (
	globalCostTracker *costTracker
	costTrackerMu     sync.RWMutex
)

// EnableCostTracking activates cost tracking with the given config.
func EnableCostTracking(cfg CostConfig) {
	if len(cfg.AlertThresholds) == 0 {
		cfg.AlertThresholds = []float64{50, 75, 90, 100}
	}

	// Apply custom pricing.
	for model, pricing := range cfg.CustomPricing {
		DefaultPricing[model] = pricing
	}

	ct := &costTracker{
		config:          cfg,
		maxEntries:      10000,
		costByModel:     make(map[string]*ModelCostSummary),
		costByProvider:  make(map[string]*ProviderCostSummary),
		alertedAt:       make(map[int]bool),
		lastHourlyReset: time.Now().Truncate(24 * time.Hour),
	}

	costTrackerMu.Lock()
	globalCostTracker = ct
	costTrackerMu.Unlock()

	// Install observability hook.
	OnCompletion(func(e RequestEvent) {
		ct.recordCost(e)
	})
}

// GetCostDashboard returns the current cost summary for dashboard display.
func GetCostDashboard() CostSummary {
	costTrackerMu.RLock()
	ct := globalCostTracker
	costTrackerMu.RUnlock()

	if ct == nil {
		return CostSummary{
			CostByModel:    make(map[string]*ModelCostSummary),
			CostByProvider: make(map[string]*ProviderCostSummary),
		}
	}
	return ct.summary()
}

// GetTotalCost returns the current total cost.
func GetTotalCost() float64 {
	costTrackerMu.RLock()
	ct := globalCostTracker
	costTrackerMu.RUnlock()
	if ct == nil {
		return 0
	}
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.totalCost
}

// ResetCosts resets all cost tracking counters (e.g., monthly reset).
func ResetCosts() {
	costTrackerMu.RLock()
	ct := globalCostTracker
	costTrackerMu.RUnlock()
	if ct == nil {
		return
	}
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.entries = nil
	ct.totalCost = 0
	ct.totalRequests = 0
	ct.totalTokens = 0
	ct.costByModel = make(map[string]*ModelCostSummary)
	ct.costByProvider = make(map[string]*ProviderCostSummary)
	ct.hourlyCosts = [24]float64{}
	ct.alertedAt = make(map[int]bool)
}

// CostReport returns a formatted cost report string.
func CostReport() string {
	d := GetCostDashboard()

	report := fmt.Sprintf("AI Cost Report\n"+
		"==============\n"+
		"Total Cost:     $%.4f\n"+
		"Total Requests: %d\n"+
		"Total Tokens:   %d\n",
		d.TotalCost, d.TotalRequests, d.TotalTokens)

	if d.MonthlyBudget > 0 {
		report += fmt.Sprintf("Budget:         $%.2f (%.1f%% used, $%.2f remaining)\n",
			d.MonthlyBudget, d.BudgetPercent, d.BudgetRemaining)
	}

	report += "\nPer Model:\n"
	for _, m := range d.CostByModel {
		report += fmt.Sprintf("  %-30s %5d reqs  $%.4f  (avg $%.6f/req)\n",
			m.Model, m.Requests, m.TotalCost, m.AvgCostPerReq)
	}

	report += "\nPer Provider:\n"
	for _, p := range d.CostByProvider {
		report += fmt.Sprintf("  %-20s %5d reqs  $%.4f\n",
			p.Provider, p.Requests, p.TotalCost)
	}

	return report
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

func (ct *costTracker) recordCost(e RequestEvent) {
	if e.Usage == nil {
		return
	}

	pricing := ct.getPricing(e.Model)
	promptCost := float64(e.Usage.PromptTokens) / 1000.0 * pricing.PromptPer1K
	completionCost := float64(e.Usage.CompletionTokens) / 1000.0 * pricing.CompletionPer1K
	totalCost := promptCost + completionCost

	entry := CostEntry{
		Timestamp:        e.Timestamp,
		Provider:         e.Provider,
		Model:            e.Model,
		PromptTokens:     e.Usage.PromptTokens,
		CompletionTokens: e.Usage.CompletionTokens,
		TotalTokens:      e.Usage.TotalTokens,
		PromptCost:       promptCost,
		CompletionCost:   completionCost,
		TotalCost:        totalCost,
	}

	ct.mu.Lock()

	// Reset hourly buckets if day changed.
	today := time.Now().Truncate(24 * time.Hour)
	if today.After(ct.lastHourlyReset) {
		ct.hourlyCosts = [24]float64{}
		ct.lastHourlyReset = today
	}

	// Record entry.
	ct.entries = append(ct.entries, entry)
	if len(ct.entries) > ct.maxEntries {
		ct.entries = ct.entries[len(ct.entries)-ct.maxEntries:]
	}

	ct.totalCost += totalCost
	ct.totalRequests++
	atomic.AddInt64(&ct.totalTokens, int64(e.Usage.TotalTokens))

	// Per-model tracking.
	ms, ok := ct.costByModel[e.Model]
	if !ok {
		ms = &ModelCostSummary{Model: e.Model}
		ct.costByModel[e.Model] = ms
	}
	ms.Requests++
	ms.PromptTokens += int64(e.Usage.PromptTokens)
	ms.CompletionTokens += int64(e.Usage.CompletionTokens)
	ms.TotalCost += totalCost
	ms.AvgCostPerReq = ms.TotalCost / float64(ms.Requests)

	// Per-provider tracking.
	ps, ok := ct.costByProvider[e.Provider]
	if !ok {
		ps = &ProviderCostSummary{Provider: e.Provider}
		ct.costByProvider[e.Provider] = ps
	}
	ps.Requests++
	ps.TotalCost += totalCost

	// Hourly bucket.
	hour := time.Now().Hour()
	ct.hourlyCosts[hour] += totalCost

	currentCost := ct.totalCost
	ct.mu.Unlock()

	// Check budget alerts.
	if ct.config.MonthlyBudget > 0 && ct.config.OnBudgetAlert != nil {
		pct := (currentCost / ct.config.MonthlyBudget) * 100
		for i, threshold := range ct.config.AlertThresholds {
			if pct >= threshold && !ct.alertedAt[i] {
				ct.alertedAt[i] = true
				ct.config.OnBudgetAlert(ct.summary())
			}
		}
	}
}

func (ct *costTracker) getPricing(model string) ModelPricing {
	if p, ok := DefaultPricing[model]; ok {
		return p
	}
	return DefaultPricing["_default"]
}

func (ct *costTracker) summary() CostSummary {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	s := CostSummary{
		TotalCost:      ct.totalCost,
		TotalRequests:  ct.totalRequests,
		TotalTokens:    ct.totalTokens,
		MonthlyBudget:  ct.config.MonthlyBudget,
		CostByModel:    ct.costByModel,
		CostByProvider: ct.costByProvider,
		HourlyCosts:    ct.hourlyCosts,
	}

	if ct.config.MonthlyBudget > 0 {
		s.BudgetPercent = (ct.totalCost / ct.config.MonthlyBudget) * 100
		s.BudgetRemaining = ct.config.MonthlyBudget - ct.totalCost
		if s.BudgetRemaining < 0 {
			s.BudgetRemaining = 0
		}
	}

	// Recent entries (last 50).
	recentCount := 50
	if recentCount > len(ct.entries) {
		recentCount = len(ct.entries)
	}
	s.RecentEntries = make([]CostEntry, recentCount)
	copy(s.RecentEntries, ct.entries[len(ct.entries)-recentCount:])

	return s
}
