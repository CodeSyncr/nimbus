package database

import (
	"encoding/json"
	"reflect"
)

// SerializeOptions configures which fields to include when serializing.
type SerializeOptions struct {
	// Omit excludes these field names from output (e.g. "password", "remember_token").
	Omit []string
	// Pick includes only these fields (if non-empty).
	Pick []string
}

// Serialize converts a model to a map, applying omit/pick.
// Use for API responses to exclude sensitive fields.
func Serialize(v any, opts SerializeOptions) (map[string]any, error) {
	data, err := structToMap(v)
	if err != nil {
		return nil, err
	}
	omitSet := make(map[string]bool)
	for _, k := range opts.Omit {
		omitSet[k] = true
	}
	pickSet := make(map[string]bool)
	for _, k := range opts.Pick {
		pickSet[k] = true
	}

	result := make(map[string]any)
	for k, val := range data {
		if omitSet[k] {
			continue
		}
		if len(pickSet) > 0 && !pickSet[k] {
			continue
		}
		result[k] = val
	}
	return result, nil
}

func structToMap(v any) (map[string]any, error) {
	// Use JSON round-trip for simplicity (handles nested structs, time, etc.)
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// SerializeAsNull returns a struct tag value to omit a field from JSON.
// In Go, use: `json:"-"` to omit. This helper documents the pattern.
const SerializeAsNull = "-"

// IsDirty checks if a struct field was modified (for hooks).
// GORM tracks this via Statement.Changed(). This is a helper for custom logic.
func IsDirty(v any, field string) bool {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return false
	}
	f := rv.FieldByName(field)
	return f.IsValid() && !f.IsZero()
}
