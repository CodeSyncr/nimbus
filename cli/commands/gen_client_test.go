package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenClient_TSTypeGeneration(t *testing.T) {
	// Sample entries
	entries := []RouteManifestEntry{
		{
			Name:   "posts.index",
			Method: "GET",
			Path:   "/posts",
			Params: []string{},
			Response: &ResponseManifestType{
				Kind: "array",
				Elem: &ResponseManifestType{
					Kind: "struct",
					Fields: map[string]ResponseManifestType{
						"id":    {Kind: "primitive", Type: "number"},
						"title": {Kind: "primitive", Type: "string"},
					},
				},
			},
		},
		{
			Name:   "posts.store",
			Method: "POST",
			Path:   "/posts",
			Params: []string{},
			Schema: map[string]RuleManifestInfo{
				"title": {Type: "string", Required: true},
			},
			Response: &ResponseManifestType{
				Kind: "struct",
				Fields: map[string]ResponseManifestType{
					"id":    {Kind: "primitive", Type: "number"},
					"title": {Kind: "primitive", Type: "string"},
				},
			},
		},
	}

	tempDir := t.TempDir()
	cmd := &GenClientCommand{Out: tempDir}

	err := cmd.writeDataDTS(tempDir, entries)
	if err != nil {
		t.Fatalf("writeDataDTS failed: %v", err)
	}

	dtsData, err := os.ReadFile(filepath.Join(tempDir, "data.d.ts"))
	if err != nil {
		t.Fatalf("could not read data.d.ts: %v", err)
	}
	dtsStr := string(dtsData)

	// Verify data.d.ts
	if !strings.Contains(dtsStr, "export interface PostsStoreBody") {
		t.Errorf("missing PostsStoreBody in data.d.ts: %s", dtsStr)
	}
	if !strings.Contains(dtsStr, "export type PostsIndexResponse = Array<{\n  id: number;\n  title: string;\n}>;") {
		t.Errorf("missing or incorrect PostsIndexResponse in data.d.ts: %s", dtsStr)
	}
	if !strings.Contains(dtsStr, "export type PostsStoreResponse = {\n  id: number;\n  title: string;\n};") {
		t.Errorf("missing or incorrect PostsStoreResponse in data.d.ts: %s", dtsStr)
	}

	err = cmd.writeRegistry(tempDir, entries)
	if err != nil {
		t.Fatalf("writeRegistry failed: %v", err)
	}

	registryData, err := os.ReadFile(filepath.Join(tempDir, "registry.ts"))
	if err != nil {
		t.Fatalf("could not read registry.ts: %v", err)
	}
	registryStr := string(registryData)

	// Verify registry.ts
	if !strings.Contains(registryStr, `response: Data.PostsIndexResponse;`) {
		t.Errorf("missing response reference in registry.ts: %s", registryStr)
	}
	if !strings.Contains(registryStr, `response: Data.PostsStoreResponse;`) {
		t.Errorf("missing response reference in registry.ts: %s", registryStr)
	}
}
