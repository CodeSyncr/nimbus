package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/cli/ui"
	"github.com/spf13/cobra"
)

func init() {
	cli.RegisterCommand(&DeployCommand{})
	cli.RegisterCommand(&DeployInitCommand{})
	cli.RegisterCommand(&DeployStatusCommand{})
	cli.RegisterCommand(&DeployLogsCommand{})
	cli.RegisterCommand(&DeployEnvCommand{})
	cli.RegisterCommand(&DeployRollbackCommand{})
}

// ---------------------------------------------------------------------------
// Nimbus Forge — one-command deployment CLI
// ---------------------------------------------------------------------------

// DeployCommand deploys the application.
type DeployCommand struct {
	target    string
	region    string
	app       string
	skipBuild bool
	dryRun    bool
	tag       string
	env       string
}

func (c *DeployCommand) Name() string { return "deploy" }
func (c *DeployCommand) Description() string {
	return "Deploy your Nimbus app to production (Nimbus Forge)"
}
func (c *DeployCommand) Aliases() []string { return []string{"forge"} }
func (c *DeployCommand) Args() int         { return -1 }

func (c *DeployCommand) Flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.target, "target", "", "Deploy target: fly, railway, docker, render, aws, gcp, netlify")
	cmd.Flags().StringVar(&c.region, "region", "", "Deployment region")
	cmd.Flags().StringVar(&c.app, "app", "", "Application name")
	cmd.Flags().BoolVar(&c.skipBuild, "skip-build", false, "Skip build step")
	cmd.Flags().BoolVar(&c.dryRun, "dry-run", false, "Show what would be deployed without deploying")
	cmd.Flags().StringVar(&c.tag, "tag", "", "Docker image tag (default: git sha)")
	cmd.Flags().StringVar(&c.env, "env", "production", "Environment (production, staging)")
}

func (c *DeployCommand) Run(ctx *cli.Context) error {
	return runDeploy(ctx.Cmd, ctx.Args)
}

// DeployInitCommand initializes deployment config.
type DeployInitCommand struct {
	target string
}

func (c *DeployInitCommand) Name() string        { return "deploy:init" }
func (c *DeployInitCommand) Description() string { return "Initialize deployment configuration" }
func (c *DeployInitCommand) Args() int           { return -1 }

func (c *DeployInitCommand) Flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.target, "target", "", "Deploy target: fly, railway, docker, render, netlify")
}

func (c *DeployInitCommand) Run(ctx *cli.Context) error {
	return runDeployInit(ctx.Cmd, ctx.Args)
}

// DeployStatusCommand checks deployment status.
type DeployStatusCommand struct{}

func (c *DeployStatusCommand) Name() string        { return "deploy:status" }
func (c *DeployStatusCommand) Description() string { return "Check deployment status" }

func (c *DeployStatusCommand) Run(ctx *cli.Context) error {
	return runDeployStatus(ctx.Cmd, ctx.Args)
}

// DeployLogsCommand shows deployment logs.
type DeployLogsCommand struct{}

func (c *DeployLogsCommand) Name() string        { return "deploy:logs" }
func (c *DeployLogsCommand) Description() string { return "View deployment logs" }

func (c *DeployLogsCommand) Run(ctx *cli.Context) error {
	return runDeployLogs(ctx.Cmd, ctx.Args)
}

// DeployEnvCommand manages env vars.
type DeployEnvCommand struct {
	set   string
	unset string
	list  bool
}

func (c *DeployEnvCommand) Name() string        { return "deploy:env" }
func (c *DeployEnvCommand) Description() string { return "Manage deployment environment variables" }

func (c *DeployEnvCommand) Flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.set, "set", "", "Set env var: KEY=VALUE")
	cmd.Flags().StringVar(&c.unset, "unset", "", "Unset env var: KEY")
	cmd.Flags().BoolVar(&c.list, "list", false, "List all env vars")
}

func (c *DeployEnvCommand) Run(ctx *cli.Context) error {
	return runDeployEnv(ctx.Cmd, ctx.Args)
}

// DeployRollbackCommand rolls back deployment.
type DeployRollbackCommand struct{}

func (c *DeployRollbackCommand) Name() string        { return "deploy:rollback" }
func (c *DeployRollbackCommand) Description() string { return "Rollback to previous deployment" }

func (c *DeployRollbackCommand) Run(ctx *cli.Context) error {
	return runDeployRollback(ctx.Cmd, ctx.Args)
}

// ---------------------------------------------------------------------------
// Deploy Config
// ---------------------------------------------------------------------------

