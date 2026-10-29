package commands

import (
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
	t.Setenv("PATH", t.TempDir()) // no go, no nimbus-native
	t.Chdir(t.TempDir())
	if _, _, err := nativeTool(); err == nil || !strings.Contains(err.Error(), nativeModule) {
		t.Fatalf("want a setup hint naming %s, got %v", nativeModule, err)
	}
}
