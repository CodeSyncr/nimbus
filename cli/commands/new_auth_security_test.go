package commands

import (
	"github.com/CodeSyncr/nimbus/auth"
	"strings"
	"testing"
)

func TestGeneratedAuthSecrets(t *testing.T) {
	for _, guard := range []string{"session", "stateless"} {
		a, b := authEnvVars(guard), authEnvVars(guard)
		for i, line := range a {
			if !strings.Contains(line, "SECRET=") {
				continue
			}
			_, secret, _ := strings.Cut(line, "=")
			if err := auth.ValidateTokenSecret(secret); err != nil {
				t.Fatal(err)
			}
			if line == b[i] {
				t.Fatal("secrets reused across scaffolds")
			}
		}
	}
	if strings.Contains(authConfigContent("stateless"), "please-change") {
		t.Fatal("placeholder fallback")
	}
}
