package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/router"
	"github.com/CodeSyncr/nimbus/validation"
	"github.com/spf13/cobra"
)

func init() {
	cli.RegisterCommand(&GenClientCommand{})
	cli.RegisterRootAttach(attachGenClientSubcommand)
}

func attachGenClientSubcommand(root *cobra.Command) {
	gen := &cobra.Command{
		Use:   "gen",
		Short: "Code generation commands",
	}
	clientCmd := &cobra.Command{
		Use:   "client",
		Short: (&GenClientCommand{}).Description(),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out, _ := cmd.Flags().GetString("out")
			ctx := cli.NewContext(cmd, args)
			return (&GenClientCommand{Out: out}).Run(ctx)
		},
	}
	clientCmd.Flags().String("out", ".nimbus-client", "Output directory for generated files")
	gen.AddCommand(clientCmd)
	root.AddCommand(gen)
}

// GenClientCommand generates a type-safe TypeScript client manifest for Nimbus Hive.
type GenClientCommand struct {
	Out string // output directory, defaults to ".nimbus-client"
}

func (c *GenClientCommand) Name() string        { return "gen:client" }
func (c *GenClientCommand) Description() string { return "Generate TypeScript client manifest for Nimbus Hive" }
func (c *GenClientCommand) Args() int           { return 0 }

// RouteManifestEntry represents a single route in the manifest.
type RouteManifestEntry struct {
	Method string                      `json:"method"`
	Path   string                      `json:"path"`
	Name   string                      `json:"name"`
	Params []string                    `json:"params,omitempty"`
	Schema map[string]RuleManifestInfo `json:"schema,omitempty"`
}

