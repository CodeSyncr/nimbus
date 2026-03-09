package unpoly

// DefaultConfig returns the default configuration for the Unpoly plugin.
func (p *Plugin) DefaultConfig() map[string]any {
	return map[string]any{
		"enabled": true,
		"cdn":     "https://unpoly.com",
		"version": "3.12.0",
	}
}
