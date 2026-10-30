package commands

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/spf13/cobra"
)

// nativeModule holds the Nimbus Native toolchain. `nimbus native` only finds
// the version an app depends on and hands it the arguments, so the nimbus
// CLI itself gains no dependency.
const nativeModule = "github.com/CodeSyncr/nimbus-native"

func init() {
	cli.RegisterRootAttach(attachNative)
}

func attachNative(root *cobra.Command) {
	root.AddCommand(&cobra.Command{
		Use:   "native [command]",
		Short: "Build and run native iOS and Android apps written in Go (Nimbus Native)",
		Long: `Build and run native iOS and Android apps written in Go.

  nimbus native new <name>             a project: the app (native/ios for Xcode,
                                       native/android for Gradle) with a Nimbus
                                       server and web app, or the app alone
  nimbus native upgrade [version]      move a project to a newer release
  nimbus native doctor                 check Xcode, the Android SDK and Go
  nimbus native dev ios|android [dir]  the app, plus its Nimbus server and web
                                       app, reloading on every save
  nimbus native run ios|android [dir]  build, install and launch, streaming logs
  nimbus native build ios|android      build an .app or .apk
  nimbus native gen [dir]              regenerate wire schemas

Inside an app whose go.mod requires ` + nativeModule + `, the toolchain
version the app depends on is used. Elsewhere the latest release runs, or
set NIMBUS_NATIVE_BIN to a toolchain binary.`,
		DisableFlagParsing: true, // every flag belongs to the toolchain
		SilenceUsage:       true,
		SilenceErrors:      true, // main prints the error once
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
				return cmd.Help()
			}
			if len(args) > 0 && args[0] == "new" {
				return nativeNew(cmd, args[1:])
			}
			return runNative(args)
		},
	})
}

func runNative(args []string) error {
	name, pre, err := nativeTool()
	if err != nil {
		return err
	}
	c := exec.Command(name, append(pre, args...)...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	c.Env = append(os.Environ(), "NIMBUS_NATIVE_VIA=nimbus")
	if err := c.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode()) // the toolchain already said what went wrong
		}
		return err
	}
	return nil
}

// nativeTool picks the toolchain: NIMBUS_NATIVE_BIN, else the version the
// current module requires (via go run, so it always matches the app's
// runtime), else a nimbus-native binary on PATH, else the latest release
// (so `nimbus native new` works anywhere).
func nativeTool() (string, []string, error) {
	if p := os.Getenv("NIMBUS_NATIVE_BIN"); p != "" {
		return p, nil, nil
	}
	if out, err := exec.Command("go", "list", "-m", nativeModule).Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		return "go", []string{"run", nativeModule + "/cmd/nimbus-native"}, nil
	}
	if p, err := exec.LookPath("nimbus-native"); err == nil {
		return p, nil, nil
	}
	if _, err := exec.LookPath("go"); err == nil {
		return "go", []string{"run", nativeModule + "/cmd/nimbus-native@latest"}, nil
	}
	return "", nil, fmt.Errorf("Nimbus Native isn't set up here: run this inside an app module that requires %s, or set NIMBUS_NATIVE_BIN", nativeModule)
}