// ForgeConfig is the deployment configuration file (nimbus.deploy.json).
type ForgeConfig struct {
	App         string            `json:"app"`
	Target      string            `json:"target"`
	Region      string            `json:"region"`
	Port        int               `json:"port"`
	GoVersion   string            `json:"go_version"`
	Environment string            `json:"environment"`
	Env         map[string]string `json:"env"`
	Build       ForgeBuild        `json:"build"`
	Resources   ForgeResources    `json:"resources"`
	Health      ForgeHealth       `json:"health"`
	Scaling     ForgeScaling      `json:"scaling"`
	Hooks       ForgeHooks        `json:"hooks"`
	Volumes     []ForgeVolume     `json:"volumes,omitempty"`
	Services    []ForgeService    `json:"services,omitempty"`
}

// ForgeBuild configures the build step.
type ForgeBuild struct {
	Command    string   `json:"command"`
	Output     string   `json:"output"`
	LDFlags    string   `json:"ldflags"`
	Tags       []string `json:"tags,omitempty"`
	CGOEnabled bool     `json:"cgo_enabled"`
}

// ForgeResources configures resource limits.
type ForgeResources struct {
	Memory      string `json:"memory"`
	CPUs        int    `json:"cpus"`
	DiskGB      int    `json:"disk_gb,omitempty"`
	MaxBodySize string `json:"max_body_size,omitempty"`
}

// ForgeHealth configures health checks.
type ForgeHealth struct {
	Path     string `json:"path"`
	Interval int    `json:"interval"`
	Timeout  int    `json:"timeout"`
}

// ForgeScaling configures auto-scaling.
type ForgeScaling struct {
	MinInstances int `json:"min_instances"`
	MaxInstances int `json:"max_instances"`
	TargetCPU    int `json:"target_cpu_percent,omitempty"`
}

// ForgeHooks are lifecycle hooks.
type ForgeHooks struct {
	PreBuild  string `json:"pre_build,omitempty"`
	PostBuild string `json:"post_build,omitempty"`
	PreDeploy string `json:"pre_deploy,omitempty"`
	Release   string `json:"release,omitempty"` // DB migrations etc.
}

// ForgeVolume configures persistent volumes.
type ForgeVolume struct {
	Name      string `json:"name"`
	MountPath string `json:"mount_path"`
	SizeGB    int    `json:"size_gb"`
}

// ForgeService is an additional service (DB, cache, etc.).
type ForgeService struct {
	Name  string `json:"name"`
	Type  string `json:"type"` // postgres, redis, mysql
	Plan  string `json:"plan"`
	EnvAs string `json:"env_as"` // env var to bind connection string
}

// ---------------------------------------------------------------------------
// Deploy Init
// ---------------------------------------------------------------------------

