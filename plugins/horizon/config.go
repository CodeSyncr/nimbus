/*
|--------------------------------------------------------------------------
| Horizon Configuration (Laravel Horizon 1:1 style)
|--------------------------------------------------------------------------
|
| Code-driven configuration for queue workers: environments, supervisors,
| balance strategy, tries, timeout, backoff. Use when running horizon
| (workers) so Nimbus starts the right number of workers per queue.
|
*/

package horizon

import (
	"os"
	"time"
)

// Config is the top-level Horizon configuration.
type Config struct {
	// Environments holds per-environment supervisor sets (e.g. "production", "local").
	Environments map[string]EnvironmentConfig
	// Defaults are merged into each supervisor.
	Defaults SupervisorDefaults
	// Waits is per-queue wait threshold in seconds for long-wait notifications (e.g. "redis:default" => 60).
	Waits map[string]int
	// Silenced job class names to hide from completed list.
	Silenced []string
	// SilencedTags hide jobs with any of these tags.
	SilencedTags []string
}

// EnvironmentConfig holds supervisors for one environment (e.g. APP_ENV=production).
type EnvironmentConfig struct {
	Supervisors map[string]SupervisorConfig
}

// SupervisorConfig defines a group of workers (Laravel "supervisor").
type SupervisorConfig struct {
	// Connection name (e.g. "redis").
	Connection string
	// Queue names this supervisor processes.
	Queue []string
	// Balance: "auto", "simple", or "false" (strict order).
	Balance string
	// Auto/simple: total process count. Simple splits evenly across queues.
	Processes int
	// Auto: min processes per queue.
	MinProcesses int
	// Auto/false: max total processes.
	MaxProcesses int
	// Auto: max processes to add/remove per balance step.
	BalanceMaxShift int
	// Auto: seconds between balance steps.
	BalanceCooldown int
	// Max job attempts (0 = unlimited). Job $tries overrides if set.
	Tries int
	// Job timeout in seconds (force kill worker).
	Timeout int
	// Backoff in seconds before retry, or nil for default.
	Backoff []int
	// Force run in maintenance mode when true.
	Force bool
}

// SupervisorDefaults are merged into each supervisor.
type SupervisorDefaults struct {
	Connection string
	Timeout    int
	Tries      int
	Backoff    []int
}

// DefaultConfig returns a Laravel-like default for local development.
func DefaultConfig() Config {
	return Config{
		Environments: map[string]EnvironmentConfig{
			"local": {
				Supervisors: map[string]SupervisorConfig{
					"supervisor-1": {
						Connection: "redis",
						Queue:      []string{"default"},
						Balance:    "simple",
						Processes:  3,
						Tries:      3,
						Timeout:    60,
					},
				},
			},
			"production": {
				Supervisors: map[string]SupervisorConfig{
					"supervisor-1": {
						Connection:      "redis",
						Queue:           []string{"default"},
						Balance:         "auto",
						MinProcesses:    1,
						MaxProcesses:    10,
						BalanceMaxShift: 1,
						BalanceCooldown: 3,
						Tries:           10,
						Timeout:         60,
						Backoff:         []int{1, 5, 10},
					},
				},
			},
		},
		Defaults: SupervisorDefaults{
			Connection: "redis",
			Timeout:    60,
			Tries:      3,
		},
		Waits: map[string]int{
			"redis:default": 60,
		},
	}
}

// CurrentEnvironment returns the environment name (e.g. from APP_ENV).
func (c *Config) CurrentEnvironment() string {
	if env := os.Getenv("APP_ENV"); env != "" {
		return env
	}
	return "local"
}

// SupervisorsForCurrentEnv returns supervisor configs for the current environment.
// If no match, tries "*" then "local".
func (c *Config) SupervisorsForCurrentEnv() map[string]SupervisorConfig {
	env := c.CurrentEnvironment()
	if e, ok := c.Environments[env]; ok {
		return e.Supervisors
	}
	if e, ok := c.Environments["*"]; ok {
		return e.Supervisors
	}
	if e, ok := c.Environments["local"]; ok {
		return e.Supervisors
	}
	return nil
}

// MergeSupervisor merges Defaults into a supervisor config.
func (c *Config) MergeSupervisor(s SupervisorConfig) SupervisorConfig {
	if c.Defaults.Connection != "" && s.Connection == "" {
		s.Connection = c.Defaults.Connection
	}
	if c.Defaults.Timeout > 0 && s.Timeout == 0 {
		s.Timeout = c.Defaults.Timeout
	}
	if c.Defaults.Tries > 0 && s.Tries == 0 {
		s.Tries = c.Defaults.Tries
	}
	if len(c.Defaults.Backoff) > 0 && len(s.Backoff) == 0 {
		s.Backoff = c.Defaults.Backoff
	}
	return s
}

// BackoffDuration returns the delay before retry for the given attempt (1-based).
func (s *SupervisorConfig) BackoffDuration(attempt int) time.Duration {
	if len(s.Backoff) == 0 {
		return time.Second
	}
	idx := attempt - 1
	if idx >= len(s.Backoff) {
		idx = len(s.Backoff) - 1
	}
	return time.Duration(s.Backoff[idx]) * time.Second
}
