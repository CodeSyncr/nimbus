package release

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// BumpType is patch, minor, or major.
type BumpType string

const (
	BumpPatch BumpType = "patch"
	BumpMinor BumpType = "minor"
	BumpMajor BumpType = "major"
)

// BumpVersion increments v according to bump type. e.g. v0.1.1 + patch -> v0.1.2
func BumpVersion(v string, bump BumpType) (string, error) {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid version %q", v)
	}
	var major, minor, patch int
	if _, err := fmt.Sscanf(parts[0], "%d", &major); err != nil {
		return "", err
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &minor); err != nil {
		return "", err
	}
	if _, err := fmt.Sscanf(parts[2], "%d", &patch); err != nil {
		return "", err
	}
	switch bump {
	case BumpPatch:
		patch++
	case BumpMinor:
		minor++
		patch = 0
	case BumpMajor:
		major++
		minor = 0
		patch = 0
	default:
		return "", fmt.Errorf("invalid bump type %q", bump)
	}
	return fmt.Sprintf("v%d.%d.%d", major, minor, patch), nil
}

// UpdateVersionInFile replaces oldVersion with newVersion in file.
func UpdateVersionInFile(path, oldVersion, newVersion string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)
	if !strings.Contains(content, oldVersion) {
		return nil
	}
	content = strings.ReplaceAll(content, oldVersion, newVersion)
	return os.WriteFile(path, []byte(content), 0644)
}

// UpdateVersionConstant updates the version constant in internal/version/version.go.
func UpdateVersionConstant(dir, newVersion string) error {
	path := filepath.Join(dir, "internal", "version", "version.go")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	re := regexp.MustCompile(`const Nimbus = "[^"]+"`)
	data = re.ReplaceAll(data, []byte(`const Nimbus = "`+newVersion+`"`))
	return os.WriteFile(path, data, 0644)
}

// UpdateMainGoTemplates updates version in cmd/nimbus/main.go goModContent.
func UpdateMainGoTemplates(dir, oldVersion, newVersion string) error {
	path := filepath.Join(dir, "cmd", "nimbus", "main.go")
	return UpdateVersionInFile(path, oldVersion, newVersion)
}

// GitTag creates a git tag for the version.
func GitTag(dir, version string) error {
	cmd := exec.Command("git", "tag", version)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// IsNimbusRepo returns true if dir is the Nimbus framework repo (not an app using nimbus).
func IsNimbusRepo(dir string) bool {
	modPath := filepath.Join(dir, "go.mod")
	versionPath := filepath.Join(dir, "internal", "version", "version.go")
	data, err := os.ReadFile(modPath)
	if err != nil {
		return false
	}
	if !strings.HasPrefix(strings.TrimSpace(string(data)), "module github.com/CodeSyncr/nimbus") {
		return false
	}
	_, err = os.Stat(versionPath)
	return err == nil
}
