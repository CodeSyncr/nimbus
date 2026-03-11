package commands

import (
	"fmt"
	"strings"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/internal/release"
	"github.com/CodeSyncr/nimbus/internal/version"
)

func init() {
	cli.RegisterCommand(&ReleaseCommand{})
}

type ReleaseCommand struct{}

func (c *ReleaseCommand) Name() string        { return "release" }
func (c *ReleaseCommand) Description() string { return "Release a new version of the Nimbus framework" }
func (c *ReleaseCommand) Args() int           { return -1 }

func (c *ReleaseCommand) Run(ctx *cli.Context) error {
	if !release.IsNimbusRepo(ctx.AppRoot) {
		ctx.UI.Errorf("Not the Nimbus repo. Run 'nimbus release' from github.com/CodeSyncr/nimbus")
		return nil
	}
	bump := release.BumpPatch
	if len(ctx.Args) > 0 {
		bump = release.BumpType(strings.ToLower(ctx.Args[0]))
		if bump != release.BumpPatch && bump != release.BumpMinor && bump != release.BumpMajor {
			ctx.UI.Errorf("Invalid bump %q. Use: patch, minor, major", ctx.Args[0])
			return nil
		}
	}
	oldVer := version.Nimbus
	newVer, err := release.BumpVersion(oldVer, bump)
	if err != nil {
		ctx.UI.Errorf("Failed to bump version: %v", err)
		return err
	}
	ctx.UI.Infof("Bumping %s -> %s", oldVer, newVer)

	if err := release.UpdateVersionConstant(ctx.AppRoot, newVer); err != nil {
		ctx.UI.Errorf("Failed to update version constant: %v", err)
		return err
	}
	ctx.UI.Successf("Updated internal/version/version.go")

	if err := release.GitTag(ctx.AppRoot, newVer); err != nil {
		ctx.UI.Errorf("Git tag failed: %v", err)
		return err
	}
	ctx.UI.Successf("Created git tag %s", newVer)

	msg := fmt.Sprintf("Next: git add -A && git commit -m \"release %s\" && git push && git push --tags", newVer)
	ctx.UI.Panel("Next Steps", msg)

	return nil
}
