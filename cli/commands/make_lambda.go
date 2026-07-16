package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CodeSyncr/nimbus/cli"
)

func init() {
	cli.RegisterCommand(&MakeLambdaCommand{})
}

// MakeLambdaCommand scaffolds an AWS Lambda deployment target for a Nimbus app:
// a cmd/lambda entry point that serves the router via the serverless adapter,
// a SAM template with a Function URL, and a Makefile build rule.
type MakeLambdaCommand struct{}

func (c *MakeLambdaCommand) Name() string        { return "make:lambda" }
func (c *MakeLambdaCommand) Description() string  { return "Scaffold an AWS Lambda deployment target" }
func (c *MakeLambdaCommand) Args() int           { return 0 }

func (c *MakeLambdaCommand) Run(ctx *cli.Context) error {
	root := ctx.AppRoot
	if root == "" {
		root = "."
	}

	module := moduleNameFromGoMod(filepath.Join(root, "go.mod"))
	if module == "" {
		return fmt.Errorf("make:lambda: could not read module name from go.mod (run inside a Nimbus app)")
	}

	created, skipped, err := writeLambdaFiles(root, module)
	if err != nil {
		return err
	}
	for _, p := range created {
		ctx.UI.Successf("created %s", p)
	}
	for _, p := range skipped {
		ctx.UI.Warnf("skipping existing %s", p)
	}

	// Pull the AWS Lambda runtime dependency into the app module.
	ctx.UI.Infof("Adding github.com/aws/aws-lambda-go ...")
	cmd := exec.Command("go", "get", "github.com/aws/aws-lambda-go@latest")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		ctx.UI.Warnf("could not run `go get aws-lambda-go` automatically: %v\n%s", err, string(out))
		ctx.UI.Infof("Run it manually: go get github.com/aws/aws-lambda-go@latest")
	}

	ctx.UI.Panel("AWS Lambda scaffolded", strings.TrimSpace(`
Entry point : cmd/lambda/main.go   (serves app.Router via the serverless adapter)
Template    : template.yaml        (SAM — Lambda Function URL, provided.al2023, arm64)
Build       : Makefile

Deploy with the AWS SAM CLI:
  sam build
  sam deploy --guided

Important for serverless:
  • Use Postgres/MySQL over a connection POOLER (Supabase pooler, RDS Proxy,
    PgBouncer) — direct connections are exhausted by Lambda concurrency.
  • SQLite will NOT work (ephemeral filesystem + CGO). Set DB_DRIVER=postgres.
  • Set APP_ENV=production so boot does not run auto-migrations per cold start;
    run "nimbus migrate" from CI/CD instead.
  • Queue workers, the scheduler, and WebSocket/SSE need a long-running process;
    they do not run inside request-scoped Lambda invocations.`))
	return nil
}

// writeLambdaFiles renders the Lambda entrypoint, SAM template, and Makefile
// into root, skipping any that already exist. Returned paths are root-relative.
// Shared by `make:lambda` and `nimbus new --lambda`.
func writeLambdaFiles(root, module string) (created, skipped []string, err error) {
	fnName := lambdaLogicalName(filepath.Base(root))
	files := map[string]string{
		filepath.Join(root, "cmd", "lambda", "main.go"): renderTemplate(lambdaMainTmpl, map[string]string{
			"BootImport": module + "/bin",
		}),
		filepath.Join(root, "template.yaml"): renderTemplate(lambdaSAMTmpl, map[string]string{"FnName": fnName}),
		filepath.Join(root, "Makefile"):      renderTemplate(lambdaMakefileTmpl, map[string]string{"FnName": fnName}),
	}
	// Deterministic order for stable output.
	for _, rel := range []string{
		filepath.Join("cmd", "lambda", "main.go"), "template.yaml", "Makefile",
	} {
		path := filepath.Join(root, rel)
		if _, statErr := os.Stat(path); statErr == nil {
			skipped = append(skipped, rel)
			continue
		}
		if mkErr := os.MkdirAll(filepath.Dir(path), 0755); mkErr != nil {
			return created, skipped, mkErr
		}
		if wErr := os.WriteFile(path, []byte(files[path]), 0644); wErr != nil {
			return created, skipped, wErr
		}
		created = append(created, rel)
	}
	return created, skipped, nil
}

// moduleNameFromGoMod extracts the module path from a go.mod file.
func moduleNameFromGoMod(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

// lambdaLogicalName turns an app dir name into a CloudFormation-safe logical ID.
func lambdaLogicalName(name string) string {
	var b strings.Builder
	upperNext := true
	for _, r := range name {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			if upperNext && r >= 'a' && r <= 'z' {
				b.WriteRune(r - 32)
			} else {
				b.WriteRune(r)
			}
			upperNext = false
		default:
			upperNext = true
		}
	}
	out := b.String()
	if out == "" || (out[0] >= '0' && out[0] <= '9') {
		out = "App" + out
	}
	return out + "Function"
}

func relPath(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}

// renderTemplate does simple {{Key}} substitution (no text/template, to keep
// the Go/YAML/Makefile bodies verbatim and brace-safe).
func renderTemplate(tmpl string, vars map[string]string) string {
	out := tmpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}

const lambdaMainTmpl = `// Command lambda serves the Nimbus application on AWS Lambda.
//
// It reuses the same Boot() as the HTTP server, then serves the router through
// the serverless adapter instead of opening a listener. Build with:
//
//	GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap ./cmd/lambda
package main

import (
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/CodeSyncr/nimbus/serverless"

	"{{BootImport}}"
)

func main() {
	app := bin.Boot()          // build the app: routes + middleware
	if err := app.Boot(); err != nil { // run providers/plugins (no HTTP listener)
		fmt.Fprintf(os.Stderr, "boot failed: %v\n", err)
		os.Exit(1)
	}
	lambda.Start(serverless.Lambda(app.Router))
}
`

const lambdaSAMTmpl = `AWSTemplateFormatVersion: '2010-09-09'
Transform: AWS::Serverless-2016-10-31
Description: Nimbus application on AWS Lambda (Function URL)

Globals:
  Function:
    Timeout: 30
    MemorySize: 512

Resources:
  {{FnName}}:
    Type: AWS::Serverless::Function
    Metadata:
      BuildMethod: makefile
    Properties:
      CodeUri: .
      Handler: bootstrap
      Runtime: provided.al2023
      Architectures:
        - arm64
      Environment:
        Variables:
          APP_ENV: production
          # Point these at a CONNECTION POOLER, not the database directly.
          # DB_DRIVER: postgres
          # DB_DSN: postgres://user:pass@pooler-host:6543/dbname
          # APP_KEY: <run: nimbus key:generate>
      FunctionUrlConfig:
        AuthType: NONE   # switch to AWS_IAM to require signed requests

Outputs:
  FunctionUrl:
    Description: Public URL of the Nimbus Lambda
    Value: !GetAtt {{FnName}}Url.FunctionUrl
`

const lambdaMakefileTmpl = `# SAM builds each function by invoking build-<LogicalId>.
build-{{FnName}}:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o $(ARTIFACTS_DIR)/bootstrap ./cmd/lambda
`
