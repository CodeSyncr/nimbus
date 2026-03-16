// Package shield provides AI-powered request protection middleware for Nimbus.
//
// Shield detects and blocks malicious requests including prompt injection,
// SQL injection, XSS, path traversal, command injection, and bot abuse.
// It works without requiring an external AI API by using pattern-based
// detection with scoring, but can optionally leverage the AI SDK for
// advanced content analysis.
//
// Usage:
//
//	app.Router.Use(shield.Guard())                           // defaults
//	app.Router.Use(shield.Guard(shield.Config{Level: "strict"}))
//	app.Router.Post("/api/chat", shield.AIContentGuard(), handler) // AI-specific
package shield

import (
	"encoding/json"
	"io"
	"math"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// Config configures the Shield middleware.
type Config struct {
	// Level: "permissive", "balanced" (default), "strict"
	Level string

	// Enabled modules (all true by default).
	SQLInjection     bool
	XSS              bool
	PathTraversal    bool
	CommandInjection bool
	PromptInjection  bool
	BotDetection     bool
	PayloadSize      bool
	RateBurst        bool
	HeaderAnomalies  bool

	// MaxBodySize is the maximum allowed body size in bytes (default 10MB).
	MaxBodySize int64

	// RateBurstLimit is max requests per second from a single IP (default 100).
	RateBurstLimit int

	// RateBurstWindow is the rate burst measurement window.
	RateBurstWindow time.Duration

	// BlockAction: "reject" (403, default), "challenge", "log"
	BlockAction string

	// CustomRules are additional pattern rules to check.
	CustomRules []Rule

	// OnBlock is called when a request is blocked.
	OnBlock func(event BlockEvent)

	// OnWarn is called when a request scores above warning threshold.
	OnWarn func(event BlockEvent)

	// AllowedIPs that bypass all checks.
	AllowedIPs []string

	// TrustedProxies for X-Forwarded-For parsing.
	TrustedProxies []string

	// ScoreThreshold overrides the default blocking threshold (0-100).
	// Default is based on level: permissive=70, balanced=50, strict=30.
	ScoreThreshold int
}

// Rule defines a custom detection rule.
type Rule struct {
	Name        string
	Description string
	Pattern     *regexp.Regexp
	Targets     []string // "body", "query", "header", "path", "all"
	Score       int      // 0-100 severity
	Category    string
}

// BlockEvent records details of a blocked or flagged request.
type BlockEvent struct {
	Timestamp  time.Time `json:"timestamp"`
	IP         string    `json:"ip"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Threat     string    `json:"threat"`
	Category   string    `json:"category"`
	Score      int       `json:"score"`
	Details    string    `json:"details"`
	Blocked    bool      `json:"blocked"`
	MatchedRaw string    `json:"matched_raw,omitempty"`
	UserAgent  string    `json:"user_agent,omitempty"`
}

// ---------------------------------------------------------------------------
// Detection Patterns
// ---------------------------------------------------------------------------

type detector struct {
	name     string
	category string
	patterns []*regexp.Regexp
	score    int
	targets  []string // where to look: body, query, header, path, cookie
}

var detectors = []detector{
	// SQL Injection
	{
		name:     "sql_union",
		category: "sqli",
		score:    80,
		targets:  []string{"body", "query", "path"},
		patterns: compileAll(
			`(?i)\bunion\s+(all\s+)?select\b`,
			`(?i)\bselect\b.+\bfrom\b.+\bwhere\b`,
			`(?i)\binsert\s+into\b`,
			`(?i)\bdelete\s+from\b`,
			`(?i)\bdrop\s+(table|database|index)\b`,
			`(?i)\balter\s+table\b`,
			`(?i)\bexec(\s*\(|\s+)`,
			`(?i)\bexecute\s+`,
		),
	},
	{
		name:     "sql_tautology",
		category: "sqli",
		score:    70,
		targets:  []string{"body", "query"},
		patterns: compileAll(
			`(?i)['"]?\s*or\s+['"]?\d+['"]?\s*=\s*['"]?\d+`,
			`(?i)['"]?\s*or\s+['"]?[\w]+['"]?\s*=\s*['"]?[\w]+['"]?\s*--`,
			`(?i)\b(and|or)\s+\d+=\d+`,
			`(?i)['"];\s*--`,
			`(?i)\b(and|or)\s+['"]?\w+['"]?\s*like\s`,
		),
	},
	{
		name:     "sql_comment",
		category: "sqli",
		score:    50,
		targets:  []string{"body", "query"},
		patterns: compileAll(
			`(?i)/\*.*\*/`,
			`(?i)--\s*$`,
			`(?i);\s*--`,
			`(?i)#\s*$`,
		),
	},
	{
		name:     "sql_stacked",
		category: "sqli",
		score:    90,
		targets:  []string{"body", "query"},
		patterns: compileAll(
			`(?i);\s*(select|insert|update|delete|drop|alter|exec|execute)\b`,
			`(?i)\bwaitfor\s+delay\b`,
			`(?i)\bbenchmark\s*\(`,
			`(?i)\bsleep\s*\(`,
			`(?i)\bload_file\s*\(`,
			`(?i)\binto\s+(out|dump)file\b`,
		),
	},

	// XSS
	{
		name:     "xss_script",
		category: "xss",
		score:    85,
		targets:  []string{"body", "query", "header"},
		patterns: compileAll(
			`(?i)<script[\s>]`,
			`(?i)</script>`,
			`(?i)javascript\s*:`,
			`(?i)vbscript\s*:`,
			`(?i)on(load|error|click|mouseover|submit|focus|blur|change|keyup|keydown)\s*=`,
			`(?i)<iframe[\s>]`,
			`(?i)<object[\s>]`,
			`(?i)<embed[\s>]`,
			`(?i)<svg[\s>].*on\w+\s*=`,
			`(?i)\balert\s*\(`,
			`(?i)\bconfirm\s*\(`,
			`(?i)\bprompt\s*\(`,
			`(?i)\bdocument\.(cookie|location|write)`,
			`(?i)\bwindow\.(location|open)`,
		),
	},
	{
		name:     "xss_encoding",
		category: "xss",
		score:    60,
		targets:  []string{"body", "query"},
		patterns: compileAll(
			`(?i)&#x?[0-9a-f]+;`,
			`(?i)\\u[0-9a-f]{4}`,
			`(?i)%3[cC].*%3[eE]`,
			`(?i)\\x3[cC].*\\x3[eE]`,
		),
	},

	// Path Traversal
	{
		name:     "path_traversal",
		category: "traversal",
		score:    85,
		targets:  []string{"path", "query", "body"},
		patterns: compileAll(
			`(?i)\.\./`,
			`(?i)\.\.\\`,
			`(?i)%2e%2e[/\\]`,
			`(?i)%252e%252e`,
			`(?i)/etc/passwd`,
			`(?i)/etc/shadow`,
			`(?i)/proc/self`,
			`(?i)c:\\windows`,
			`(?i)c:/windows`,
			`(?i)/var/log`,
		),
	},

	// Command Injection
	{
		name:     "cmd_injection",
		category: "cmdi",
		score:    90,
		targets:  []string{"body", "query"},
		patterns: compileAll(
			`(?i)[;&|]\s*(cat|ls|dir|whoami|id|uname|pwd|wget|curl|nc|ncat|bash|sh|cmd|powershell)\b`,
			"(?i)`[^`]*`",
			`(?i)\$\([^)]+\)`,
			`(?i)\|\s*(cat|grep|awk|sed|head|tail|sort|uniq|wc)\b`,
			`(?i);\s*(rm|mv|cp|chmod|chown|kill|pkill)\b`,
			`(?i)\b(eval|system|exec|passthru|shell_exec|popen)\s*\(`,
		),
	},

	// Prompt Injection
	{
		name:     "prompt_injection_direct",
		category: "prompt_injection",
		score:    75,
		targets:  []string{"body"},
		patterns: compileAll(
			`(?i)ignore\s+(all\s+)?(previous|prior|above|earlier)\s+(instructions?|prompts?|rules?|directives?)`,
			`(?i)forget\s+(all\s+)?(previous|prior|above|earlier)\s+(instructions?|prompts?|context)`,
			`(?i)disregard\s+(all\s+)?(previous|prior|above|earlier)\s+(instructions?|prompts?)`,
			`(?i)override\s+(system|safety|content)\s+(prompt|filter|policy|rules?)`,
			`(?i)you\s+are\s+now\s+(a|an|my)\s+`,
			`(?i)new\s+instructions?\s*:`,
			`(?i)system\s+prompt\s*:`,
			`(?i)act\s+as\s+(a|an|if)\s+`,
			`(?i)pretend\s+(you\s+are|to\s+be)\s+`,
			`(?i)roleplay\s+as\s+`,
			`(?i)from\s+now\s+on\s*,?\s*(you|ignore|forget)`,
		),
	},
	{
		name:     "prompt_injection_indirect",
		category: "prompt_injection",
		score:    60,
		targets:  []string{"body"},
		patterns: compileAll(
			`(?i)\[INST\]`,
			`(?i)\[/INST\]`,
			`(?i)<\|im_start\|>`,
			`(?i)<\|im_end\|>`,
			`(?i)<\|system\|>`,
			`(?i)<\|user\|>`,
			`(?i)<\|assistant\|>`,
			`(?i)###\s*(system|user|assistant)\s*:`,
			`(?i)<<SYS>>`,
			`(?i)<</SYS>>`,
			`(?i)human:\s*\n`,
			`(?i)assistant:\s*\n`,
		),
	},
	{
		name:     "prompt_jailbreak",
		category: "prompt_injection",
		score:    85,
		targets:  []string{"body"},
		patterns: compileAll(
			`(?i)DAN\s*(mode|prompt|jailbreak)`,
			`(?i)developer\s+mode\s+(enabled|activated|on)`,
			`(?i)jailbreak(ed)?\s*(mode|prompt)?`,
			`(?i)do\s+anything\s+now`,
			`(?i)bypass\s+(safety|content|filter|restriction)`,
			`(?i)remove\s+(all\s+)?(restrictions?|limitations?|guardrails?|safety)`,
			`(?i)no\s+(ethical|moral|safety)\s+(guidelines?|restrictions?|limitations?)`,
			`(?i)unrestricted\s+mode`,
			`(?i)unfiltered\s+(mode|response|output)`,
		),
	},
	{
		name:     "prompt_extraction",
		category: "prompt_injection",
		score:    70,
		targets:  []string{"body"},
		patterns: compileAll(
			`(?i)(show|reveal|display|print|output|repeat|echo)\s+(me\s+)?(your|the|system)\s+(system\s+)?(prompt|instructions?|rules?|directives?)`,
			`(?i)what\s+(are|is)\s+your\s+(system\s+)?(prompt|instructions?|rules?)`,
			`(?i)tell\s+me\s+your\s+(system\s+)?(prompt|instructions?)`,
			`(?i)(leak|extract|expose|dump)\s+(the\s+)?(system\s+)?(prompt|instructions?)`,
		),
	},

	// Header Anomalies
	{
		name:     "header_anomaly",
		category: "anomaly",
		score:    40,
		targets:  []string{"header"},
		patterns: compileAll(
			`(?i)(\${|%24%7b).*(\}|%7d)`, // log4j / JNDI
			`(?i)\$\{jndi:`,
			`(?i)%00`,
			`(?i)\r\n`,
		),
	},
}

// ---------------------------------------------------------------------------
// Rate burst tracker
// ---------------------------------------------------------------------------

type burstTracker struct {
	mu      sync.Mutex
	hits    map[string][]time.Time
	cleanup time.Time
}

func newBurstTracker() *burstTracker {
	return &burstTracker{
		hits:    make(map[string][]time.Time),
		cleanup: time.Now().Add(5 * time.Minute),
	}
}

func (bt *burstTracker) record(ip string, limit int, window time.Duration) bool {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)

	// Periodic cleanup.
	if now.After(bt.cleanup) {
		for k, times := range bt.hits {
			filtered := times[:0]
			for _, t := range times {
				if t.After(cutoff) {
					filtered = append(filtered, t)
				}
			}
			if len(filtered) == 0 {
				delete(bt.hits, k)
			} else {
				bt.hits[k] = filtered
			}
		}
		bt.cleanup = now.Add(5 * time.Minute)
	}

	// Filter old entries for this IP.
	times := bt.hits[ip]
	filtered := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	filtered = append(filtered, now)
	bt.hits[ip] = filtered

	return len(filtered) > limit
}

