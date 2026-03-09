package config

import (
	"os"
	"reflect"
	"strconv"
	"strings"
)

// LoadInto fills dest from the config store. Use struct tags:
//
//	config:"app.name"   - dot-notation key
//	env:"APP_NAME"      - env var (overrides config if set)
//	default:"value"     - fallback when missing
//
// Example:
//
//	type AppConfig struct {
//	    Port int    `config:"server.port" env:"PORT" default:"3333"`
//	    Env  string `config:"app.env" env:"APP_ENV" default:"development"`
//	    Name string `config:"app.name" env:"APP_NAME" default:"nimbus"`
//	}
//	var cfg AppConfig
//	config.LoadInto(&cfg)
func LoadInto(dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return nil
	}
	v = v.Elem()
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		sf := t.Field(i)
		if !field.CanSet() {
			continue
		}
		configKey := sf.Tag.Get("config")
		envKey := sf.Tag.Get("env")
		defaultVal := sf.Tag.Get("default")
		if configKey == "" && envKey == "" {
			continue
		}
		key := configKey
		if envKey != "" {
			if ev := os.Getenv(envKey); ev != "" {
				setField(field, ev)
				continue
			}
		}
		if key == "" {
			key = envToKey(envKey)
		}
		if val, ok := getByPath(store, key); ok {
			setFieldFromAny(field, val)
			continue
		}
		if defaultVal != "" {
			setField(field, defaultVal)
		}
	}
	return nil
}

func envToKey(env string) string {
	s := strings.ToLower(env)
	return strings.ReplaceAll(s, "_", ".")
}

func setField(f reflect.Value, s string) {
	s = strings.TrimSpace(s)
	switch f.Kind() {
	case reflect.String:
		f.SetString(s)
	case reflect.Int, reflect.Int32, reflect.Int64:
		n, _ := strconv.ParseInt(s, 10, 64)
		f.SetInt(n)
	case reflect.Float64:
		n, _ := strconv.ParseFloat(s, 64)
		f.SetFloat(n)
	case reflect.Bool:
		b, _ := strconv.ParseBool(s)
		f.SetBool(b)
	}
}

func setFieldFromAny(f reflect.Value, v any) {
	if v == nil {
		return
	}
	switch x := v.(type) {
	case string:
		setField(f, x)
	case int:
		setField(f, strconv.Itoa(x))
	case int64:
		setField(f, strconv.FormatInt(x, 10))
	case float64:
		setField(f, strconv.FormatFloat(x, 'f', -1, 64))
	case bool:
		setField(f, strconv.FormatBool(x))
	}
}
