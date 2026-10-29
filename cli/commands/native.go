package commands

import (
	"errors"
	"fmt"
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

  nimbus native new <dir>              a project: Nimbus server, shared UI,
                                       native/ios (Xcode) and native/android (Gradle)
  nimbus native doctor                 check Xcode, the Android SDK and Go
  nimbus native dev ios|android [dir]  run the app here, reload on every save
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
