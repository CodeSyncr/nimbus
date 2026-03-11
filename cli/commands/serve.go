package commands

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/cli/ui"
	"github.com/spf13/cobra"
)

func init() {
	cli.RegisterCommand(&ServeCommand{})
}

type ServeCommand struct {
	watch bool
}

func (c *ServeCommand) Name() string        { return "serve" }
func (c *ServeCommand) Description() string { return "Start the Nimbus development server" }
func (c *ServeCommand) Args() int           { return 0 }

func (c *ServeCommand) Flags(cmd *cobra.Command) {
	cmd.Flags().BoolVarP(&c.watch, "watch", "w", true, "Watch for file changes and reload")
}

func (c *ServeCommand) Run(ctx *cli.Context) error {
	if !isNimbusApp(ctx.AppRoot) {
		ctx.UI.Errorf("Not a Nimbus app. Run 'nimbus serve' from your app root.")
		ctx.UI.Infof("Create an app with: nimbus new myapp")
		return nil
	}

	ensureAirConfig(ctx.AppRoot)
	printServeBanner(ctx.AppRoot, ctx)

	// Use Air for hot reload
	airCmd := exec.Command("go", "run", "github.com/air-verse/air@v1.52.3")
	airCmd.Dir = ctx.AppRoot
	airCmd.Stdin = ctx.Stdin
	if isInertiaApp(ctx.AppRoot) {
		airCmd.Env = append(os.Environ(), "VITE_DEV=1")
	}
	filter := newAirFilter(ctx.Stdout, ctx.UI)
	airCmd.Stdout = filter
	airCmd.Stderr = filter

	if runtime.GOOS != "windows" {
		airCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	var viteCmd *exec.Cmd
	if isInertiaApp(ctx.AppRoot) {
		if err := ensureInertiaBuild(ctx.AppRoot, ctx); err != nil {
			return err
		}
		viteCmd = exec.Command("npx", "vite")
		viteCmd.Dir = ctx.AppRoot
		viteCmd.Env = append(os.Environ(), "FORCE_COLOR=1")
		viteCmd.Stdout = io.Discard
		viteCmd.Stderr = io.Discard
		if runtime.GOOS != "windows" {
			viteCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		}
		if err := viteCmd.Start(); err != nil {
			return err
		}
	}

	if err := airCmd.Start(); err != nil {
		if viteCmd != nil && viteCmd.Process != nil {
			_ = viteCmd.Process.Kill()
		}
		return err
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	done := make(chan error, 1)
	go func() { done <- airCmd.Wait() }()

	select {
	case sig := <-quit:
		fmt.Printf("\n  \033[33m⚠\033[0m  Received %v, shutting down...\n", sig)
		killProcessGroup(airCmd, viteCmd)
		<-done
		return nil
	case err := <-done:
		if viteCmd != nil && viteCmd.Process != nil {
			_ = viteCmd.Process.Kill()
		}
		if err != nil && !strings.Contains(err.Error(), "signal") {
			return err
		}
		return nil
	}
}

const airConfigTmpl = `# Nimbus hot reload
root = "."
tmp_dir = "tmp"

[build]
  cmd = "go build -mod=mod -o ./tmp/main ."
  bin = "./tmp/main"
  delay = 1000
  exclude_dir = ["tmp", "vendor", "node_modules"]
  exclude_regex = ["_test.go"]
  include_ext = ["go", "nimbus"]
  send_interrupt = true
  kill_delay = "1s"

[log]
  time = false
  main_only = false

[misc]
  clean_on_exit = true
`

func ensureAirConfig(dir string) {
	path := filepath.Join(dir, ".air.toml")
	_ = os.WriteFile(path, []byte(airConfigTmpl), 0644)
}

func printServeBanner(dir string, ctx *cli.Context) {
	fmt.Fprintln(ctx.Stdout)
	fmt.Fprintf(ctx.Stdout, "  \033[1m\033[36mNIMBUS\033[0m \033[2mDev Server\033[0m\n")
	fmt.Fprintf(ctx.Stdout, "  \033[2m────────────────────────────────────\033[0m\n")
	fmt.Fprintf(ctx.Stdout, "  \033[32m➜\033[0m  Mode:     \033[33mdevelopment\033[0m \033[2m(hot reload)\033[0m\n")
	fmt.Fprintf(ctx.Stdout, "  \033[32m➜\033[0m  Watching: \033[36m.go\033[0m, \033[36m.nimbus\033[0m files\n")
	if isInertiaApp(dir) {
		fmt.Fprintf(ctx.Stdout, "  \033[32m➜\033[0m  Frontend: \033[36minertia/\033[0m \033[2m(HMR at localhost:5173)\033[0m\n")
	}
	fmt.Fprintln(ctx.Stdout)
}

func isInertiaApp(dir string) bool {
	pkgPath := filepath.Join(dir, "package.json")
	inertiaDir := filepath.Join(dir, "inertia")
	if _, err := os.Stat(pkgPath); err != nil {
		return false
	}
	if _, err := os.Stat(inertiaDir); err != nil {
		return false
	}
	return true
}

func ensureInertiaBuild(dir string, ctx *cli.Context) error {
	nodeModules := filepath.Join(dir, "node_modules")
	if _, err := os.Stat(nodeModules); err != nil {
		return fmt.Errorf("node_modules not found. Run 'npm install' first")
	}
	ctx.UI.Infof("Building frontend (npm run build)...")
	build := exec.Command("npm", "run", "build")
	build.Dir = dir
	build.Stdout = ctx.Stdout
	build.Stderr = ctx.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("npm run build failed: %w", err)
	}
	fmt.Fprintln(ctx.Stdout)
	return nil
}

func killProcessGroup(air *exec.Cmd, vite *exec.Cmd) {
	if runtime.GOOS == "windows" {
		if air.Process != nil {
			_ = air.Process.Kill()
		}
		if vite != nil && vite.Process != nil {
			_ = vite.Process.Kill()
		}
		return
	}
	if air.Process != nil {
		_ = syscall.Kill(-air.Process.Pid, syscall.SIGTERM)
		time.Sleep(100 * time.Millisecond)
		_ = syscall.Kill(-air.Process.Pid, syscall.SIGKILL)
	}
	if vite != nil && vite.Process != nil {
		_ = syscall.Kill(-vite.Process.Pid, syscall.SIGTERM)
		time.Sleep(100 * time.Millisecond)
		_ = syscall.Kill(-vite.Process.Pid, syscall.SIGKILL)
	}
}

type airFilter struct {
	out  io.Writer
	ui   *ui.UI
	pw   *io.PipeWriter
	drop *regexp.Regexp
}

func newAirFilter(out io.Writer, ui *ui.UI) *airFilter {
	// ... we will fix the type of UI in a minute if UI is an alias in cli or from ui package
	drop := regexp.MustCompile(
		`(?i)` +
			`(^\s*$)` + // blank lines
			`|(__\s+_\s+___)` + // Air ASCII art
			`|(/ /\\)` +
			`|(/_/--\\)` +
			`|(watching\s+)` +
			`|(!exclude\s+)` +
			`|(see you again)` +
			`|(cleaning\.\.\.)`,
	)
	pr, pw := io.Pipe()
	f := &airFilter{out: out, ui: ui, pw: pw, drop: drop}
	go func() {
		b := make([]byte, 1024)
		for {
			n, err := pr.Read(b)
			if n > 0 {
				str := string(b[:n])
				lines := strings.Split(str, "\n")
				for _, line := range lines {
					if f.drop.MatchString(line) {
						continue
					}
					trimmed := strings.TrimSpace(line)
					if strings.Contains(trimmed, "building") {
						fmt.Fprintf(out, "  \033[33m⟳\033[0m  Building...\n")
						continue
					}
					if strings.Contains(trimmed, "running") && !strings.Contains(trimmed, "error") {
						fmt.Fprintf(out, "  \033[32m✓\033[0m  Ready\n\n")
						continue
					}
					if line != "" {
						fmt.Fprintln(out, line)
					}
				}
			}
			if err != nil {
				break
			}
		}
	}()
	return f
}

func (f *airFilter) Write(p []byte) (int, error) {
	return f.pw.Write(p)
}