// nativeNew makes a Nimbus Native project. With a Nimbus server (the
// default) it is a whole Nimbus app, made and asked for exactly as
// `nimbus new` does (database, auth, plugins), with the native app added to
// it by the toolchain; with -server none, the phone app alone, for a backend
// of your own.
func nativeNew(cmd *cobra.Command, args []string) error {
	fs := flag.NewFlagSet("native new", flag.ContinueOnError)
	server := fs.String("server", "", "nimbus (a Nimbus server and web app) or none (the phone app alone)")
	yes := fs.Bool("yes", false, "don't ask: a Nimbus server with SQLite, no auth, no extra plugins")
	fs.BoolVar(yes, "y", false, "same as -yes")
	db := fs.String("db", "", "database: sqlite, postgres, mysql, supabase")
	auth := fs.String("auth", "", "auth guard: none, session, access_token, stateless, basic")
	plugins := fs.String("plugins", "", "plugins, comma-separated: ai, mcp, telescope, pulse, scout, socialite, unpoly")
	id := fs.String("id", "", "bundle / application id (default: space.nimbusgo.<name>)")
	var flags, rest []string
	for i := 0; i < len(args); i++ { // flags may follow the name
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			rest = append(rest, a)
			continue
		}
		flags = append(flags, a)
		bare := strings.TrimLeft(a, "-")
		if !strings.Contains(a, "=") && bare != "yes" && bare != "y" && i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	if err := fs.Parse(append(flags, rest...)); err != nil {
		return err
	}

	ctx := cli.NewContext(cmd, nil)
	ctx.UI.Panel("Nimbus Native", "A native iOS and Android app, written in Go. Let's set it up.")
	asks := terminal() && !*yes && *db == "" && *auth == "" && *plugins == ""

	name := strings.TrimSpace(fs.Arg(0))
	if name == "" {
		if !asks {
			return fmt.Errorf("usage: nimbus native new <name> [-server nimbus|none] [-db D] [-auth A] [-plugins P] [-y]")
		}
		ans, err := ctx.UI.AskInput("Project name", "myapp")
		if err != nil {
			return err
		}
		name = strings.TrimSpace(ans)
	}
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("%s already exists", name)
	}

	const withNimbus = "A Nimbus server, which also serves the app on the web (recommended)"
	switch *server {
	case "nimbus", "none":
	case "":
		*server = "nimbus"
		if asks {
			ans, err := ctx.UI.AskSelect("What will the app talk to?",
				[]string{withNimbus, "My own backend (Node, Go, anything over HTTP): the phone app only"}, withNimbus)
			if err != nil {
				return err
			}
			if ans != withNimbus {
				*server = "none"
			}
		}
	default:
		return fmt.Errorf("-server %q: use nimbus or none", *server)
	}

	summary := "Server: none (the app talks to your own backend)"
	if *server == "nimbus" {
		nc := &NewCommand{Native: true, starter: "none", yes: *yes || !asks, db: *db, auth: *auth, plugins: *plugins}
		if err := nc.Run(&cli.Context{Cmd: cmd, Args: []string{name}, Stdout: ctx.Stdout, Stderr: ctx.Stderr, Stdin: ctx.Stdin, UI: ctx.UI}); err != nil {
			return err
		}
		summary = "Server: Nimbus, with the web app\n" + nc.Summary
	}

	ctx.UI.Infof("Adding the iOS and Android apps...")
	// A new project starts on the latest release (one on PATH may be old,
	// or a local build with no version to put in go.mod).
	tool, pre := "go", []string{"run", nativeModule + "/cmd/nimbus-native@latest"}
	if p := os.Getenv("NIMBUS_NATIVE_BIN"); p != "" {
		tool, pre = p, nil
	}
	targs := append(pre, "new", "-server", *server, "-quiet")
	if *id != "" {
		targs = append(targs, "-id", *id)
	}
	c := exec.Command(tool, append(targs, name)...)
	var out strings.Builder
	c.Stdout, c.Stderr = io.MultiWriter(&out), io.MultiWriter(&out)
	c.Env = append(os.Environ(), "NIMBUS_NATIVE_VIA=nimbus")
	if err := c.Run(); err != nil {
		fmt.Fprint(ctx.Stderr, out.String())
		if strings.Contains(out.String(), "flag provided but not defined") {
			return fmt.Errorf("this Nimbus Native toolchain is too old for nimbus native new; set NIMBUS_NATIVE_BIN or update it")
		}
		return fmt.Errorf("adding the native app: %w", err)
	}

	ctx.UI.Successf("Project %s created", name)
	ctx.UI.Panel("Configuration Summary", summary)
	next := "cd " + name + "\nnimbus native dev ios       the app on your iPhone or the simulator\nnimbus native dev android   the same on Android"
	if *server == "nimbus" {
		next += "\n\nBoth also run the Nimbus server and web app on http://127.0.0.1:3333,\nand reload everything when you save a .go file."
	} else {
		next += "\n\nThe app reaches your backend with api.New(\"https://your-server\")."
	}
	next += "\n\nopen native/ios/Runner.xcodeproj, or open native/android in Android Studio"
	ctx.UI.Panel("Next steps", next)
	return nil
}

// terminal reports whether someone is at the keyboard to answer questions.
func terminal() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
