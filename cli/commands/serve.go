package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/cli/ui"
	"github.com/charmbracelet/lipgloss"
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
	airCmd.Env = append(os.Environ(), "NIMBUS_SERVE=1")
	if isInertiaApp(ctx.AppRoot) {
		airCmd.Env = append(airCmd.Env, "VITE_DEV=1")
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
  cmd = "nimbus build && go build -mod=mod -o ./tmp/main ."
  bin = "./tmp/main"
  delay = 1000
  exclude_dir = ["tmp", "vendor", "node_modules", "public"]
  exclude_regex = ["_test.go"]
  include_ext = ["go", "nimbus", "css", "js"]
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
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(12)
	value := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	fmt.Fprintln(ctx.Stdout)
	fmt.Fprintf(ctx.Stdout, "  %s %s\n", title.Render("⚡ NIMBUS"), dim.Render("Dev Server"))
	fmt.Fprintf(ctx.Stdout, "  %s\n", divider.Render("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"))
	fmt.Fprintf(ctx.Stdout, "  %s%s %s\n", label.Render("Mode"), value.Render("development"), dim.Render("(hot reload)"))
	fmt.Fprintf(ctx.Stdout, "  %s%s %s\n", label.Render("Watching"), value.Render(".go, .nimbus"), dim.Render("files"))
	if isInertiaApp(dir) {
		fmt.Fprintf(ctx.Stdout, "  %s%s %s\n", label.Render("Frontend"), value.Render("inertia/"), dim.Render("(HMR at localhost:5173)"))
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

// ---------------------------------------------------------------------------
// airFilter — filters Air's output, shows a spinner during compilation,
// and parses the __NIMBUS_READY__ marker from the app for beautiful display.
// ---------------------------------------------------------------------------

var _spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type airFilter struct {
	out      io.Writer
	ui       *ui.UI
	pw       *io.PipeWriter
	drop     *regexp.Regexp
	state    int32 // 0=idle 1=building 2=starting 3=ready
	spinStop chan struct{}
	mu       sync.Mutex
}

func newAirFilter(out io.Writer, u *ui.UI) *airFilter {
	drop := regexp.MustCompile(
		`(?i)` +
			`(^\s*$)` +
			`|(__\s+_\s+___)` +
			`|(/ /\\)` +
			`|(/_/--\\)` +
			`|(watching\s+)` +
			`|(!exclude\s+)` +
			`|(see you again)` +
			`|(cleaning\.\.\.)`,
	)
	pr, pw := io.Pipe()
	stop := make(chan struct{})
	close(stop) // pre-closed so first stopSpinner is a no-op
	f := &airFilter{out: out, ui: u, pw: pw, drop: drop, spinStop: stop}
	go f.readLoop(pr)
	return f
}

func (f *airFilter) readLoop(pr *io.PipeReader) {
	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if f.drop.MatchString(line) {
			continue
		}
		trimmed := strings.TrimSpace(line)

		// App emits this marker when it's ready to serve.
		if strings.HasPrefix(trimmed, "__NIMBUS_READY__") {
			f.stopSpinner()
			f.showReady(trimmed)
			atomic.StoreInt32(&f.state, 3)
			continue
		}

		// Air says "building" → show animated spinner.
		if strings.Contains(trimmed, "building") {
			f.stopSpinner()
			atomic.StoreInt32(&f.state, 1)
			f.startSpinner()
			continue
		}

		// Air says "running" → just stop spinner, wait for __NIMBUS_READY__.
		if strings.Contains(trimmed, "running") && !strings.Contains(trimmed, "error") {
			f.stopSpinner()
			atomic.StoreInt32(&f.state, 2)
			continue
		}

		// Everything else (logs, errors) → pass through.
		if trimmed != "" {
			if atomic.LoadInt32(&f.state) == 1 {
				// During compilation: clear spinner line, print the output,
				// the spinner goroutine will naturally put the spinner back
				// on the next tick (80ms). Output scrolls up, spinner stays
				// on the bottom line — just like npm/cargo.
				fmt.Fprintf(f.out, "\r\033[K")
			} else {
				f.stopSpinner()
			}
			fmt.Fprintln(f.out, line)
		}
	}
}

func (f *airFilter) startSpinner() {
	f.mu.Lock()
	f.spinStop = make(chan struct{})
	stop := f.spinStop
	f.mu.Unlock()

	yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	go func() {
		i := 0
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				fmt.Fprintf(f.out, "\r\033[K")
				return
			case <-ticker.C:
				frame := yellow.Render(_spinnerFrames[i%len(_spinnerFrames)])
				text := dim.Render("Compiling...")
				fmt.Fprintf(f.out, "\r  %s %s", frame, text)
				i++
			}
		}
	}()
}

func (f *airFilter) stopSpinner() {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-f.spinStop:
		// already stopped
	default:
		close(f.spinStop)
		time.Sleep(15 * time.Millisecond)
	}
}

func (f *airFilter) showReady(marker string) {
	parts := strings.Split(marker, "|")
	scheme, port, name, env, plugins := "http", "3000", "nimbus", "development", "0"
	if len(parts) >= 6 {
		scheme = parts[1]
		port = parts[2]
		name = parts[3]
		env = parts[4]
		plugins = parts[5]
	}
	url := fmt.Sprintf("%s://localhost:%s", scheme, port)

	green := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	bold := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(10)

	fmt.Fprintln(f.out)
	fmt.Fprintf(f.out, "  %s  %s\n", green.Render("✓"), bold.Render(name+" is ready"))
	fmt.Fprintln(f.out)
	fmt.Fprintf(f.out, "  %s  %s%s\n", green.Render("➜"), label.Render("Local:"), cyan.Render(url))
	fmt.Fprintf(f.out, "     %s%s %s %s\n", label.Render("Env:"), dim.Render(env), dim.Render("·"), dim.Render(plugins+" plugin(s)"))
	fmt.Fprintln(f.out)
}

func (f *airFilter) Write(p []byte) (int, error) {
	return f.pw.Write(p)
}
