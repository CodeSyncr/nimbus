package commands

const reverbConfigFile = `/*
|--------------------------------------------------------------------------
| Reverb (WebSocket broadcasting)
|--------------------------------------------------------------------------
|
| See: github.com/CodeSyncr/nimbus/plugins/reverb/README.md
|
*/

package config

var Reverb ReverbConfig

type ReverbConfig struct {
	Path              string
	AllowedOriginsCSV string
	RedisChannel      string
}

func loadReverb() {
	Reverb = ReverbConfig{
		Path:              env("REVERB_PATH", "/reverb/ws"),
		AllowedOriginsCSV: env("REVERB_ALLOWED_ORIGINS", ""),
		RedisChannel:      env("REVERB_REDIS_CHANNEL", ""),
	}
}
`
