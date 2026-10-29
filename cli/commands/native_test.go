package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeToolOverride(t *testing.T) {
	t.Setenv("NIMBUS_NATIVE_BIN", "/opt/nn/nimbus-native")
	name, pre, err := nativeTool()
	if err != nil || name != "/opt/nn/nimbus-native" || len(pre) != 0 {
		t.Fatalf("got %q %v %v", name, pre, err)
	}
}

func TestNativeToolOutsideAnApp(t *testing.T) {
	t.Setenv("NIMBUS_NATIVE_BIN", "")
	t.Setenv("PATH", t.TempDir()) // no go, no nimbus-native: nothing can run it
	t.Chdir(t.TempDir())
	if _, _, err := nativeTool(); err == nil || !strings.Contains(err.Error(), nativeModule) {
		t.Fatalf("want a setup hint naming %s, got %v", nativeModule, err)
	}
}

func TestNativeToolLatest(t *testing.T) {
	t.Setenv("NIMBUS_NATIVE_BIN", "")
	bin := t.TempDir()
	// A go that knows no module: outside an app, new runs the latest toolchain.
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Chdir(t.TempDir())
	name, pre, err := nativeTool()
	if err != nil || name != "go" || strings.Join(pre, " ") != "run "+nativeModule+"/cmd/nimbus-native@latest" {
		t.Fatalf("got %q %v %v", name, pre, err)
	}
}