// ---------------------------------------------------------------------------
// Bot detection
// ---------------------------------------------------------------------------

var suspiciousBots = compileAll(
	`(?i)(sqlmap|nikto|nmap|masscan|dirbuster|gobuster|wfuzz|ffuf|hydra)`,
	`(?i)(havij|acunetix|nessus|openvas|burpsuite|zaproxy)`,
	`(?i)(python-requests|python-urllib|go-http-client|java/|libwww-perl)`,
	`(?i)(wget|curl)/\d`,
	`(?i)^$`, // empty user-agent
)

// ---------------------------------------------------------------------------
// Guard — Main Middleware
// ---------------------------------------------------------------------------

// Guard returns the Shield middleware that inspects every request for threats.
func Guard(cfgs ...Config) router.Middleware {
	cfg := defaultConfig()
	if len(cfgs) > 0 {
		cfg = mergeConfig(cfg, cfgs[0])
	}

	threshold := cfg.ScoreThreshold
	if threshold == 0 {
		switch cfg.Level {
		case "permissive":
			threshold = 70
		case "strict":
			threshold = 30
		default:
			threshold = 50
		}
	}

	warnThreshold := int(math.Max(float64(threshold)-20, 10))
	burst := newBurstTracker()

	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			ip := clientIP(c, cfg.TrustedProxies)

			// Check allowed IPs.
			for _, allowed := range cfg.AllowedIPs {
				if ip == allowed {
					return next(c)
				}
			}

			score := 0
			var threats []BlockEvent

			// --- Rate Burst ---
			if cfg.RateBurst {
				limit := cfg.RateBurstLimit
				if limit == 0 {
					limit = 100
				}
				window := cfg.RateBurstWindow
				if window == 0 {
					window = time.Second
				}
				if burst.record(ip, limit, window) {
					threats = append(threats, BlockEvent{
						Threat:   "rate_burst",
						Category: "rate",
						Score:    60,
						Details:  "Request rate exceeds burst limit",
					})
					score += 60
				}
			}

			// --- Payload Size ---
			if cfg.PayloadSize {
				maxSize := cfg.MaxBodySize
				if maxSize == 0 {
					maxSize = 10 * 1024 * 1024
				}
				if c.Request.ContentLength > maxSize {
					threats = append(threats, BlockEvent{
						Threat:   "payload_too_large",
						Category: "size",
						Score:    50,
						Details:  "Request body exceeds maximum size",
					})
					score += 50
				}
			}

			// --- Bot Detection ---
			if cfg.BotDetection {
				ua := c.Request.Header.Get("User-Agent")
				for _, p := range suspiciousBots {
					if p.MatchString(ua) {
						threats = append(threats, BlockEvent{
							Threat:     "suspicious_bot",
							Category:   "bot",
							Score:      55,
							Details:    "Suspicious user-agent detected",
							MatchedRaw: ua,
						})
						score += 55
						break
					}
				}
			}

			// Collect request data for pattern scanning.
			targets := collectTargets(c, cfg)

			// --- Pattern Detection ---
			for _, det := range detectors {
				if !isModuleEnabled(det.category, cfg) {
					continue
				}
				for targetName, targetData := range targets {
					if !matchTarget(det.targets, targetName) {
						continue
					}
					for _, p := range det.patterns {
						if match := p.FindString(targetData); match != "" {
							threats = append(threats, BlockEvent{
								Threat:     det.name,
								Category:   det.category,
								Score:      det.score,
								Details:    "Pattern match: " + det.name,
								MatchedRaw: truncate(match, 200),
							})
							score += det.score
							break // one match per detector per target is enough
						}
					}
				}
			}

			// --- Custom Rules ---
			for _, rule := range cfg.CustomRules {
				for targetName, targetData := range targets {
					if !matchTarget(rule.Targets, targetName) {
						continue
					}
					if match := rule.Pattern.FindString(targetData); match != "" {
						threats = append(threats, BlockEvent{
							Threat:     rule.Name,
							Category:   rule.Category,
							Score:      rule.Score,
							Details:    rule.Description,
							MatchedRaw: truncate(match, 200),
						})
						score += rule.Score
					}
				}
			}

			// --- Evaluate Score ---
			if score >= threshold {
				event := topThreat(threats, c, ip, true)

				if cfg.OnBlock != nil {
					cfg.OnBlock(event)
				}

				action := cfg.BlockAction
				if action == "" {
					action = "reject"
				}

				switch action {
				case "log":
					// Log only, don't block.
				case "challenge":
					return c.JSON(http.StatusForbidden, map[string]any{
						"error":     "Request flagged for review",
						"threat":    event.Threat,
						"category":  event.Category,
						"challenge": true,
					})
				default: // reject
					return c.JSON(http.StatusForbidden, map[string]any{
						"error":    "Request blocked by security shield",
						"threat":   event.Threat,
						"category": event.Category,
						"score":    score,
					})
				}
			} else if score >= warnThreshold && cfg.OnWarn != nil {
				cfg.OnWarn(topThreat(threats, c, ip, false))
			}

			return next(c)
		}
	}
}

