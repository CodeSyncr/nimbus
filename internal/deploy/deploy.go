package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Deploy runs the deploy pipeline for the given target.
func Deploy(dir, target string, cfg *Config) error {
	if cfg == nil {
		cfg = &Config{}
	}
	if target == "" {
		target = cfg.Target
	}
	if target == "" {
		return fmt.Errorf("no deploy target specified. Use: nimbus deploy fly|railway|render|aws|docker")
	}
	target = strings.ToLower(target)

	// Ensure Dockerfile exists
	if _, err := EnsureDockerfile(dir); err != nil {
		return err
	}

	// Vendor deps when go.mod has local replace (../) - remote builds can't resolve it
	if err := ensureVendorForDeploy(dir); err != nil {
		return err
	}

	switch target {
	case "fly", "fly.io":
		if err := requireCLI("fly"); err != nil {
			return err
		}
		return deployFly(dir, cfg)
	case "railway":
		if err := requireCLI("railway"); err != nil {
			return err
		}
		return deployRailway(dir, cfg)
	case "render":
		return deployRender(dir, cfg)
	case "aws":
		if err := requireCLI("docker"); err != nil {
			return err
		}
		return deployAWS(dir, cfg)
	case "docker", "kubernetes", "k8s":
		if err := requireCLI("docker"); err != nil {
			return err
		}
		return deployDocker(dir, cfg)
	default:
		return fmt.Errorf("unknown target %q. Use: fly, railway, aws, docker", target)
	}
}

// deployDocker builds the image locally. Used for docker/k8s.
func deployDocker(dir string, cfg *Config) error {
	fmt.Println("  Building Docker image...")
	cmd := exec.Command("docker", "build", "-t", appNameFromDir(dir)+":latest", ".")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}
	fmt.Println("  Done. Run: docker run -p 8080:8080 -e PORT=8080 " + appNameFromDir(dir) + ":latest")
	return nil
}

func appNameFromDir(dir string) string {
	return filepath.Base(dir)
}

var cliInstallURLs = map[string]string{
	"fly":     "https://fly.io/docs/flyctl/install/",
	"railway": "https://docs.railway.com/develop/cli",
	"docker":  "https://docs.docker.com/get-docker/",
}

// ensureVendorForDeploy runs go mod vendor when go.mod has a replace pointing to ../ (local path).
// Remote builds (Railway, Fly, etc.) can't resolve ../ so vendor is required.
func ensureVendorForDeploy(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return nil
	}
	if !strings.Contains(string(data), "=> ../") {
		return nil
	}
	fmt.Println("  Vendoring deps (go.mod has local replace)...")
	cmd := exec.Command("go", "mod", "vendor")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go mod vendor failed: %w", err)
	}
	return nil
}

func requireCLI(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		url := cliInstallURLs[name]
		if url == "" {
			url = "https://google.com/search?q=" + name + "+cli+install"
		}
		return fmt.Errorf("%s CLI not found. Install it first:\n  %s", name, url)
	}
	return nil
}
