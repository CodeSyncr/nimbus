package cli

import (
	"strings"
	"time"
)

// ToSnake converts a string to snake_case.
func ToSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ToPascal converts a string from snake_case to PascalCase.
func ToPascal(snake string) string {
	var b strings.Builder
	up := true
	for _, r := range snake {
		if r == '_' {
			up = true
			continue
		}
		if up && r >= 'a' && r <= 'z' {
			r = r - ('a' - 'A')
			up = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Timestamp returns a standard migration-style timestamp format.
func Timestamp() string {
	return time.Now().Format("20060102150405")
}
