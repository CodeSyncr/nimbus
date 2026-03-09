package deploy

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// PromptTarget prompts the user to select a deploy target. Returns target and updated config.
func PromptTarget(dir string, cfg *Config) (target string, outCfg *Config, err error) {
	if cfg == nil {
		cfg = &Config{}
	}
	if !isTerminal(os.Stdin) {
		return "", nil, fmt.Errorf("no deploy target specified. Use: nimbus deploy fly|railway|aws|docker")
	}

	reader := bufio.NewReader(os.Stdin)
	defaultApp := filepath.Base(dir)
	defaultTarget := "fly"
	if cfg.Target != "" {
		defaultTarget = strings.ToLower(cfg.Target)
	}

	fmt.Println()
	fmt.Println("  Where do you want to deploy?")
	fmt.Println("    1) Fly.io    - Global edge, fast cold starts")
	fmt.Println("    2) Railway   - Simple, great DX")
	fmt.Println("    3) AWS       - ECS, App Runner, EKS")
	fmt.Println("    4) Docker    - Build image locally (for K8s or custom)")
	fmt.Println()
	fmt.Printf("  Select target (1-4) [%s]: ", defaultTarget)

	choice, err := readLine(reader)
	if err != nil {
		return "", nil, err
	}
	choice = strings.TrimSpace(choice)

	switch choice {
	case "1", "fly":
		target = "fly"
	case "2", "railway":
		target = "railway"
	case "3", "aws":
		target = "aws"
	case "4", "docker":
		target = "docker"
	default:
		if choice != "" {
			target = strings.ToLower(choice)
		} else {
			target = defaultTarget
		}
	}

	// Target-specific prompts
	switch target {
	case "fly", "fly.io":
		cfg, err = promptFlyConfig(reader, dir, cfg, defaultApp)
		if err != nil {
			return "", nil, err
		}
	case "railway":
		cfg, err = promptRailwayConfig(reader, dir, cfg, defaultApp)
		if err != nil {
			return "", nil, err
		}
	case "aws":
		cfg.AppName = defaultApp
	case "docker":
		cfg.AppName = defaultApp
	}

	cfg.Target = target

	// Save to deploy.yaml?
	path := filepath.Join(dir, "deploy.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Print("  Save to deploy.yaml for next time? [Y/n]: ")
		val, err := readLine(reader)
		if err != nil {
			return target, cfg, nil
		}
		val = strings.TrimSpace(strings.ToLower(val))
		if val == "" || val == "y" || val == "yes" {
			if err := saveConfig(dir, cfg); err != nil {
				fmt.Printf("  Warning: could not save deploy.yaml: %v\n", err)
			} else {
				fmt.Println("  Saved deploy.yaml")
			}
		}
	}

	return target, cfg, nil
}

func saveConfig(dir string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "deploy.yaml"), data, 0644)
}

func promptFlyConfig(r *bufio.Reader, dir string, cfg *Config, defaultApp string) (*Config, error) {
	// App name
	if cfg.AppName == "" {
		fmt.Print("  App name (Fly.io) [" + defaultApp + "]: ")
		val, err := readLine(r)
		if err != nil {
			return nil, err
		}
		val = strings.TrimSpace(val)
		if val == "" {
			cfg.AppName = defaultApp
		} else {
			cfg.AppName = val
		}
	}

	// Region
	if cfg.Region == "" {
		fmt.Print("  Region [iad] (iad=Virginia, lhr=London, syd=Sydney): ")
		val, err := readLine(r)
		if err != nil {
			return nil, err
		}
		val = strings.TrimSpace(val)
		if val == "" {
			cfg.Region = "iad"
		} else {
			cfg.Region = val
		}
	}

	// Migrations
	if cfg.Migrations == nil {
		fmt.Print("  Run migrations on deploy? [Y/n]: ")
		val, err := readLine(r)
		if err != nil {
			return nil, err
		}
		val = strings.TrimSpace(strings.ToLower(val))
		mig := val == "" || val == "y" || val == "yes"
		cfg.Migrations = &mig
	}

	fmt.Println()
	return cfg, nil
}

func promptRailwayConfig(r *bufio.Reader, dir string, cfg *Config, defaultApp string) (*Config, error) {
	cfg.AppName = defaultApp
	fmt.Println("  Railway will use your project from 'railway link' or current directory.")
	fmt.Println()
	return cfg, nil
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(line, "\n"), nil
}

func isTerminal(f *os.File) bool {
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}
