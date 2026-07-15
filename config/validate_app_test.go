package config

import "testing"

func TestConfigValidate(t *testing.T) {
	long := "0123456789abcdef0123456789abcdef" // 32 chars

	tests := []struct {
		name      string
		env       string
		key       string
		wantErr   bool
		wantWarns int
	}{
		{name: "prod missing key fails", env: "production", key: "", wantErr: true},
		{name: "prod short key fails", env: "production", key: "short", wantErr: true},
		{name: "prod good key ok", env: "production", key: long, wantErr: false},
		{name: "prod alias good key ok", env: "prod", key: long, wantErr: false},
		{name: "dev missing key warns", env: "development", key: "", wantErr: false, wantWarns: 1},
		{name: "dev short key warns", env: "development", key: "short", wantErr: false, wantWarns: 1},
		{name: "dev good key clean", env: "development", key: long, wantErr: false, wantWarns: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{App: AppConfig{Env: tt.env, Key: tt.key}}
			warns, err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.wantErr && len(warns) != tt.wantWarns {
				t.Fatalf("expected %d warnings, got %d: %v", tt.wantWarns, len(warns), warns)
			}
		})
	}
}
