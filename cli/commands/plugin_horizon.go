package commands

const horizonConfigFile = `/*
|--------------------------------------------------------------------------
| Horizon Configuration
|--------------------------------------------------------------------------
|
| Horizon provides a dashboard and configuration layer for your
| queue workers. It supports supervisor configuration per
| environment with auto-scaling, retry policies, and monitoring.
|
| See: /docs/horizon
|
*/

package config

var Horizon HorizonConfig

type HorizonConfig struct {
	// Path is the URL prefix for the Horizon dashboard.
	Path string

	// RedisURL enables failed job persistence in Redis.
	// Leave empty to skip Redis-backed failure storage.
	RedisURL string

	// Defaults apply to all supervisors unless overridden.
	Defaults HorizonDefaults

	// Environments maps environment names (e.g. "local",
	// "production") to their supervisor configurations.
	Environments map[string]HorizonEnvironmentConfig

	// Waits maps "connection:queue" to the maximum number of
	// seconds Horizon will wait for jobs before recycling.
	Waits map[string]int

	// Silenced is a list of job class names to hide from the
	// dashboard (e.g. heartbeat or cleanup jobs).
	Silenced []string
}

type HorizonDefaults struct {
	Connection string
	Timeout    int
	Tries      int
	Backoff    []int
}

type HorizonEnvironmentConfig struct {
	Supervisors map[string]HorizonSupervisorConfig
}

type HorizonSupervisorConfig struct {
	Connection      string
	Queue           []string
	Balance         string
	Processes       int
	MinProcesses    int
	MaxProcesses    int
	BalanceMaxShift int
	BalanceCooldown int
	Tries           int
	Timeout         int
	Backoff         []int
	Force           bool
}

func loadHorizon() {
	Horizon = HorizonConfig{
		Path:     env("HORIZON_PATH", "/horizon"),
		RedisURL: env("REDIS_URL", ""),

		Defaults: HorizonDefaults{
			Connection: "redis",
			Timeout:    60,
			Tries:      3,
			Backoff:    []int{1, 5, 10},
		},

		Environments: map[string]HorizonEnvironmentConfig{
			"local": {
				Supervisors: map[string]HorizonSupervisorConfig{
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
				Supervisors: map[string]HorizonSupervisorConfig{
					"supervisor-1": {
						Connection:      "redis",
						Queue:           []string{"default", "high", "low"},
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

		Waits: map[string]int{
			"redis:default": 60,
		},

		Silenced: []string{},
	}
}
`
