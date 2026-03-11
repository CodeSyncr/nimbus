package main

import (
	"fmt"
	"os"

	"github.com/CodeSyncr/nimbus/cli"
	_ "github.com/CodeSyncr/nimbus/cli/commands"
	"github.com/CodeSyncr/nimbus/internal/version"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:     "nimbus",
	Short:   "Nimbus CLI",
	Long:    "Nimbus framework command line interface for scaffolding, developing, and deploying applications.",
	Version: version.Nimbus,
}

func main() {
	if err := cli.NewRoot(rootCmd).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