func runDeployInit(cmd *cobra.Command, args []string) error {
	target, _ := cmd.Flags().GetString("target")
	if target == "" {
		prompt := ui.NewUI(os.Stdin, os.Stdout, os.Stderr)
		selected, err := prompt.AskSelect("Select deploy target:", []string{"fly", "render", "railway", "netlify", "docker", "aws", "gcp"}, "docker")
		if err != nil {
			return err
		}
		target = selected
	}

	appName := filepath.Base(mustGetwd())

	cfg := ForgeConfig{
		App:         appName,
		Target:      target,
		Region:      "auto",
		Port:        3000,
		GoVersion:   detectGoVersion(),
		Environment: "production",
		Env:         map[string]string{},
		Build: ForgeBuild{
			Command: "go build -o ./bin/server .",
			Output:  "./bin/server",
			LDFlags: "-s -w",
		},
		Resources: ForgeResources{
			Memory: "512mb",
			CPUs:   1,
		},
		Health: ForgeHealth{
			Path:     "/health",
			Interval: 15,
			Timeout:  5,
		},
		Scaling: ForgeScaling{
			MinInstances: 1,
			MaxInstances: 3,
		},
		Hooks: ForgeHooks{
			Release: "./bin/server migrate",
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile("nimbus.deploy.json", data, 0644); err != nil {
		return err
	}

	fmt.Println("✓ Created nimbus.deploy.json")

	// Generate Dockerfile.
	if err := generateDockerfile(cfg); err != nil {
		return err
	}
	fmt.Println("✓ Created Dockerfile")

	// Generate .dockerignore.
	if err := generateDockerignore(); err != nil {
		return err
	}
	fmt.Println("✓ Created .dockerignore")

	// Generate platform-specific files.
	switch target {
	case "fly":
		if err := generateFlyToml(cfg); err != nil {
			return err
		}
		fmt.Println("✓ Created fly.toml")
	case "render":
		if err := generateRenderYaml(cfg); err != nil {
			return err
		}
		fmt.Println("✓ Created render.yaml")
	case "railway":
		if err := generateRailwayToml(cfg); err != nil {
			return err
		}
		fmt.Println("✓ Created railway.toml")
	case "netlify":
		if err := generateNetlifyToml(cfg); err != nil {
			return err
		}
		fmt.Println("✓ Created netlify.toml")
		fmt.Println("")
		fmt.Println("  ⚠  Netlify deploys static files only.")
		fmt.Println("     Place your built HTML/CSS/JS in the 'public/' directory.")
		fmt.Println("     For full-stack Nimbus apps, use: fly, railway, or render.")
	}

	fmt.Printf("\n🚀 Deployment initialized for %s target: %s\n", appName, target)
	fmt.Printf("   Run `nimbus deploy` to deploy your app.\n")
	return nil
}

// ---------------------------------------------------------------------------
// Deploy
// ---------------------------------------------------------------------------

func runDeploy(cmd *cobra.Command, args []string) error {
	cfg, err := loadDeployConfig()
	if err != nil {
		return fmt.Errorf("no deployment config found — run `nimbus deploy:init` first: %w", err)
	}

	// Override from flags.
	if t, _ := cmd.Flags().GetString("target"); t != "" {
		cfg.Target = t
	}
	if r, _ := cmd.Flags().GetString("region"); r != "" {
		cfg.Region = r
	}
	if a, _ := cmd.Flags().GetString("app"); a != "" {
		cfg.App = a
	}
	if e, _ := cmd.Flags().GetString("env"); e != "" {
		cfg.Environment = e
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	skipBuild, _ := cmd.Flags().GetBool("skip-build")
	tag, _ := cmd.Flags().GetString("tag")

	if tag == "" {
		tag = getGitSHA()
	}

	steps := []deployStep{
		{name: "Pre-flight checks", fn: func() error { return preflight(cfg) }},
	}

	if !skipBuild {
		if cfg.Hooks.PreBuild != "" {
			steps = append(steps, deployStep{name: "Pre-build hook", fn: func() error { return runHook(cfg.Hooks.PreBuild) }})
		}
		steps = append(steps, deployStep{name: "Building application", fn: func() error { return buildApp(cfg) }})
		if cfg.Hooks.PostBuild != "" {
			steps = append(steps, deployStep{name: "Post-build hook", fn: func() error { return runHook(cfg.Hooks.PostBuild) }})
		}
		needsDocker := cfg.Target == "docker" || cfg.Target == "aws" || cfg.Target == "gcp"
		if needsDocker {
			steps = append(steps, deployStep{name: "Building Docker image", fn: func() error { return buildDockerImage(cfg, tag) }})
		}
	}

	switch cfg.Target {
	case "fly":
		steps = append(steps, deployStep{name: "Deploying to Fly.io", fn: func() error { return deployToFly(cfg) }})
	case "railway":
		steps = append(steps, deployStep{name: "Deploying to Railway", fn: func() error { return deployToRailway(cfg) }})
	case "render":
		steps = append(steps, deployStep{name: "Deploying to Render", fn: func() error { return deployToRender(cfg) }})
	case "docker":
		steps = append(steps, deployStep{name: "Pushing Docker image", fn: func() error { return pushDockerImage(cfg, tag) }})
	case "aws":
		steps = append(steps, deployStep{name: "Deploying to AWS", fn: func() error { return deployToAWS(cfg, tag) }})
	case "gcp":
		steps = append(steps, deployStep{name: "Deploying to GCP", fn: func() error { return deployToGCP(cfg, tag) }})
	case "netlify":
		steps = append(steps, deployStep{name: "Deploying to Netlify", fn: func() error { return deployToNetlify(cfg) }})
	default:
		return fmt.Errorf("unsupported target: %s", cfg.Target)
	}

	fmt.Printf("\n🔨 Nimbus Forge — Deploying %s\n", cfg.App)
	fmt.Printf("   Target: %s | Region: %s | Env: %s\n\n", cfg.Target, cfg.Region, cfg.Environment)

	if dryRun {
		fmt.Println("DRY RUN — steps that would be executed:")
		for i, step := range steps {
			fmt.Printf("  %d. %s\n", i+1, step.name)
		}
		return nil
	}

	start := time.Now()
	for i, step := range steps {
		fmt.Printf("  [%d/%d] %s... ", i+1, len(steps), step.name)
		if err := step.fn(); err != nil {
			fmt.Println("✗")
			return fmt.Errorf("deploy failed at step '%s': %w", step.name, err)
		}
		fmt.Println("✓")
	}

	fmt.Printf("\n✅ Deployed successfully in %s\n", time.Since(start).Round(time.Millisecond))
	return nil
}

type deployStep struct {
	name string
	fn   func() error
}

// ---------------------------------------------------------------------------
// Pre-flight
// ---------------------------------------------------------------------------

func preflight(cfg *ForgeConfig) error {
	// Check Go is available.
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("go not found in PATH")
	}

	// Check Docker is available.
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found in PATH — install Docker to deploy")
	}

	// Check target CLI tools.
	switch cfg.Target {
	case "fly":
		if _, err := exec.LookPath("flyctl"); err != nil {
			return fmt.Errorf("flyctl not found — install with: curl -L https://fly.io/install.sh | sh")
		}
	case "railway":
		if _, err := exec.LookPath("railway"); err != nil {
			return fmt.Errorf("railway CLI not found — install with: npm i -g @railway/cli")
		}
	case "aws":
		if _, err := exec.LookPath("aws"); err != nil {
			return fmt.Errorf("aws CLI not found — install from: https://aws.amazon.com/cli/")
		}
	case "gcp":
		if _, err := exec.LookPath("gcloud"); err != nil {
			return fmt.Errorf("gcloud not found — install from: https://cloud.google.com/sdk/")
		}
	case "netlify":
		if _, err := exec.LookPath("netlify"); err != nil {
			return fmt.Errorf("netlify CLI not found — install with: npm i -g netlify-cli")
		}
	case "render":
		if _, err := exec.LookPath("render"); err != nil {
			fmt.Println("  ℹ Render CLI not found — installing via Homebrew...")
			// Tap and install.
			if _, brewErr := exec.LookPath("brew"); brewErr != nil {
				return fmt.Errorf("render CLI not found and Homebrew is not installed — install from: https://brew.sh then run: brew tap render-oss/render && brew install render")
			}
			tap := exec.Command("brew", "tap", "render-oss/render")
			tap.Stdout = os.Stdout
			tap.Stderr = os.Stderr
			if err := tap.Run(); err != nil {
				return fmt.Errorf("failed to tap render-oss/render: %w", err)
			}
			install := exec.Command("brew", "install", "render")
			install.Stdout = os.Stdout
			install.Stderr = os.Stderr
			if err := install.Run(); err != nil {
				return fmt.Errorf("failed to install render CLI: %w", err)
			}
			fmt.Println("  ✓ Render CLI installed")
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Build
// ---------------------------------------------------------------------------

func buildApp(cfg *ForgeConfig) error {
	env := os.Environ()
	env = append(env, "GOOS=linux", "GOARCH=amd64")
	if !cfg.Build.CGOEnabled {
		env = append(env, "CGO_ENABLED=0")
	}

	cmdParts := strings.Fields(cfg.Build.Command)
	if len(cmdParts) >= 2 && cmdParts[0] == "go" && cmdParts[1] == "build" {
		var newParts []string
		newParts = append(newParts, "go", "build")
		if cfg.Build.LDFlags != "" {
			newParts = append(newParts, "-ldflags", cfg.Build.LDFlags)
		}
		for _, tag := range cfg.Build.Tags {
			newParts = append(newParts, "-tags", tag)
		}
		newParts = append(newParts, cmdParts[2:]...)
		cmdParts = newParts
	} else {
		if cfg.Build.LDFlags != "" {
			cmdParts = append(cmdParts, "-ldflags", cfg.Build.LDFlags)
		}
		for _, tag := range cfg.Build.Tags {
			cmdParts = append(cmdParts, "-tags", tag)
		}
	}

	c := exec.Command(cmdParts[0], cmdParts[1:]...)
	c.Env = env
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func buildDockerImage(cfg *ForgeConfig, tag string) error {
	imageName := fmt.Sprintf("%s:%s", cfg.App, tag)
	c := exec.Command("docker", "build", "-t", imageName, "-t", cfg.App+":latest", ".")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func pushDockerImage(cfg *ForgeConfig, tag string) error {
	imageName := fmt.Sprintf("%s:%s", cfg.App, tag)
	c := exec.Command("docker", "push", imageName)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// ---------------------------------------------------------------------------
// Platform Deploys
// ---------------------------------------------------------------------------

func deployToFly(cfg *ForgeConfig) error {
	args := []string{"deploy", "--no-cache"}
	if cfg.Region != "" && cfg.Region != "auto" {
		args = append(args, "--region", cfg.Region)
	}
	c := exec.Command("flyctl", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func deployToRailway(cfg *ForgeConfig) error {
	c := exec.Command("railway", "up")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func deployToRender(cfg *ForgeConfig) error {
	// 1. Check if user is logged in; if not, trigger interactive login.
	if err := exec.Command("render", "workspaces", "-o", "json").Run(); err != nil {
		fmt.Println("\n  ℹ Not logged in to Render — opening login flow...")
		login := exec.Command("render", "login")
		login.Stdin = os.Stdin
		login.Stdout = os.Stdout
		login.Stderr = os.Stderr
		if err := login.Run(); err != nil {
			return fmt.Errorf("render login failed: %w", err)
		}
	}

	// 2. Ensure render.yaml exists (Render Blueprint for the service).
	if _, err := os.Stat("render.yaml"); os.IsNotExist(err) {
		fmt.Println("  ℹ No render.yaml found — generating...")
		if err := generateRenderYaml(*cfg); err != nil {
			return fmt.Errorf("failed to generate render.yaml: %w", err)
		}
		fmt.Println("  ✓ Created render.yaml")
	}

	// 3. Check if we have an existing service by listing services.
	listOut, err := exec.Command("render", "services", "-o", "json").Output()
	hasServices := err == nil && len(listOut) > 5 && string(listOut) != "[]"

	if hasServices {
		// Existing service found — trigger a deploy via interactive service selection.
		fmt.Println("  ℹ Triggering deploy on existing service...")
		deploy := exec.Command("render", "deploys", "create")
		deploy.Stdin = os.Stdin
		deploy.Stdout = os.Stdout
		deploy.Stderr = os.Stderr
		return deploy.Run()
	}

	// 4. No services yet — guide the user to create one via Blueprint.
	fmt.Println("")
	fmt.Println("  ┌─────────────────────────────────────────────────────────┐")
	fmt.Println("  │  No Render services found. First-time setup required:  │")
	fmt.Println("  │                                                        │")
	fmt.Println("  │  1. Push your code to GitHub/GitLab                    │")
	fmt.Println("  │  2. Go to: https://dashboard.render.com/blueprints    │")
	fmt.Println("  │  3. Click 'New Blueprint Instance'                     │")
	fmt.Println("  │  4. Select your repo — Render will use render.yaml     │")
	fmt.Println("  │                                                        │")
	fmt.Println("  │  After setup, run `nimbus deploy` again to trigger     │")
	fmt.Println("  │  deploys directly from the CLI.                        │")
	fmt.Println("  └─────────────────────────────────────────────────────────┘")
	fmt.Println("")

	// Open the Render Blueprint page in the browser.
	exec.Command("open", "https://dashboard.render.com/blueprints").Run()

	return nil
}

func deployToAWS(cfg *ForgeConfig, tag string) error {
	region := cfg.Region
	if region == "" || region == "auto" {
		region = "us-east-1"
	}

	// Get ECR registry.
	out, err := exec.Command("aws", "sts", "get-caller-identity", "--query", "Account", "--output", "text").Output()
	if err != nil {
		return fmt.Errorf("failed to get AWS account: %w", err)
	}
	accountID := strings.TrimSpace(string(out))
	registry := fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com", accountID, region)
	fullImage := fmt.Sprintf("%s/%s:%s", registry, cfg.App, tag)

	// Login to ECR.
	loginCmd := exec.Command("aws", "ecr", "get-login-password", "--region", region)
	loginOut, err := loginCmd.Output()
	if err != nil {
		return fmt.Errorf("ECR login failed: %w", err)
	}

	dockerLogin := exec.Command("docker", "login", "--username", "AWS", "--password-stdin", registry)
	dockerLogin.Stdin = bytes.NewReader(loginOut)
	if err := dockerLogin.Run(); err != nil {
		return fmt.Errorf("docker ECR login failed: %w", err)
	}

	// Create repo if not exists.
	exec.Command("aws", "ecr", "create-repository",
		"--repository-name", cfg.App,
		"--region", region).Run() // ignore error if exists

	// Tag and push.
	if err := exec.Command("docker", "tag", cfg.App+":"+tag, fullImage).Run(); err != nil {
		return err
	}
	pushCmd := exec.Command("docker", "push", fullImage)
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return err
	}

	// Update ECS service.
	exec.Command("aws", "ecs", "update-service",
		"--cluster", cfg.App,
		"--service", cfg.App,
		"--force-new-deployment",
		"--region", region).Run()

	return nil
}

func deployToGCP(cfg *ForgeConfig, tag string) error {
	project, err := exec.Command("gcloud", "config", "get-value", "project").Output()
	if err != nil {
		return fmt.Errorf("no GCP project configured: %w", err)
	}
	proj := strings.TrimSpace(string(project))
	imageName := fmt.Sprintf("gcr.io/%s/%s:%s", proj, cfg.App, tag)

	// Tag and push.
	if err := exec.Command("docker", "tag", cfg.App+":"+tag, imageName).Run(); err != nil {
		return err
	}
	pushCmd := exec.Command("docker", "push", imageName)
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return err
	}

	// Deploy to Cloud Run.
	region := cfg.Region
	if region == "" || region == "auto" {
		region = "us-central1"
	}

	deployCmd := exec.Command("gcloud", "run", "deploy", cfg.App,
		"--image", imageName,
		"--region", region,
		"--platform", "managed",
		"--allow-unauthenticated",
		"--port", fmt.Sprintf("%d", cfg.Port),
		"--memory", cfg.Resources.Memory,
		"--min-instances", fmt.Sprintf("%d", cfg.Scaling.MinInstances),
		"--max-instances", fmt.Sprintf("%d", cfg.Scaling.MaxInstances),
	)
	deployCmd.Stdout = os.Stdout
	deployCmd.Stderr = os.Stderr
	return deployCmd.Run()
}

func deployToNetlify(cfg *ForgeConfig) error {
	// 1. Check if user is logged in; if not, trigger interactive login.
	if err := exec.Command("netlify", "status").Run(); err != nil {
		fmt.Println("\n  ℹ Not logged in to Netlify — opening login flow...")
		login := exec.Command("netlify", "login")
		login.Stdin = os.Stdin
		login.Stdout = os.Stdout
		login.Stderr = os.Stderr
		if err := login.Run(); err != nil {
			return fmt.Errorf("netlify login failed: %w", err)
		}
	}

	// 2. Check if site is linked; if not, create one (no Git hooks).
	if _, err := os.Stat(".netlify/state.json"); os.IsNotExist(err) {
		fmt.Println("  ℹ No Netlify site linked — creating site...")

		// Create a new site without Git integration.
		create := exec.Command("netlify", "sites:create", "--name", cfg.App, "--manual")
		create.Stdin = os.Stdin
		create.Stdout = os.Stdout
		create.Stderr = os.Stderr
		if err := create.Run(); err != nil {
			// Name may be taken — create with random name instead.
			fmt.Println("  ℹ Name taken, creating with auto-generated name...")
			create = exec.Command("netlify", "sites:create", "--manual")
			create.Stdin = os.Stdin
			create.Stdout = os.Stdout
			create.Stderr = os.Stderr
			if err := create.Run(); err != nil {
				return fmt.Errorf("netlify site creation failed: %w", err)
			}
		}

		// Link the current directory to the newly created site.
		link := exec.Command("netlify", "link")
		link.Stdin = os.Stdin
		link.Stdout = os.Stdout
		link.Stderr = os.Stderr
		if err := link.Run(); err != nil {
			return fmt.Errorf("netlify link failed: %w", err)
		}
	}

	// 3. Direct deploy — upload the publish directory only (static files).
	//    No Git required, no functions — pure static file upload.
	deploy := exec.Command("netlify", "deploy", "--prod", "--dir", "public")
	deploy.Stdin = os.Stdin
	deploy.Stdout = os.Stdout
	deploy.Stderr = os.Stderr
	return deploy.Run()
}

// ---------------------------------------------------------------------------
// Deploy Status / Logs / Env / Rollback
// ---------------------------------------------------------------------------

func runDeployStatus(cmd *cobra.Command, args []string) error {
	cfg, err := loadDeployConfig()
	if err != nil {
		return fmt.Errorf("no deployment config: %w", err)
	}

	fmt.Printf("📊 Deployment Status — %s\n\n", cfg.App)
	fmt.Printf("  Target:      %s\n", cfg.Target)
	fmt.Printf("  Region:      %s\n", cfg.Region)
	fmt.Printf("  Environment: %s\n", cfg.Environment)
	fmt.Printf("  Resources:   %s RAM, %d CPU\n", cfg.Resources.Memory, cfg.Resources.CPUs)
	fmt.Printf("  Scaling:     %d-%d instances\n", cfg.Scaling.MinInstances, cfg.Scaling.MaxInstances)
	fmt.Printf("  Health:      %s (every %ds)\n", cfg.Health.Path, cfg.Health.Interval)

	switch cfg.Target {
	case "fly":
		c := exec.Command("flyctl", "status")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	case "railway":
		c := exec.Command("railway", "status")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	case "netlify":
		c := exec.Command("netlify", "status")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	case "render":
		c := exec.Command("render", "services")
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	}
	return nil
}

func runDeployLogs(cmd *cobra.Command, args []string) error {
	cfg, err := loadDeployConfig()
	if err != nil {
		return err
	}

	switch cfg.Target {
	case "fly":
		c := exec.Command("flyctl", "logs")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	case "railway":
		c := exec.Command("railway", "logs")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	case "gcp":
		c := exec.Command("gcloud", "run", "services", "logs", "read", cfg.App)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	default:
		return fmt.Errorf("logs not supported for target: %s", cfg.Target)
	}
}

func runDeployEnv(cmd *cobra.Command, args []string) error {
	cfg, err := loadDeployConfig()
	if err != nil {
		return err
	}

	if set, _ := cmd.Flags().GetString("set"); set != "" {
		parts := strings.SplitN(set, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid format, use KEY=VALUE")
		}
		return setDeployEnv(cfg, parts[0], parts[1])
	}

	if unset, _ := cmd.Flags().GetString("unset"); unset != "" {
		return unsetDeployEnv(cfg, unset)
	}

	// List env vars.
	fmt.Printf("Environment Variables (%s):\n\n", cfg.Environment)
	for k, v := range cfg.Env {
		display := v
		lower := strings.ToLower(k)
		if strings.Contains(lower, "secret") || strings.Contains(lower, "key") || strings.Contains(lower, "password") || strings.Contains(lower, "token") {
			if len(v) > 4 {
				display = v[:4] + strings.Repeat("*", len(v)-4)
			}
		}
		fmt.Printf("  %s = %s\n", k, display)
	}
	return nil
}

func setDeployEnv(cfg *ForgeConfig, key, value string) error {
	cfg.Env[key] = value
	if err := saveDeployConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("✓ Set %s\n", key)

	// Also set on platform.
	switch cfg.Target {
	case "fly":
		exec.Command("flyctl", "secrets", "set", key+"="+value).Run()
	case "railway":
		exec.Command("railway", "variables", "set", key+"="+value).Run()
	}
	return nil
}

func unsetDeployEnv(cfg *ForgeConfig, key string) error {
	delete(cfg.Env, key)
	if err := saveDeployConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("✓ Unset %s\n", key)
	return nil
}

func runDeployRollback(cmd *cobra.Command, args []string) error {
	cfg, err := loadDeployConfig()
	if err != nil {
		return err
	}

	fmt.Printf("⏪ Rolling back %s...\n", cfg.App)

	switch cfg.Target {
	case "fly":
		releases, err := exec.Command("flyctl", "releases", "--json").Output()
		if err != nil {
			return err
		}
		var releaseList []map[string]any
		json.Unmarshal(releases, &releaseList)
		if len(releaseList) < 2 {
			return fmt.Errorf("no previous release to rollback to")
		}
		version := fmt.Sprintf("%v", releaseList[1]["Version"])
		c := exec.Command("flyctl", "releases", "rollback", version)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()

	case "gcp":
		region := cfg.Region
		if region == "" || region == "auto" {
			region = "us-central1"
		}
		// GCP Cloud Run: list revisions and route traffic.
		revisions, err := exec.Command("gcloud", "run", "revisions", "list",
			"--service", cfg.App, "--region", region,
			"--format=value(metadata.name)", "--limit=2").Output()
		if err != nil {
			return err
		}
		lines := strings.Split(strings.TrimSpace(string(revisions)), "\n")
		if len(lines) < 2 {
			return fmt.Errorf("no previous revision to rollback to")
		}
		c := exec.Command("gcloud", "run", "services", "update-traffic", cfg.App,
			"--region", region, "--to-revisions", lines[1]+"=100")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()

	default:
		return fmt.Errorf("rollback not supported for target: %s — use platform dashboard", cfg.Target)
	}
}

// ---------------------------------------------------------------------------
// File Generators
// ---------------------------------------------------------------------------

var dockerfileTmpl = template.Must(template.New("dockerfile").Parse(`# ============================================
# Nimbus Forge — Multi-stage Dockerfile
# ============================================

# Stage 1: Build
FROM golang:{{.GoVersion}}-alpine AS builder
RUN apk add --no-cache git ca-certificates tzdata
WORKDIR /app

# Cache dependencies.
COPY go.mod go.sum ./
# Strip local replace directives for Docker builds.
RUN sed -i '/replace.*=>.*\.\.\//d' go.mod && go mod download

# Copy source and build.
COPY . .
RUN sed -i '/replace.*=>.*\.\.\//d' go.mod && go mod tidy
{{- if .Build.CGOEnabled }}
RUN CGO_ENABLED=1 go build {{if .Build.LDFlags}}-ldflags "{{.Build.LDFlags}}"{{end}} -o /app/server {{.BuildTarget}}
{{- else }}
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build {{if .Build.LDFlags}}-ldflags "{{.Build.LDFlags}}"{{end}} -o /app/server {{.BuildTarget}}
{{- end }}

# Stage 2: Runtime
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata curl
WORKDIR /app

# Copy binary and assets.
COPY --from=builder /app/server /app/server
COPY --from=builder /app/public ./public/
COPY --from=builder /app/resources ./resources/

# Create non-root user.
RUN addgroup -S nimbus && adduser -S nimbus -G nimbus
RUN chown -R nimbus:nimbus /app
USER nimbus

EXPOSE {{.Port}}
ENV PORT={{.Port}}
ENV APP_ENV=production

HEALTHCHECK --interval={{.Health.Interval}}s --timeout={{.Health.Timeout}}s --retries=3 \
  CMD curl -fs http://localhost:{{.Port}}{{.Health.Path}} || exit 1

ENTRYPOINT ["/app/server"]
`))

func generateDockerfile(cfg ForgeConfig) error {
	buildTarget := extractBuildTarget(cfg.Build.Command)

	data := struct {
		ForgeConfig
		BuildTarget string
	}{cfg, buildTarget}

	var buf bytes.Buffer
	if err := dockerfileTmpl.Execute(&buf, data); err != nil {
		return err
	}
	return os.WriteFile("Dockerfile", buf.Bytes(), 0644)
}

func generateDockerignore() error {
	content := `# Nimbus Forge
.git
.gitignore
.env
.env.*
*.md
tmp/
vendor/
node_modules/
.DS_Store
*.test
*.spec
coverage/
.idea/
.vscode/
bin/server
nimbus.deploy.json
`
	return os.WriteFile(".dockerignore", []byte(content), 0644)
}

func generateFlyToml(cfg ForgeConfig) error {
	content := fmt.Sprintf(`# Nimbus Forge — Fly.io Configuration
app = "%s"
primary_region = "%s"
kill_signal = "SIGINT"
kill_timeout = "5s"

[build]
  dockerfile = "Dockerfile"

[env]
  PORT = "%d"

[http_service]
  internal_port = %d
  force_https = true
  auto_start_machines = true
  auto_stop_machines = true
  min_machines_running = %d

[[vm]]
  memory = "%s"
  cpus = %d

[checks]
  [checks.health]
    grace_period = "10s"
    interval = "%ds"
    method = "GET"
    path = "%s"
    timeout = "%ds"
    type = "http"
`, cfg.App, cfg.Region, cfg.Port, cfg.Port,
		cfg.Scaling.MinInstances, cfg.Resources.Memory,
		cfg.Resources.CPUs, cfg.Health.Interval,
		cfg.Health.Path, cfg.Health.Timeout)

	if cfg.Hooks.Release != "" {
		content += fmt.Sprintf(`
[deploy]
  release_command = "%s"
`, cfg.Hooks.Release)
	}

	return os.WriteFile("fly.toml", []byte(content), 0644)
}

func generateRenderYaml(cfg ForgeConfig) error {
	region := cfg.Region
	if region == "" || region == "auto" {
		region = "oregon"
	}

	content := fmt.Sprintf(`# Nimbus Forge — Render Configuration
services:
  - type: web
    name: %s
    runtime: docker
    plan: starter
    region: %s
    dockerfilePath: ./Dockerfile
    healthCheckPath: %s
    autoDeploy: false
    envVars:
      - key: PORT
        value: "%d"
      - key: APP_ENV
        value: production
`, cfg.App, region, cfg.Health.Path, cfg.Port)

	for _, svc := range cfg.Services {
		content += fmt.Sprintf(`
  - type: %s
    name: %s-%s
    plan: %s
    ipAllowList: []
`, svc.Type, cfg.App, svc.Name, svc.Plan)
	}

	return os.WriteFile("render.yaml", []byte(content), 0644)
}

func generateRailwayToml(cfg ForgeConfig) error {
	content := fmt.Sprintf(`# Nimbus Forge — Railway Configuration
[build]
  builder = "DOCKERFILE"
  dockerfilePath = "Dockerfile"

[deploy]
  startCommand = "/app/server"
  healthcheckPath = "%s"
  healthcheckTimeout = %d
  restartPolicyType = "ON_FAILURE"
  restartPolicyMaxRetries = 10
`, cfg.Health.Path, cfg.Health.Timeout)

	return os.WriteFile("railway.toml", []byte(content), 0644)
}

func generateNetlifyToml(cfg ForgeConfig) error {
	content := fmt.Sprintf(`# Nimbus Forge — Netlify Configuration
# Netlify serves static files only. Place built HTML/CSS/JS in public/.
# For full-stack Nimbus apps, use: nimbus deploy:init --target fly

[build]
  publish = "public"

[[headers]]
  for = "/*"
  [headers.values]
    Cache-Control = "public, max-age=3600"
`)

	return os.WriteFile("netlify.toml", []byte(content), 0644)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func loadDeployConfig() (*ForgeConfig, error) {
	data, err := os.ReadFile("nimbus.deploy.json")
	if err != nil {
		return nil, err
	}
	var cfg ForgeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Env == nil {
		cfg.Env = make(map[string]string)
	}
	return &cfg, nil
}

func saveDeployConfig(cfg *ForgeConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("nimbus.deploy.json", data, 0644)
}

func detectGoVersion() string {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "1.22"
	}
	re := regexp.MustCompile(`go\s+(\d+\.\d+)`)
	matches := re.FindStringSubmatch(string(data))
	if len(matches) >= 2 {
		return matches[1]
	}
	return "1.22"
}

func getGitSHA() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return fmt.Sprintf("%d", time.Now().Unix())
	}
	return strings.TrimSpace(string(out))
}

func runHook(cmd string) error {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return nil
	}
	c := exec.Command(parts[0], parts[1:]...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func mustGetwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return "app"
	}
	return dir
}

func extractBuildTarget(cmd string) string {
	parts := strings.Fields(cmd)
	for i, p := range parts {
		if p == "-o" && i+2 < len(parts) {
			return parts[i+2]
		}
	}
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return "."
}
