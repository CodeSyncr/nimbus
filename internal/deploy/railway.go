package deploy

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func deployRailway(dir string, cfg *Config) error {
	// Ensure user is logged in
	if err := ensureRailwayLogin(dir); err != nil {
		return err
	}

	// Ensure project is linked (run link or init if needed)
	if err := ensureRailwayProject(dir, cfg); err != nil {
		return err
	}

	// Resolve service name (required when Service: None in railway status)
	serviceName := cfg.Service
	if serviceName == "" {
		serviceName = cfg.AppName
	}
	if serviceName == "" {
		serviceName = filepath.Base(dir)
	}
	if serviceName == "" {
		return fmt.Errorf("railway deploy requires app_name or service in deploy.yaml")
	}

	// Link service if Service: None (fixes "Please specify a service" in non-interactive mode)
	link := exec.Command("railway", "service", serviceName)
	link.Dir = dir
	link.Stdout = os.Stdout
	link.Stderr = os.Stderr
	_ = link.Run() // ignore error; may already be linked

	// Railway uses `railway up` which builds from Dockerfile or Nixpacks
	args := []string{"up", "--detach", "--service", serviceName}
	fmt.Println("  Deploying to Railway...")
	cmd := exec.Command("railway", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("railway up failed: %w", err)
	}
	fmt.Println("  Deployed! Check your Railway dashboard.")
	return nil
}

func ensureRailwayLogin(dir string) error {
	// Check if already logged in
	check := exec.Command("railway", "whoami")
	check.Dir = dir
	check.Stdout = nil
	check.Stderr = nil
	if err := check.Run(); err == nil {
		return nil
	}

	// Not logged in - run railway login (opens browser)
	fmt.Println("  Railway login required. Opening browser...")
	login := exec.Command("railway", "login")
	login.Dir = dir
	login.Stdin = os.Stdin
	login.Stdout = os.Stdout
	login.Stderr = os.Stderr
	if err := login.Run(); err != nil {
		return fmt.Errorf("railway login failed: %w", err)
	}

	// Verify login succeeded
	verify := exec.Command("railway", "whoami")
	verify.Dir = dir
	var out bytes.Buffer
	verify.Stdout = &out
	verify.Stderr = io.Discard
	if err := verify.Run(); err != nil {
		return fmt.Errorf("railway login did not complete: %w", err)
	}
	fmt.Printf("  Logged in successfully.\n")
	return nil
}

func ensureRailwayProject(dir string, cfg *Config) error {
	// Check if project is linked
	check := exec.Command("railway", "status")
	check.Dir = dir
	var stderr bytes.Buffer
	check.Stdout = io.Discard
	check.Stderr = &stderr
	if err := check.Run(); err == nil {
		return nil
	}
	msg := stderr.String()
	if !strings.Contains(msg, "linked") && !strings.Contains(msg, "No linked") {
		return nil
	}

	// No project linked - try link first (select existing project)
	fmt.Println("  No project linked. Linking to Railway project...")
	link := exec.Command("railway", "link")
	link.Dir = dir
	link.Stdin = os.Stdin
	link.Stdout = os.Stdout
	link.Stderr = os.Stderr
	if err := link.Run(); err == nil {
		return nil
	}

	// Link failed (maybe no projects) - create new project
	fmt.Println("  Creating new Railway project...")
	appName := filepath.Base(dir)
	if cfg != nil && cfg.AppName != "" {
		appName = cfg.AppName
	}
	init := exec.Command("railway", "init", "--name", appName)
	init.Dir = dir
	init.Stdin = os.Stdin
	init.Stdout = os.Stdout
	init.Stderr = os.Stderr
	if err := init.Run(); err != nil {
		return fmt.Errorf("railway init failed: %w", err)
	}
	return nil
}
