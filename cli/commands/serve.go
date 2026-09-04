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
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/cli/ui"
	"github.com/CodeSyncr/nimbus/internal/startupview"
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

	// Say something immediately. Compiling, building assets and migrating can
	// run for a long time before the app is ready to report itself, and an
	// empty terminal for that whole stretch looks like a hang.
	fmt.Fprint(ctx.Stdout, startupview.RenderLaunch(startupview.Launch{
		Inertia: isInertiaApp(ctx.AppRoot),
	}))

	// Use Air for hot reload
	airCmd := exec.Command("go", "run", "github.com/air-verse/air@v1.52.3")
	airCmd.Dir = ctx.AppRoot
	airCmd.Stdin = ctx.Stdin
	airCmd.Env = append(os.Environ(), "NIMBUS_SERVE=1")
	if isInertiaApp(ctx.AppRoot) {
		airCmd.Env = append(airCmd.Env, "VITE_DEV=1")
	}
	// Give Air a real pipe rather than the filter directly: with a plain
	// io.Writer, cmd.Wait() blocks until the pipe EOFs, and the reloaded app
	// binary inherits the write end — so if Air dies while the app keeps
	// running, serve would hang forever with a dead watcher and a zombie child.
	filter := newAirFilter(ctx.Stdout, ctx.UI)
	airOut, airIn, err := os.Pipe()
	if err != nil {
		return err
	}
	airCmd.Stdout = airIn
	airCmd.Stderr = airIn
	go func() {
		_, _ = io.Copy(filter, airOut)
		_ = airOut.Close()
	}()

	setPgid(airCmd)

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
		setPgid(viteCmd)
		if err := viteCmd.Start(); err != nil {
			return err
		}
	}

	if err := airCmd.Start(); err != nil {
		_ = airIn.Close()
		_ = airOut.Close()
		if viteCmd != nil && viteCmd.Process != nil {
			_ = viteCmd.Process.Kill()
		}
		return err
	}
	// Parent must drop its copy of the write end so the reader sees EOF
	// once Air and the app are gone.
	_ = airIn.Close()

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
		// Air exited on its own; take Vite (and its node children) down too.
		killGroup(viteCmd, 100*time.Millisecond)
		if err != nil && !strings.Contains(err.Error(), "signal") {
			return err
		}
		return nil
	}
}

// airConfigTmpl is the raw config; use airConfig() to get the version for
// the current platform. Air runs `bin` through `cmd /c` on Windows, which
// refuses to execute a file without an .exe extension, and `go build -o`
// uses the name verbatim — so the binary must be named main.exe there.
const airConfigTmpl = `# Nimbus hot reload
root = "."
tmp_dir = "tmp"

[build]
  cmd = "nimbus build && go build -mod=mod -o ./tmp/main ."
  bin = "./tmp/main"
  delay = 1000
  exclude_dir = ["tmp", "vendor", "node_modules", "public", "workspaces"]
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

// airConfig returns the .air.toml contents for the current platform.
func airConfig() string {
	return strings.ReplaceAll(airConfigTmpl, "./tmp/main", "./tmp/main"+binExt)
}

func ensureAirConfig(dir string) {
	path := filepath.Join(dir, ".air.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		_ = os.WriteFile(path, []byte(airConfig()), 0644)
		return
	}

	// Air only reads .air.toml at startup, so patch stale configs before
	// launching it.
	if patched, changed := patchAirConfig(string(data), binExt); changed {
		_ = os.WriteFile(path, []byte(patched), 0644)
	}
}

var (
	airExcludeDirRe = regexp.MustCompile(`(?m)^(\s*exclude_dir\s*=\s*\[)([^\]]*)(\])`)
	// Matches tmp/main (or tmp\main) that isn't already followed by an
	// extension, e.g. in `-o ./tmp/main .` or `bin = "./tmp/main"`.
	airBinRe = regexp.MustCompile(`(tmp[/\\]main)([^.\w]|$)`)
)

// patchAirConfig upgrades a config generated by an older nimbus version.
// ext is the executable extension for the current platform ("" or ".exe").
func patchAirConfig(content, ext string) (string, bool) {
	orig := content

	// Older configs don't exclude workspaces/. Files created there at runtime
	// (agent sessions, git clones, scaffolds) then trigger a rebuild that kills
	// the running app mid-request.
	if !strings.Contains(content, `"workspaces"`) {
		if m := airExcludeDirRe.FindStringSubmatch(content); m != nil {
			entry := `"workspaces"`
			if items := strings.TrimSpace(m[2]); items != "" {
				entry = strings.TrimRight(items, ", ") + `, "workspaces"`
			}
			content = strings.Replace(content, m[0], m[1]+entry+m[3], 1)
		}
	}

	// Older configs build an extension-less ./tmp/main, which Air's Windows
	// runner (cmd /c) cannot execute. A main.exe binary runs fine on every
	// platform, so a config patched on Windows stays valid for Unix users.
	if ext != "" {
		content = airBinRe.ReplaceAllString(content, "${1}"+ext+"${2}")
	}

	return content, content != orig
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
	// Air needs to interrupt the app binary and wait kill_delay (1s)
	// before it can exit; killing it sooner orphans the app process.
	killGroup(air, 2*time.Second)
	killGroup(vite, 100*time.Millisecond)
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

// maxLogLine bounds one line of app output. A line longer than this is split
// rather than refused: bufio.Scanner reports ErrTooLong and stops, and a
// scanner that stops here takes the rest of the session's output with it.
const maxLogLine = 256 * 1024

func (f *airFilter) readLoop(pr *io.PipeReader) {
	reader := bufio.NewReaderSize(pr, 64*1024)
	for {
		line, err := readLine(reader, maxLogLine)
		if line == "" && err != nil {
			return
		}
		if f.drop.MatchString(line) {
			continue
		}
		trimmed := strings.TrimSpace(line)

		// App emits this marker when it's ready to serve.
		if startupview.IsMarker(trimmed) {
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

		if err != nil {
			return
		}
	}
}

// readLine reads one line, giving back at most max bytes of it.
//
// An over-long line is truncated and its remainder discarded, so a single
// enormous log entry costs that one line rather than every line after it.
// The returned error is only reported once the line in hand has been handled.
func readLine(r *bufio.Reader, max int) (string, error) {
	var sb strings.Builder
	for {
		chunk, isPrefix, err := r.ReadLine()
		if sb.Len() < max {
			room := max - sb.Len()
			if len(chunk) > room {
				chunk = chunk[:room]
			}
			sb.Write(chunk)
		}
		if err != nil {
			return sb.String(), err
		}
		if !isPrefix {
			return sb.String(), nil
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
	// The app hands over its whole boot report; the CLI owns the terminal, so
	// it draws the same view a direct `go run .` would have drawn itself.
	info, ok := startupview.ParseMarker(marker)
	if !ok {
		// Better a raw line than nothing: the app said it was ready, and
		// swallowing the message leaves the terminal looking hung.
		fmt.Fprintln(f.out, marker)
		return
	}
	fmt.Fprint(f.out, startupview.Render(info))
}

func (f *airFilter) Write(p []byte) (int, error) {
	return f.pw.Write(p)
}
