package commands

const transmitConfigFile = `/*
|--------------------------------------------------------------------------
| Transmit Configuration
|--------------------------------------------------------------------------
|
| Transmit is the Server-Sent Events (SSE) transport layer for
| real-time communication. It lets the server push updates to
| connected clients without WebSockets.
|
| Transport: set to "redis" in production so broadcasts reach
| clients connected to any server instance.
|
| See: /docs/transmit
|
*/

package config

var Transmit TransmitConfig

type TransmitConfig struct {
	// Path is the URL prefix for Transmit SSE endpoints.
	// Default: "__transmit"
	Path string

	// PingInterval controls how often a keep-alive ":ping"
	// comment is sent to clients. Set to "" to disable.
	// Examples: "15s", "30s", "1m"
	PingInterval string

	// Transport controls how broadcasts are distributed across
	// multiple server instances.
	// Values: "" (single instance), "redis" (multi-instance)
	Transport string

	// Redis transport configuration
	Redis TransmitRedisConfig
}

type TransmitRedisConfig struct {
	// URL is the Redis connection string.
	URL string

	// Channel is the Redis Pub/Sub channel for broadcasts.
	// Default: "transmit::broadcast"
	Channel string
}

func loadTransmit() {
	Transmit = TransmitConfig{
		Path:         env("TRANSMIT_PATH", "__transmit"),
		PingInterval: env("TRANSMIT_PING_INTERVAL", "30s"),
		Transport:    env("TRANSMIT_TRANSPORT", ""),

		Redis: TransmitRedisConfig{
			URL:     env("REDIS_URL", ""),
			Channel: env("TRANSMIT_REDIS_CHANNEL", "transmit::broadcast"),
		},
	}
}
`
