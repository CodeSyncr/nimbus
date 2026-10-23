package commands

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/cli/generators"
)

func init() { cli.RegisterCommand(&MakeGRPC{}) }

// MakeGRPC scaffolds a gRPC service: the .proto file that describes it and
// the Go type that implements it, wired for plugins/grpc.
type MakeGRPC struct{}

func (c *MakeGRPC) Name() string { return "make:grpc" }
func (c *MakeGRPC) Description() string {
	return "Create a gRPC service: a .proto file and its Go implementation"
}
func (c *MakeGRPC) Args() int { return 1 }
func (c *MakeGRPC) Run(ctx *cli.Context) error {
	name := ctx.Args[0]
	snake := cli.ToSnake(name)
	module := "app"
	if modData, err := os.ReadFile("go.mod"); err == nil {
		for _, line := range strings.Split(string(modData), "\n") {
			if m, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
				module = strings.TrimSpace(m)
				break
			}
		}
	}
	data := generators.Data{"Name": name, "Snake": snake, "Module": module}
	protoPath := filepath.Join("proto", snake, snake+".proto")
	goPath := filepath.Join("app", "services", snake+".go")
	for _, f := range []struct{ tmpl, dest string }{{"grpc/service.proto.tmpl", protoPath}, {"grpc/service.go.tmpl", goPath}} {
		if _, err := os.Stat(f.dest); err == nil {
			ctx.UI.Errorf("%s already exists; nothing was written", f.dest)
			return os.ErrExist
		}
		if err := os.MkdirAll(filepath.Dir(f.dest), 0o755); err != nil {
			return err
		}
		if err := generators.RenderToFile(f.tmpl, f.dest, data); err != nil {
			ctx.UI.Errorf("Failed to create %s: %v", f.dest, err)
			return err
		}
	}
	ctx.UI.Successf("Created %s and %s", protoPath, goPath)
	ctx.UI.Infof("Next:")
	ctx.UI.Infof("  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest")
	ctx.UI.Infof("  protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative %s", protoPath)
	ctx.UI.Infof("  then register it with plugins/grpc in bin/server.go (see the comment in %s)", goPath)
	return nil
}