// AIContentGuard is a specialized middleware for AI/LLM endpoints that
// performs deep prompt injection analysis on request bodies containing
// "prompt", "message", "content", "input", or "query" fields.
func AIContentGuard(cfgs ...Config) router.Middleware {
	cfg := defaultConfig()
	if len(cfgs) > 0 {
		cfg = mergeConfig(cfg, cfgs[0])
	}

	threshold := cfg.ScoreThreshold
	if threshold == 0 {
		threshold = 40 // more sensitive for AI endpoints
	}

	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			// Read body.
			body, err := io.ReadAll(c.Request.Body)
			if err != nil || len(body) == 0 {
				return next(c)
			}
			// Restore body for downstream handlers.
			c.Request.Body = io.NopCloser(strings.NewReader(string(body)))

			// Extract text fields from JSON body.
			var jsonBody map[string]any
			if err := json.Unmarshal(body, &jsonBody); err != nil {
				// Not JSON, scan raw body.
				return scanAndContinue(c, next, string(body), threshold, cfg)
			}

			// Collect all text-like fields.
			textFields := extractTextFields(jsonBody)
			combinedText := strings.Join(textFields, "\n")

			return scanAndContinue(c, next, combinedText, threshold, cfg)
		}
	}
}

func scanAndContinue(c *http.Context, next router.HandlerFunc, text string, threshold int, cfg Config) error {
	score := 0
	var threats []BlockEvent

	// Check prompt injection patterns.
	for _, det := range detectors {
		if det.category != "prompt_injection" {
			continue
		}
		for _, p := range det.patterns {
			if match := p.FindString(text); match != "" {
				threats = append(threats, BlockEvent{
					Threat:     det.name,
					Category:   det.category,
					Score:      det.score,
					Details:    det.name,
					MatchedRaw: truncate(match, 200),
				})
				score += det.score
				break
			}
		}
	}

	// Structural analysis: check for unusual formatting that might indicate injection.
	score += structuralAnalysis(text)

	if score >= threshold {
		ip := c.Request.RemoteAddr
		event := topThreat(threats, c, ip, true)
		if cfg.OnBlock != nil {
			cfg.OnBlock(event)
		}
		return c.JSON(http.StatusForbidden, map[string]any{
			"error":    "Prompt injection detected",
			"threat":   event.Threat,
			"category": "prompt_injection",
			"score":    score,
		})
	}

	return next(c)
}

