package ai

import (
	"encoding/json"
	"reflect"
	"testing"
)

type schemaStep struct {
	Do      string  `json:"do" description:"what to do"`
	Text    string  `json:"text,omitempty"`
	Seconds float64 `json:"seconds,omitempty"`
}

type schemaInput struct {
	Page  string       `json:"page,omitempty"`
	Steps []schemaStep `json:"steps" description:"in order"`
	Tags  []string     `json:"tags,omitempty"`
}

// A list says what goes in it, and an omitempty field is optional: without
// either, a model guesses a list's shape and fills in fields it should leave.
func TestToolSchemaDescribesListsAndOptionalFields(t *testing.T) {
	var s struct {
		Properties map[string]struct {
			Type  string         `json:"type"`
			Items map[string]any `json:"items"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(structToJSONSchema(reflectTypeOf[schemaInput]()), &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Required) != 1 || s.Required[0] != "steps" {
		t.Errorf("required = %v, want only [steps]", s.Required)
	}
	steps := s.Properties["steps"]
	if steps.Type != "array" || steps.Items["type"] != "object" {
		t.Fatalf("steps = %+v", steps)
	}
	props, _ := steps.Items["properties"].(map[string]any)
	if _, ok := props["do"]; !ok || props["seconds"].(map[string]any)["type"] != "number" {
		t.Errorf("step item properties = %v", props)
	}
	if req, _ := steps.Items["required"].([]any); len(req) != 1 || req[0] != "do" {
		t.Errorf("step item required = %v, want [do]", steps.Items["required"])
	}
	if s.Properties["tags"].Items["type"] != "string" {
		t.Errorf("tags items = %v", s.Properties["tags"].Items)
	}
}

func reflectTypeOf[T any]() reflect.Type { var v T; return reflect.TypeOf(v) }
