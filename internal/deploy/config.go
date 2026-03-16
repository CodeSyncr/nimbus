package deploy

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds deploy configuration. Loaded from deploy.yaml.
type Config struct {
	Target     string         `yaml:"target"`     // fly, railway, aws, kubernetes
	AppName    string         `yaml:"app_name"`   // app name on platform
	Service    string         `yaml:"service"`    // Railway service name (defaults to app_name)
	Region     string         `yaml:"region"`     // primary region (fly, aws)
	Secrets    []string       `yaml:"secrets"`    // env var names to set as secrets (from .env)
	Migrations *bool          `yaml:"migrations"` // run migrations on deploy (default true)
	Workers    []WorkerConfig `yaml:"workers"`    // worker processes (e.g. queue:work)
}

type WorkerConfig struct {
	Command string `yaml:"command"` // e.g. "queue:work"
	Scale   int    `yaml:"scale"`   // instance count (0 = same as app)
}

// Load reads deploy.yaml from dir. Returns nil if file doesn't exist.
func LoadConfig(dir string) (*Config, error) {
	path := filepath.Join(dir, "deploy.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
