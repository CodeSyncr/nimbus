package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/CodeSyncr/nimbus/cli"
)

func init() {
	cli.RegisterCommand(&BuildCommand{})
}

type BuildCommand struct{}

func (c *BuildCommand) Name() string        { return "build" }
func (c *BuildCommand) Description() string { return "Build the application assets and binary" }
func (c *BuildCommand) Args() int           { return 0 }

func (c *BuildCommand) Run(ctx *cli.Context) error {
	ctx.UI.Infof("Building assets...")

	if err := copyDir(filepath.Join(ctx.AppRoot, "resources", "css"), filepath.Join(ctx.AppRoot, "public", "css")); err != nil {
		if !os.IsNotExist(err) {
			ctx.UI.Errorf("Failed to copy CSS: %v", err)
		}
	} else {
		ctx.UI.Successf("Copied resources/css to public/css")
	}

	if err := copyDir(filepath.Join(ctx.AppRoot, "resources", "js"), filepath.Join(ctx.AppRoot, "public", "js")); err != nil {
		if !os.IsNotExist(err) {
			ctx.UI.Errorf("Failed to copy JS: %v", err)
		}
	} else {
		ctx.UI.Successf("Copied resources/js to public/js")
	}

	return nil
}

func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}

	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
