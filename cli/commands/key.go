package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/encryption"
	"github.com/spf13/cobra"
)

func init() {
	cli.RegisterCommand(&KeyGenerateCommand{})
}

// KeyGenerateCommand generates a cryptographically strong APP_KEY and writes it
// into the application's .env file. This backs session encryption and HMAC
// token signing; the framework refuses to boot in production without a strong
// key (see config.Validate).
//
// Artisan-equivalent: `php artisan key:generate`.
type KeyGenerateCommand struct {
	show  bool
	force bool
}

func (c *KeyGenerateCommand) Name() string        { return "key:generate" }
func (c *KeyGenerateCommand) Description() string { return "Generate an APP_KEY and set it in .env" }
func (c *KeyGenerateCommand) Aliases() []string   { return []string{"key:gen"} }
func (c *KeyGenerateCommand) Args() int           { return 0 }

func (c *KeyGenerateCommand) Flags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&c.show, "show", false, "Print the generated key without modifying .env")
	cmd.Flags().BoolVarP(&c.force, "force", "f", false, "Overwrite an existing APP_KEY without confirmation")
}

func (c *KeyGenerateCommand) Run(ctx *cli.Context) error {
	key, err := encryption.GenerateKey256()
	if err != nil {
		ctx.UI.Errorf("failed to generate key: %v", err)
		return err
	}

	if c.show {
		fmt.Fprintln(ctx.Stdout, key)
		return nil
	}

	if !isNimbusApp(ctx.AppRoot) {
		ctx.UI.Errorf("Not a Nimbus app. Run 'nimbus key:generate' from your app root, or use --show to just print a key.")
		return nil
	}

	envPath := filepath.Join(ctx.AppRoot, ".env")
	existing, hadKey, err := readAppKey(envPath)
	if err != nil {
		ctx.UI.Errorf("could not read %s: %v", envPath, err)
		return err
	}

	// Guard against clobbering a key that's already in use.
	if hadKey && existing != "" && !c.force {
		ok, err := ctx.UI.AskConfirm("APP_KEY is already set. Overwrite it? Existing sessions and signed tokens will be invalidated.", false)
		if err != nil {
			return err
		}
		if !ok {
			ctx.UI.Infof("Aborted. Existing APP_KEY left unchanged.")
			return nil
		}
	}

	if err := setAppKey(envPath, key); err != nil {
		ctx.UI.Errorf("could not update %s: %v", envPath, err)
		return err
	}

	ctx.UI.Successf("Application key set successfully in %s", envPath)
	return nil
}

// readAppKey returns the current APP_KEY value from an .env file. hadKey
// reports whether an APP_KEY line was present at all. A missing file is not an
// error (returns "", false, nil).
func readAppKey(envPath string) (value string, hadKey bool, err error) {
	data, err := os.ReadFile(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "APP_KEY=") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "APP_KEY=")), true, nil
		}
	}
	return "", false, nil
}

// setAppKey writes key into the APP_KEY entry of an .env file, replacing an
// existing line in place (preserving surrounding content) or appending one if
// absent. Creates the file if it does not exist.
func setAppKey(envPath, key string) error {
	newLine := "APP_KEY=" + key

	data, err := os.ReadFile(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			return os.WriteFile(envPath, []byte(newLine+"\n"), 0o600)
		}
		return err
	}

	lines := strings.Split(string(data), "\n")
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "APP_KEY=") {
			lines[i] = newLine
			replaced = true
			break
		}
	}
	if !replaced {
		// Append, avoiding a stray blank line if the file already ends in one.
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines[len(lines)-1] = newLine
			lines = append(lines, "")
		} else {
			lines = append(lines, newLine)
		}
	}

	return os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0o600)
}