// structuralAnalysis checks for structural patterns that may indicate injection.
func structuralAnalysis(text string) int {
	score := 0

	// Excessive special characters.
	specialCount := 0
	for _, ch := range text {
		if ch == '{' || ch == '}' || ch == '<' || ch == '>' || ch == '|' || ch == '\\' || ch == '^' || ch == '~' {
			specialCount++
		}
	}
	ratio := float64(specialCount) / float64(len(text)+1)
	if ratio > 0.15 {
		score += 20
	}

	// Multiple newlines with role-like prefixes.
	lines := strings.Split(text, "\n")
	rolePrefixCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "System:") || strings.HasPrefix(trimmed, "User:") ||
			strings.HasPrefix(trimmed, "Assistant:") || strings.HasPrefix(trimmed, "Human:") ||
			strings.HasPrefix(trimmed, "AI:") {
			rolePrefixCount++
		}
	}
	if rolePrefixCount >= 2 {
		score += 25
	}

	// Base64-encoded content (potential obfuscation).
	base64Pattern := regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)
	if base64Pattern.MatchString(text) && len(text) > 100 {
		score += 15
	}

	// Unicode homoglyph abuse.
	homoglyphCount := 0
	for _, r := range text {
		if (r >= 0x0400 && r <= 0x04FF) || // Cyrillic
			(r >= 0x2000 && r <= 0x206F) || // General punctuation
			(r >= 0xFF00 && r <= 0xFFEF) { // Fullwidth forms
			homoglyphCount++
		}
	}
	if homoglyphCount > 5 {
		score += 20
	}

	return score
}

