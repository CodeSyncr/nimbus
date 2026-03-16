package config

import (
	"fmt"
	"os"
	"strings"
)

// EnvRule describes a validation rule for an environment variable.
type EnvRule struct {
	Key      string   // environment variable name
	Required bool     // must be set and non-empty
	OneOf    []string // if set, value must be one of these
	Default  string   // default value to set if missing (implies not required unless Required is true)
}

// ValidateEnv checks the environment against a set of rules.
// Returns an error listing all violations. If no violations exist, returns nil.
//
// Usage:
//
//	err := config.ValidateEnv(
//	    config.EnvRule{Key: "APP_KEY", Required: true},
//	    config.EnvRule{Key: "APP_ENV", OneOf: []string{"development", "staging", "production"}, Default: "development"},
//	    config.EnvRule{Key: "DB_DRIVER", Required: true, OneOf: []string{"sqlite", "postgres", "mysql"}},
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
func ValidateEnv(rules ...EnvRule) error {
	var errs []string

	for _, r := range rules {
		val := os.Getenv(r.Key)

		// Apply default if missing.
		if val == "" && r.Default != "" {
			os.Setenv(r.Key, r.Default)
			val = r.Default
		}

		// Check required.
		if r.Required && val == "" {
			errs = append(errs, fmt.Sprintf("  - %s is required but not set", r.Key))
			continue
		}

		// Check OneOf.
		if val != "" && len(r.OneOf) > 0 {
			found := false
			for _, allowed := range r.OneOf {
				if val == allowed {
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, fmt.Sprintf("  - %s=%q is not one of [%s]", r.Key, val, strings.Join(r.OneOf, ", ")))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("environment validation failed:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

// Required is a shorthand for validating that one or more env vars are set.
//
//	config.Required("APP_KEY", "DB_DSN")
func Required(keys ...string) error {
	rules := make([]EnvRule, len(keys))
	for i, k := range keys {
		rules[i] = EnvRule{Key: k, Required: true}
	}
	return ValidateEnv(rules...)
}