type RuleManifestInfo struct {
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// TypeScriptRuleInfo describes a field's type info for code generation.
type TypeScriptRuleInfo struct {
	TSType   string
	Required bool
}

// ruleToTS converts a validation.Rule to its TypeScript type descriptor.
// It uses the TypeScriptType() and IsRequired() methods we added to each rule.
func ruleToTS(rule validation.Rule) TypeScriptRuleInfo {
	type tsTyper interface {
		TypeScriptType() string
		IsRequired() bool
	}
	if t, ok := rule.(tsTyper); ok {
		return TypeScriptRuleInfo{TSType: t.TypeScriptType(), Required: t.IsRequired()}
	}
	// Fallback for custom rules that don't implement the interface.
	return TypeScriptRuleInfo{TSType: "unknown", Required: false}
}

// extractPathParams returns all param names from a route path, supporting both
// ":id" and "{id}" syntaxes. Delegates to the router so the generator and the
// runtime always agree.
func extractPathParams(path string) []string {
	return router.PathParams(path)
}

// routeNameToIdentifier converts "posts.store" → "PostsStore" for interface names.
func routeNameToIdentifier(name string) string {
	parts := strings.Split(name, ".")
	for i, p := range parts {
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// routeNameToProxy converts "posts.store" → "posts.store" (identity, used for registry key).
// Segments with underscores become camelCase for proxy access: new_post → newPost.
func routeNameToProxy(name string) string {
	parts := strings.Split(name, ".")
	for i, p := range parts {
		segs := strings.Split(p, "_")
		for j, s := range segs {
			if j == 0 {
				segs[j] = s
			} else {
				segs[j] = strings.ToUpper(s[:1]) + s[1:]
			}
		}
		parts[i] = strings.Join(segs, "")
	}
	return strings.Join(parts, ".")
}

func (c *GenClientCommand) Run(ctx *cli.Context) error {
	outDir := c.Out
	if outDir == "" {
		outDir = ".nimbus-client"
	}

	// Routes are discovered from the manifest the app writes at startup when
	// NIMBUS_DUMP_ROUTES=1 (a CLI process cannot see routes registered at
	// runtime in Go code, so the app must dump them first).
	manifestPath := filepath.Join(outDir, router.ManifestFileName)

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf(
			"gen:client: no route manifest at %s.\n\n"+
				"Generate it by running your app once with NIMBUS_DUMP_ROUTES=1 (it writes the\n"+
				"manifest and exits without serving), then re-run this command:\n\n"+
				"  NIMBUS_DUMP_ROUTES=1 go run .\n"+
				"  nimbus gen:client",
			manifestPath)
	}

	var entries []RouteManifestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("gen:client: could not parse %s: %w", manifestPath, err)
	}
	if len(entries) == 0 {
		return fmt.Errorf(
			"gen:client: %s contains no routes. Re-run NIMBUS_DUMP_ROUTES=1 after registering routes",
			manifestPath)
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("gen:client: could not create output dir %s: %w", outDir, err)
	}

	if err := c.writeDataDTS(outDir, entries); err != nil {
		return err
	}

	if err := c.writeRegistry(outDir, entries); err != nil {
		return err
	}

	ctx.UI.Successf("Generated .nimbus-client/registry.ts and .nimbus-client/data.d.ts (%d routes)", len(entries))
	ctx.UI.Infof("")
	ctx.UI.Infof("Next steps:")
	ctx.UI.Infof("  1. Install @codesyncr/hive:  npm install @codesyncr/hive")
	ctx.UI.Infof("  2. Create your client:")
	ctx.UI.Infof("       import { createHive } from '@codesyncr/hive'")
	ctx.UI.Infof("       import { registry } from './.nimbus-client/registry'")
	ctx.UI.Infof("       export const client = createHive({ baseUrl: 'http://localhost:8080', registry })")
	ctx.UI.Infof("")
	ctx.UI.Infof("  3. Call API routes type-safely:")
	ctx.UI.Infof("       const post = await client.api.posts.store({ body: { title: 'Hello' } })")

	return nil
}

// writeDataDTS writes interface declarations for validation schemas.
func (c *GenClientCommand) writeDataDTS(outDir string, entries []RouteManifestEntry) error {
	var sb strings.Builder
	sb.WriteString("// Auto-generated by `nimbus gen:client` — DO NOT EDIT\n")
	sb.WriteString("// Re-run `nimbus gen:client` after changing routes.\n\n")

	for _, e := range entries {
		if e.Name == "" || len(e.Schema) == 0 {
			continue
		}
		id := routeNameToIdentifier(e.Name)
		sb.WriteString(fmt.Sprintf("export interface %sBody {\n", id))

		// Sort keys for deterministic output
		var keys []string
		for k := range e.Schema {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			rule := e.Schema[k]
			if rule.Required {
				sb.WriteString(fmt.Sprintf("  %s: %s;\n", k, rule.Type))
			} else {
				sb.WriteString(fmt.Sprintf("  %s?: %s;\n", k, rule.Type))
			}
		}
		sb.WriteString("}\n\n")
	}

	dtsPath := filepath.Join(outDir, "data.d.ts")
	return os.WriteFile(dtsPath, []byte(sb.String()), 0644)
}

// writeRegistry writes the .nimbus-client/registry.ts file.
func (c *GenClientCommand) writeRegistry(outDir string, entries []RouteManifestEntry) error {
	var sb strings.Builder

	sb.WriteString("// Auto-generated by `nimbus gen:client` — DO NOT EDIT\n")
	sb.WriteString("// Re-run `nimbus gen:client` after changing routes.\n\n")
	sb.WriteString("import type * as Data from './data.js'\n\n")

	// registry const
	sb.WriteString("export const registry = {\n")

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	for _, e := range entries {
		paramsStr := formatParamsTS(e.Params)
		method := strings.ToUpper(e.Method)

		var typeParams string
		if len(e.Params) > 0 {
			typeParams = fmt.Sprintf("{ %s }", formatParamsTypeLiteral(e.Params))
		} else {
			typeParams = "Record<string, never>"
		}

		var typeBody string
		if len(e.Schema) > 0 {
			id := routeNameToIdentifier(e.Name)
			typeBody = fmt.Sprintf("Data.%sBody", id)
		} else {
			typeBody = "Record<string, never>"
		}

		sb.WriteString(fmt.Sprintf("  %q: {\n", e.Name))
		sb.WriteString(fmt.Sprintf("    method: %q as const,\n", method))
		sb.WriteString(fmt.Sprintf("    path: %q,\n", e.Path))
		sb.WriteString(fmt.Sprintf("    params: %s as Record<string, 'string'>,\n", paramsStr))
		sb.WriteString("    types: {} as {\n")
		sb.WriteString(fmt.Sprintf("      params: %s;\n", typeParams))
		sb.WriteString("      query: Record<string, never>;\n")
		sb.WriteString(fmt.Sprintf("      body: %s;\n", typeBody))
		sb.WriteString("      response: any;\n")
		sb.WriteString("    }\n")
		sb.WriteString("  },\n")
	}
	sb.WriteString("} as const\n")

	registryPath := filepath.Join(outDir, "registry.ts")
	return os.WriteFile(registryPath, []byte(sb.String()), 0644)
}

func formatParamsTS(params []string) string {
	if len(params) == 0 {
		return "{}"
	}
	parts := make([]string, len(params))
	for i, p := range params {
		parts[i] = fmt.Sprintf("%q: 'string'", p)
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func formatParamsTypeLiteral(params []string) string {
	parts := make([]string, len(params))
	for i, p := range params {
		parts[i] = fmt.Sprintf("%s: string", p)
	}
	return strings.Join(parts, "; ")
}

// WriteRouteManifest writes the route manifest JSON to outDir.
//
// Deprecated: the app writes this itself at startup when NIMBUS_DUMP_ROUTES=1.
// Retained as a thin wrapper for direct callers; use router.WriteManifest.
func WriteRouteManifest(routes *router.Router, outDir string) error {
	return router.WriteManifest(routes, outDir)
}
