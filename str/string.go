package str

import (
	"regexp"
	"strings"
	"unicode"
)

// NimbusString is a fluent, chainable string wrapper.
type NimbusString struct {
	val string
}

// Str is the entry point for fluent string manipulation.
func Str(s string) NimbusString {
	return NimbusString{val: s}
}

// =========================================================================
// Chainable Methods
// =========================================================================

func (s NimbusString) Append(suffix string) NimbusString {
	return Str(s.val + suffix)
}

func (s NimbusString) Prepend(prefix string) NimbusString {
	return Str(prefix + s.val)
}

func (s NimbusString) Upper() NimbusString {
	return Str(strings.ToUpper(s.val))
}

func (s NimbusString) Lower() NimbusString {
	return Str(strings.ToLower(s.val))
}

func (s NimbusString) Title() NimbusString {
	return Str(strings.Title(strings.ToLower(s.val))) // Note: strings.Title is deprecated in newer Go, but acceptable for simple use
}

// Helper to convert to a slice of words
func extractWords(str string) []string {
	re := regexp.MustCompile(`[a-zA-Z0-9]+`)
	return re.FindAllString(str, -1)
}

func (s NimbusString) Pascal() NimbusString {
	words := extractWords(s.val)
	for i, w := range words {
		words[i] = strings.Title(strings.ToLower(w))
	}
	return Str(strings.Join(words, ""))
}

func (s NimbusString) Camel() NimbusString {
	pascal := s.Pascal().val
	if len(pascal) == 0 {
		return Str("")
	}
	runes := []rune(pascal)
	runes[0] = unicode.ToLower(runes[0])
	return Str(string(runes))
}

func (s NimbusString) Snake() NimbusString {
	return s.Slug("_")
}

func (s NimbusString) Kebab() NimbusString {
	return s.Slug("-")
}

func (s NimbusString) Slug(separator ...string) NimbusString {
	sep := "-"
	if len(separator) > 0 {
		sep = separator[0]
	}
	words := extractWords(strings.ToLower(s.val))
	return Str(strings.Join(words, sep))
}

func (s NimbusString) Trim() NimbusString {
	return Str(strings.TrimSpace(s.val))
}

func (s NimbusString) TrimLeft() NimbusString {
	return Str(strings.TrimLeftFunc(s.val, unicode.IsSpace))
}

func (s NimbusString) TrimRight() NimbusString {
	return Str(strings.TrimRightFunc(s.val, unicode.IsSpace))
}

func (s NimbusString) Replace(old, new string) NimbusString {
	return Str(strings.Replace(s.val, old, new, 1))
}

func (s NimbusString) ReplaceAll(old, new string) NimbusString {
	return Str(strings.ReplaceAll(s.val, old, new))
}

func (s NimbusString) Limit(n int) NimbusString {
	runes := []rune(s.val)
	if len(runes) <= n {
		return s
	}
	return Str(string(runes[:n]) + "...")
}

func (s NimbusString) Words(n int) NimbusString {
	words := strings.Fields(s.val)
	if len(words) <= n {
		return s
	}
	return Str(strings.Join(words[:n], " ") + "...")
}

// PadRight adds padding to the right
func (s NimbusString) PadRight(length int, pad string) NimbusString {
	runes := []rune(s.val)
	padRunes := []rune(pad)
	if len(runes) >= length || len(padRunes) == 0 {
		return s
	}
	for len(runes) < length {
		runes = append(runes, padRunes...)
	}
	return Str(string(runes[:length]))
}

// PadLeft adds padding to the left
func (s NimbusString) PadLeft(length int, pad string) NimbusString {
	runes := []rune(s.val)
	padRunes := []rune(pad)
	if len(runes) >= length || len(padRunes) == 0 {
		return s
	}
	needed := length - len(runes)
	prefix := []rune{}
	for len(prefix) < needed {
		prefix = append(prefix, padRunes...)
	}
	return Str(string(prefix[:needed]) + s.val)
}

func (s NimbusString) Pad(length int, pad string) NimbusString {
	runes := []rune(s.val)
	if len(runes) >= length {
		return s
	}
	leftPadding := (length - len(runes)) / 2
	return s.PadLeft(len(runes)+leftPadding, pad).PadRight(length, pad)
}

func (s NimbusString) Repeat(n int) NimbusString {
	return Str(strings.Repeat(s.val, n))
}

func (s NimbusString) Reverse() NimbusString {
	runes := []rune(s.val)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return Str(string(runes))
}

func (s NimbusString) Mask(char string, start, length int) NimbusString {
	runes := []rune(s.val)
	if start < 0 || start >= len(runes) || length <= 0 {
		return s
	}
	end := start + length
	if end > len(runes) {
		end = len(runes)
	}
	maskChar := []rune(char)[0]
	for i := start; i < end; i++ {
		runes[i] = maskChar
	}
	return Str(string(runes))
}

func (s NimbusString) Excerpt(phrase string, radius int) NimbusString {
	idx := strings.Index(strings.ToLower(s.val), strings.ToLower(phrase))
	if idx == -1 {
		return s
	}
	runes := []rune(s.val)
	start := len([]rune(s.val[:idx])) - radius
	if start < 0 {
		start = 0
	}
	end := len([]rune(s.val[:idx])) + len([]rune(phrase)) + radius
	if end > len(runes) {
		end = len(runes)
	}

	prefix, suffix := "", ""
	if start > 0 {
		prefix = "..."
	}
	if end < len(runes) {
		suffix = "..."
	}

	return Str(prefix + string(runes[start:end]) + suffix)
}

// =========================================================================
// Terminal Methods
// =========================================================================

func (s NimbusString) Contains(sub string) bool {
	return strings.Contains(s.val, sub)
}

func (s NimbusString) StartsWith(prefix string) bool {
	return strings.HasPrefix(s.val, prefix)
}

func (s NimbusString) EndsWith(suffix string) bool {
	return strings.HasSuffix(s.val, suffix)
}

func (s NimbusString) WordCount() int {
	return len(strings.Fields(s.val))
}

func (s NimbusString) Length() int {
	return len([]rune(s.val))
}

func (s NimbusString) IsEmpty() bool {
	return len(s.val) == 0
}

func (s NimbusString) IsNotEmpty() bool {
	return len(s.val) > 0
}

func (s NimbusString) Split(sep string) []string {
	return strings.Split(s.val, sep)
}

func (s NimbusString) String() string {
	return s.val
}
