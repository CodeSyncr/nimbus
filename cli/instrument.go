package cli

import (
	"time"

	"github.com/spf13/cobra"
)

const instrumentedAnnotationKey = "nimbus:instrumented"

func instrumentCommandTree(root *cobra.Command) {
	if root == nil {
		return
	}
	instrumentCommand(root)
	for _, c := range root.Commands() {
		instrumentCommandTree(c)
	}
}

func instrumentCommand(cmd *cobra.Command) {
	if cmd == nil {
		return
	}

	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	if cmd.Annotations[instrumentedAnnotationKey] == "true" {
		return
	}
	cmd.Annotations[instrumentedAnnotationKey] = "true"

	origRunE := cmd.RunE
	origRun := cmd.Run

	// Wrap RunE first if present; otherwise wrap Run.
	if origRunE != nil {
		cmd.RunE = func(c *cobra.Command, args []string) error {
			start := time.Now()
			err := origRunE(c, args)
			runAfterCommandHooks(NewContext(c, args), time.Since(start), err)
			return err
		}
		return
	}

	if origRun != nil {
		cmd.Run = func(c *cobra.Command, args []string) {
			start := time.Now()
			origRun(c, args)
			runAfterCommandHooks(NewContext(c, args), time.Since(start), nil)
		}
	}
}
