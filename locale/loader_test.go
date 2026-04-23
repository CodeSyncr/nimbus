package locale

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadJSONBytesFlatten(t *testing.T) {
	t.Parallel()
	mu.Lock()
	translations = make(map[string]map[string]string)
	defaultLocale = "en"
	mu.Unlock()

	data := []byte(`{"hello":"Hi","nested":{"key":"val"}}`)
	if err := LoadJSONBytes("en", data); err != nil {
		t.Fatal(err)
	}
	if TLocale("en", "hello") != "Hi" {
		t.Fatal("flat key")
	}
	if TLocale("en", "nested.key") != "val" {
		t.Fatal("nested key")
	}
}

func TestLoadDirectoryJSONFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "fr.json"), []byte(`{"bye":"Au revoir"}`), 0644)

	mu.Lock()
	translations = make(map[string]map[string]string)
	defaultLocale = "en"
	mu.Unlock()

	if err := LoadDirectory(dir); err != nil {
		t.Fatal(err)
	}
	if TLocale("fr", "bye") != "Au revoir" {
		t.Fatalf("got %q", TLocale("fr", "bye"))
	}
}
