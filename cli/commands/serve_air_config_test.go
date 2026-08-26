package commands

import (
	"strings"
	"testing"
)

const legacyAirConfig = `# Nimbus hot reload
root = "."
tmp_dir = "tmp"

[build]
  cmd = "nimbus build && go build -mod=mod -o ./tmp/main ."
  bin = "./tmp/main"
  exclude_dir = ["tmp", "vendor", "node_modules", "public"]
`

func TestPatchAirConfig_Unix(t *testing.T) {
	got, changed := patchAirConfig(legacyAirConfig, "")
	if !changed {
		t.Fatal("expected legacy config to be patched")
	}
	if !strings.Contains(got, `exclude_dir = ["tmp", "vendor", "node_modules", "public", "workspaces"]`) {
		t.Errorf("workspaces not added:\n%s", got)
	}
	if strings.Contains(got, ".exe") {
		t.Errorf("unix config must not gain .exe:\n%s", got)
	}
	if _, changed := patchAirConfig(got, ""); changed {
		t.Error("patch is not idempotent")
	}
}

func TestPatchAirConfig_Windows(t *testing.T) {
	got, changed := patchAirConfig(legacyAirConfig, ".exe")
	if !changed {
		t.Fatal("expected legacy config to be patched")
	}
	if !strings.Contains(got, `cmd = "nimbus build && go build -mod=mod -o ./tmp/main.exe ."`) {
		t.Errorf("build cmd not patched:\n%s", got)
	}
	if !strings.Contains(got, `bin = "./tmp/main.exe"`) {
		t.Errorf("bin not patched:\n%s", got)
	}
	if _, changed := patchAirConfig(got, ".exe"); changed {
		t.Error("patch is not idempotent")
	}
	// Custom/legacy backslash form and pre-patched configs stay untouched.
	custom := "bin = \"tmp\\main.exe\"\ncmd = \"go build -o tmp\\main.exe .\"\nexclude_dir = [\"workspaces\"]\n"
	if _, changed := patchAirConfig(custom, ".exe"); changed {
		t.Error("already-.exe config must not change")
	}
}

func TestAirConfigTemplateUsesPlatformExt(t *testing.T) {
	cfg := airConfig()
	want := `bin = "./tmp/main` + binExt + `"`
	if !strings.Contains(cfg, want) {
		t.Errorf("template missing %q:\n%s", want, cfg)
	}
}
