package config

import (
	"reflect"
	"strconv"
	"strings"
)

// Get returns a config value by dot-notation key. Type-safe via generic.
// Returns zero value and false if key not found or conversion fails.
//
//	config.Get[string]("app.name")
//	config.Get[int]("server.port")
//	config.Get[bool]("app.debug")
func Get[T any](key string) (T, bool) {
	var zero T
	v, ok := getByPath(store, key)
	if !ok {
		return zero, false
	}
	return convert[T](v)
}

// Must returns the value or panics if not found. Use when key is required.
func Must[T any](key string) T {
	v, ok := Get[T](key)
	if !ok {
		panic("config: missing required key " + key)
	}
	return v
}

// GetOrDefault returns the value or default if not found.
func GetOrDefault[T any](key string, defaultVal T) T {
	v, ok := Get[T](key)
	if !ok {
		return defaultVal
	}
	return v
}

func convert[T any](v any) (T, bool) {
	var zero T
	s := valueToString(v)
	s = strings.TrimSpace(s)

	val := reflect.ValueOf(&zero).Elem()
	switch val.Kind() {
	case reflect.String:
		val.SetString(s)
		return zero, true
	case reflect.Int, reflect.Int32:
		n, err := strconv.Atoi(s)
		if err != nil {
			return zero, false
		}
		val.SetInt(int64(n))
		return zero, true
	case reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return zero, false
		}
		val.SetInt(n)
		return zero, true
	case reflect.Float64:
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return zero, false
		}
		val.SetFloat(n)
		return zero, true
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return zero, false
		}
		val.SetBool(b)
		return zero, true
	default:
		return zero, false
	}
}

func valueToString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	default:
		return ""
	}
}
