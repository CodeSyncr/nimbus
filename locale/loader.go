package locale

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadJSONFile loads a single JSON file of translations into the given locale.
// The file must be a JSON object with string values (nested objects are
// flattened with dot keys, e.g. {"auth": {"failed": "x"}} → "auth.failed").
func LoadJSONFile(locale, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("locale: read %s: %w", path, err)
	}
	return LoadJSONBytes(locale, b)
}

// LoadJSONBytes merges JSON translation data into locale.
func LoadJSONBytes(locale string, data []byte) error {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("locale: invalid JSON: %w", err)
	}
	flat := make(map[string]string)
	flattenJSON("", root, flat)
	if len(flat) == 0 {
		return nil
	}
	AddTranslations(locale, flat)
	return nil
}

func flattenJSON(prefix string, m map[string]any, out map[string]string) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch t := v.(type) {
		case string:
			out[key] = t
		case map[string]any:
			flattenJSON(key, t, out)
		default:
			// numbers, bools → string for sprintf-style messages
			out[key] = fmt.Sprint(t)
		}
	}
}

// LoadDirectory walks dir for locale files. Supported layout:
//
//   lang/en.json          → locale "en"
//   lang/en/messages.json → locale "en" (merged)
//   resources/lang/en.json (same)
//
// Only files named *.json are loaded; subdirectories named with a locale tag
// (two+ letters) load all JSON inside them into that locale.
func LoadDirectory(dir string) error {
	st, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("locale: %s is not a directory", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			loc := e.Name()
			if !isLocaleDirName(loc) {
				continue
			}
			sub := filepath.Join(dir, loc)
			_ = filepath.WalkDir(sub, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				if strings.HasSuffix(strings.ToLower(path), ".json") {
					_ = LoadJSONFile(loc, path)
				}
				return nil
			})
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		base := strings.TrimSuffix(name, filepath.Ext(name))
		loc := normalizeLocaleTag(base)
		if loc == "" {
			continue
		}
		_ = LoadJSONFile(loc, filepath.Join(dir, name))
	}
	return nil
}

func isLocaleDirName(s string) bool {
	if len(s) < 2 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '-' {
			return false
		}
	}
	return true
}

func normalizeLocaleTag(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "_", "-")
	return strings.ToLower(s)
}

// BootFromEnv loads translations from disk and applies APP_LOCALE default.
// Directories: LANG_PATH if set (single dir), else "lang" and "resources/lang";
// then RESOURCES_LANG_PATH if set. Missing directories are ignored.
func BootFromEnv() {
	if p := strings.TrimSpace(os.Getenv("LANG_PATH")); p != "" {
		_ = LoadDirectory(p)
	} else {
		_ = LoadDirectory("lang")
		_ = LoadDirectory("resources/lang")
	}
	if p := strings.TrimSpace(os.Getenv("RESOURCES_LANG_PATH")); p != "" {
		_ = LoadDirectory(p)
	}
	if d := strings.TrimSpace(os.Getenv("APP_LOCALE")); d != "" {
		SetDefault(normalizeLocaleTag(d))
	}
}