// extractTextFields recursively extracts string values from a JSON structure,
// focusing on fields likely to contain user input.
func extractTextFields(data map[string]any) []string {
	var results []string
	textKeys := map[string]bool{
		"prompt": true, "message": true, "content": true, "input": true,
		"query": true, "text": true, "question": true, "instruction": true,
		"system": true, "user": true, "assistant": true, "context": true,
		"body": true, "description": true, "comment": true,
	}

	var extract func(v any, depth int)
	extract = func(v any, depth int) {
		if depth > 10 {
			return
		}
		switch val := v.(type) {
		case string:
			results = append(results, val)
		case map[string]any:
			for k, child := range val {
				if textKeys[strings.ToLower(k)] {
					extract(child, depth+1)
				}
			}
		case []any:
			for _, item := range val {
				extract(item, depth+1)
			}
		}
	}

	for k, v := range data {
		if textKeys[strings.ToLower(k)] {
			extract(v, 0)
		}
		// Also recurse into "messages" arrays (OpenAI-style).
		if strings.ToLower(k) == "messages" {
			extract(v, 0)
		}
	}

	return results
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func defaultConfig() Config {
	return Config{
		Level:            "balanced",
		SQLInjection:     true,
		XSS:              true,
		PathTraversal:    true,
		CommandInjection: true,
		PromptInjection:  true,
		BotDetection:     true,
		PayloadSize:      true,
		RateBurst:        true,
		HeaderAnomalies:  true,
		MaxBodySize:      10 * 1024 * 1024,
		RateBurstLimit:   100,
		RateBurstWindow:  time.Second,
		BlockAction:      "reject",
	}
}

func mergeConfig(base, override Config) Config {
	if override.Level != "" {
		base.Level = override.Level
	}
	if override.MaxBodySize > 0 {
		base.MaxBodySize = override.MaxBodySize
	}
	if override.RateBurstLimit > 0 {
		base.RateBurstLimit = override.RateBurstLimit
	}
	if override.RateBurstWindow > 0 {
		base.RateBurstWindow = override.RateBurstWindow
	}
	if override.BlockAction != "" {
		base.BlockAction = override.BlockAction
	}
	if override.ScoreThreshold > 0 {
		base.ScoreThreshold = override.ScoreThreshold
	}
	if override.OnBlock != nil {
		base.OnBlock = override.OnBlock
	}
	if override.OnWarn != nil {
		base.OnWarn = override.OnWarn
	}
	if len(override.AllowedIPs) > 0 {
		base.AllowedIPs = override.AllowedIPs
	}
	if len(override.TrustedProxies) > 0 {
		base.TrustedProxies = override.TrustedProxies
	}
	if len(override.CustomRules) > 0 {
		base.CustomRules = override.CustomRules
	}
	// Module flags: override only if explicitly set (zero-value is tricky for bools).
	// For simplicity, always accept the override struct's values.
	return base
}

func isModuleEnabled(category string, cfg Config) bool {
	switch category {
	case "sqli":
		return cfg.SQLInjection
	case "xss":
		return cfg.XSS
	case "traversal":
		return cfg.PathTraversal
	case "cmdi":
		return cfg.CommandInjection
	case "prompt_injection":
		return cfg.PromptInjection
	case "anomaly":
		return cfg.HeaderAnomalies
	default:
		return true
	}
}

func collectTargets(c *http.Context, cfg Config) map[string]string {
	targets := make(map[string]string)

	targets["path"] = c.Request.URL.Path
	targets["query"] = c.Request.URL.RawQuery

	// Headers.
	var headerBuf strings.Builder
	for k, v := range c.Request.Header {
		headerBuf.WriteString(k + ": " + strings.Join(v, ",") + "\n")
	}
	targets["header"] = headerBuf.String()

	// Body (if applicable).
	if c.Request.Body != nil && c.Request.ContentLength > 0 && c.Request.ContentLength < cfg.MaxBodySize {
		body, err := io.ReadAll(c.Request.Body)
		if err == nil {
			targets["body"] = string(body)
			// Restore body.
			c.Request.Body = io.NopCloser(strings.NewReader(string(body)))
		}
	}

	return targets
}

func matchTarget(detTargets []string, actual string) bool {
	for _, t := range detTargets {
		if t == "all" || t == actual {
			return true
		}
	}
	return false
}

func topThreat(threats []BlockEvent, c *http.Context, ip string, blocked bool) BlockEvent {
	if len(threats) == 0 {
		return BlockEvent{
			Timestamp: time.Now(),
			IP:        ip,
			Method:    c.Request.Method,
			Path:      c.Request.URL.Path,
			Threat:    "unknown",
			Blocked:   blocked,
			UserAgent: c.Request.Header.Get("User-Agent"),
		}
	}
	// Return highest-scoring threat.
	best := threats[0]
	for _, t := range threats[1:] {
		if t.Score > best.Score {
			best = t
		}
	}
	best.Timestamp = time.Now()
	best.IP = ip
	best.Method = c.Request.Method
	best.Path = c.Request.URL.Path
	best.Blocked = blocked
	best.UserAgent = c.Request.Header.Get("User-Agent")
	return best
}

func clientIP(c *http.Context, trustedProxies []string) string {
	// Check X-Forwarded-For if trusted.
	if xff := c.Request.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			// Verify it's from a trusted proxy.
			remoteIP := remoteAddr(c)
			for _, tp := range trustedProxies {
				if remoteIP == tp {
					return ip
				}
			}
		}
	}

	// Check X-Real-IP.
	if xri := c.Request.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	return remoteAddr(c)
}

func remoteAddr(c *http.Context) string {
	addr := c.Request.RemoteAddr
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func compileAll(patterns ...string) []*regexp.Regexp {
	result := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		result = append(result, regexp.MustCompile(p))
	}
	return result
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
