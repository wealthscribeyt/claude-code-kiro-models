package reqconv

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/d-kuro/kirocc/internal/anthropic"
)

func TestConvertTools_Basic(t *testing.T) {
	tools := []anthropic.Tool{
		{
			Name:        "get_weather",
			Description: "Get weather",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"city": map[string]any{"type": "string"}},
				"required":   []any{"city"},
			},
		},
	}
	entries := ConvertTools(tools, nil)
	if len(entries) != 1 {
		t.Fatalf("got %d entries", len(entries))
	}
	spec := entries[0].ToolSpecification
	if spec.Name != "get_weather" || spec.Description != "Get weather" {
		t.Fatalf("unexpected spec: %+v", spec)
	}
}

func TestConvertTools_EmptyDescription(t *testing.T) {
	tools := []anthropic.Tool{{Name: "my_tool", InputSchema: map[string]any{}}}
	entries := ConvertTools(tools, nil)
	if entries[0].ToolSpecification.Description != "Tool: my_tool" {
		t.Fatalf("got %q", entries[0].ToolSpecification.Description)
	}
}

func TestConvertTools_LongDescription(t *testing.T) {
	longDesc := strings.Repeat("x", 50001)
	tools := []anthropic.Tool{{Name: "Bash", Description: longDesc, InputSchema: map[string]any{}}}
	entries := ConvertTools(tools, nil)
	if entries[0].ToolSpecification.Description != longDesc {
		t.Fatal("long description should be kept as-is")
	}
}

func TestConvertTools_LongNameShortened(t *testing.T) {
	longName := strings.Repeat("a", 65)
	tools := []anthropic.Tool{{Name: longName, InputSchema: map[string]any{}}}
	nameMap := NewToolNameMap()
	entries := ConvertTools(tools, nameMap)
	if len(entries) != 1 {
		t.Fatalf("got %d entries", len(entries))
	}
	short := entries[0].ToolSpecification.Name
	if len(short) > maxToolNameLen {
		t.Fatalf("shortened name still too long: %d chars", len(short))
	}
	if nameMap.Restore(short) != longName {
		t.Fatal("reverse mapping failed")
	}
}

func TestDereferenceSchema_InlinesLocalRef(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"v": map[string]any{"$ref": "#/$defs/AntiHype"},
		},
		"$defs": map[string]any{
			"AntiHype": map[string]any{"type": "string", "maxLength": 5},
		},
	}
	got := DereferenceSchema(schema)
	props, _ := got["properties"].(map[string]any)
	v, _ := props["v"].(map[string]any)
	if v["type"] != "string" || v["maxLength"] != 5 {
		t.Errorf("ref not inlined: %v", v)
	}
	if _, ok := got["$defs"]; ok {
		t.Errorf("$defs should be dropped after inlining")
	}
}

func TestDereferenceSchema_CycleTerminates(t *testing.T) {
	schema := map[string]any{
		"$defs": map[string]any{
			"A": map[string]any{"$ref": "#/$defs/A"},
		},
		"type": "object",
		"properties": map[string]any{
			"v": map[string]any{"$ref": "#/$defs/A"},
		},
	}
	_ = DereferenceSchema(schema) // must return, not hang
}

func TestDereferenceSchema_MissingRefDropsKey(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"v": map[string]any{"$ref": "#/$defs/Nope"},
		},
	}
	got := DereferenceSchema(schema)
	props, _ := got["properties"].(map[string]any)
	if v, _ := props["v"].(map[string]any); len(v) != 0 {
		t.Errorf("missing ref should resolve empty, got %v", v)
	}
}

func TestSanitizeJSONSchema_KeepsAdditionalProperties(t *testing.T) {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]any{"x": map[string]any{"type": "string", "additionalProperties": true}},
	}
	got := SanitizeJSONSchema(schema)
	if got["additionalProperties"] != false {
		t.Fatal("additionalProperties should be kept")
	}
	props := got["properties"].(map[string]any)
	x := props["x"].(map[string]any)
	if x["additionalProperties"] != true {
		t.Fatal("nested additionalProperties should be kept")
	}
}

func TestSanitizeJSONSchema_RemovesEmptyRequired(t *testing.T) {
	schema := map[string]any{"type": "object", "required": []any{}}
	got := SanitizeJSONSchema(schema)
	if _, ok := got["required"]; ok {
		t.Fatal("empty required should be removed")
	}
}

func TestSanitizeJSONSchema_KeepsNonEmptyRequired(t *testing.T) {
	schema := map[string]any{"type": "object", "required": []any{"x"}}
	got := SanitizeJSONSchema(schema)
	if _, ok := got["required"]; !ok {
		t.Fatal("non-empty required should be kept")
	}
}

func TestSanitizeJSONSchema_ConstToEnum(t *testing.T) {
	schema := map[string]any{"const": "hello"}
	got := SanitizeJSONSchema(schema)
	if _, ok := got["const"]; ok {
		t.Fatal("const should be removed")
	}
	enum, ok := got["enum"].([]any)
	if !ok || len(enum) != 1 || enum[0] != "hello" {
		t.Fatalf("expected enum: [hello], got %v", got["enum"])
	}
}

func TestSanitizeJSONSchema_Nil(t *testing.T) {
	got := SanitizeJSONSchema(nil)
	if got == nil {
		t.Fatal("should return empty map, not nil")
	}
}

func TestSanitizeJSONSchema_FlattensAnyOfEnums(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{
				"anyOf": []any{
					map[string]any{"enum": []any{"pending", "in_progress", "completed"}, "type": "string"},
					map[string]any{"enum": []any{"deleted"}, "type": "string"},
				},
			},
		},
	}
	got := SanitizeJSONSchema(schema)
	props := got["properties"].(map[string]any)
	status := props["status"].(map[string]any)
	if _, ok := status["anyOf"]; ok {
		t.Fatal("anyOf should be flattened")
	}
	enum, ok := status["enum"].([]any)
	if !ok {
		t.Fatal("expected enum field")
	}
	if len(enum) != 4 {
		t.Fatalf("expected 4 enum values, got %d: %v", len(enum), enum)
	}
	if status["type"] != "string" {
		t.Fatalf("expected type string, got %v", status["type"])
	}
}

func TestSanitizeJSONSchema_AnyOfNullable_NoWarning(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	old := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(old)

	schema := map[string]any{
		"anyOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "null"},
		},
	}
	got := SanitizeJSONSchema(schema)

	if got["type"] != "string" {
		t.Fatalf("expected type string, got %v", got["type"])
	}
	if _, ok := got["anyOf"]; ok {
		t.Fatal("anyOf should be removed")
	}
	if buf.Len() > 0 {
		t.Fatalf("expected no warning for nullable anyOf, got: %q", buf.String())
	}
}

func TestSanitizeJSONSchema_OneOfNullable_NoWarning(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	old := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(old)

	schema := map[string]any{
		"oneOf": []any{
			map[string]any{"type": "null"},
			map[string]any{"type": "integer", "description": "count"},
		},
	}
	got := SanitizeJSONSchema(schema)

	if got["type"] != "integer" {
		t.Fatalf("expected type integer, got %v", got["type"])
	}
	if got["description"] != "count" {
		t.Fatalf("expected description preserved, got %v", got["description"])
	}
	if buf.Len() > 0 {
		t.Fatalf("expected no warning for nullable oneOf, got: %q", buf.String())
	}
}

func TestSanitizeJSONSchema_AnyOfNullableMultiNonNull_Passthrough(t *testing.T) {
	// Null branch dropped; remaining 2 branches pass through verbatim.
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "integer"},
			map[string]any{"type": "null"},
		},
	}
	got := SanitizeJSONSchema(schema)
	branches, ok := got["anyOf"].([]any)
	if !ok || len(branches) != 3 {
		t.Fatalf("expected anyOf with 3 branches kept, got %v", got)
	}
}

func TestSanitizeJSONSchema_AnyOfNonEnum_NoWarning(t *testing.T) {
	// Multi-branch anyOf passes through (proven accepted live): no warning.
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "number"},
		},
	}
	got := SanitizeJSONSchema(schema)
	if _, ok := got["anyOf"]; !ok {
		t.Fatal("anyOf branches should be kept")
	}
}

func TestSanitizeJSONSchema_OneOfNonEnum_NoWarning(t *testing.T) {
	schema := map[string]any{
		"oneOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "number"},
		},
	}
	got := SanitizeJSONSchema(schema)
	if _, ok := got["oneOf"]; !ok {
		t.Fatal("oneOf branches should be kept")
	}
}

func TestSanitizeJSONSchema_AnyOfEnum_NoWarning(t *testing.T) {
	// When all branches are enum-based, no warning should be logged.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	old := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(old)

	schema := map[string]any{
		"anyOf": []any{
			map[string]any{"enum": []any{"a"}, "type": "string"},
			map[string]any{"enum": []any{"b"}, "type": "string"},
		},
	}
	SanitizeJSONSchema(schema)

	if buf.Len() > 0 {
		t.Fatalf("expected no warning for enum-based anyOf, got: %q", buf.String())
	}
}

func TestSanitizeJSONSchema_AnyOfNonEnum_KeepsAllBranches(t *testing.T) {
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{"type": "string", "description": "a string"},
			map[string]any{"type": "number"},
		},
	}
	got := SanitizeJSONSchema(schema)
	branches, ok := got["anyOf"].([]any)
	if !ok || len(branches) != 2 {
		t.Fatalf("expected 2 anyOf branches kept, got %v", got)
	}
	if _, ok := got["type"]; ok {
		t.Fatal("no branch type should leak to top level")
	}
}

func TestSanitizeJSONSchema_AnyOfConstBranches(t *testing.T) {
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{"const": "A"},
			map[string]any{"const": "B"},
			map[string]any{"const": "C"},
		},
	}
	got := SanitizeJSONSchema(schema)
	if _, ok := got["anyOf"]; ok {
		t.Fatal("anyOf should be flattened")
	}
	enum, ok := got["enum"].([]any)
	if !ok {
		t.Fatalf("expected enum field, got %v", got)
	}
	if len(enum) != 3 {
		t.Fatalf("expected 3 enum values, got %d: %v", len(enum), enum)
	}
}

func TestSanitizeJSONSchema_AnyOfMixedTypes_NoType(t *testing.T) {
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{"enum": []any{"hello"}, "type": "string"},
			map[string]any{"enum": []any{42}, "type": "integer"},
		},
	}
	got := SanitizeJSONSchema(schema)
	enum, ok := got["enum"].([]any)
	if !ok {
		t.Fatalf("expected enum field, got %v", got)
	}
	if len(enum) != 2 {
		t.Fatalf("expected 2 enum values, got %d: %v", len(enum), enum)
	}
	if _, ok := got["type"]; ok {
		t.Fatal("type should be omitted for mixed-type enums")
	}
}

func TestSanitizeJSONSchema_AllOfMerged(t *testing.T) {
	schema := map[string]any{
		"allOf": []any{
			map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}},
			map[string]any{"required": []any{"a"}},
		},
	}
	got := SanitizeJSONSchema(schema)
	if _, ok := got["allOf"]; ok {
		t.Fatal("allOf should be removed")
	}
	if got["type"] != "object" {
		t.Fatalf("expected type object, got %v", got["type"])
	}
	req, ok := got["required"].([]any)
	if !ok || len(req) != 1 {
		t.Fatalf("expected required [a], got %v", got["required"])
	}
}

func TestSanitizeJSONSchema_KeepsValidationKeywords(t *testing.T) {
	keywords := []string{
		"format", "pattern",
		"minLength", "maxLength",
		"minimum", "maximum",
		"minItems", "maxItems",
		"uniqueItems", "multipleOf",
		"not",
	}
	for _, kw := range keywords {
		schema := map[string]any{"type": "string", kw: "value"}
		got := SanitizeJSONSchema(schema)
		if _, ok := got[kw]; !ok {
			t.Fatalf("%q should be kept", kw)
		}
		if got["type"] != "string" {
			t.Fatalf("type should be preserved when keeping %q", kw)
		}
	}
}

func TestSanitizeJSONSchema_RemovesDollarSchema(t *testing.T) {
	schema := map[string]any{"type": "object", "$schema": "http://json-schema.org/draft-07/schema#"}
	got := SanitizeJSONSchema(schema)
	if _, ok := got["$schema"]; ok {
		t.Fatal("$schema should be removed")
	}
}

func TestSanitizeJSONSchema_KeepsPatternProperties(t *testing.T) {
	schema := map[string]any{"type": "object", "patternProperties": map[string]any{}}
	got := SanitizeJSONSchema(schema)
	if _, ok := got["patternProperties"]; !ok {
		t.Fatal("patternProperties should be kept")
	}
}

func TestSanitizeJSONSchema_AnyOfKeepsSiblings_Deterministic(t *testing.T) {
	// anyOf passes through AND sibling type stays: combinators no longer
	// override since nothing is lossy anymore.
	schema := map[string]any{
		"type": "object",
		"anyOf": []any{
			map[string]any{"type": "string", "description": "a string"},
			map[string]any{"type": "number"},
		},
	}
	// Run multiple times to catch map iteration order flakiness.
	for i := range 100 {
		got := SanitizeJSONSchema(schema)
		if got["type"] != "object" {
			t.Fatalf("iteration %d: type = %v, want object (sibling preserved)", i, got["type"])
		}
		if _, ok := got["anyOf"]; !ok {
			t.Fatalf("iteration %d: anyOf should be kept", i)
		}
	}
}

func TestEnsureObjectRoot_AlreadyObject(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}}
	got := EnsureObjectRoot(schema)
	if got["type"] != "object" || got["properties"] == nil {
		t.Fatalf("should be unchanged: %v", got)
	}
}

func TestEnsureObjectRoot_StringType_Wraps(t *testing.T) {
	schema := map[string]any{"type": "string", "description": "search query"}
	got := EnsureObjectRoot(schema)
	if got["type"] != "object" {
		t.Fatalf("expected object root, got %v", got["type"])
	}
	props, ok := got["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties")
	}
	input, ok := props["input"].(map[string]any)
	if !ok {
		t.Fatal("expected input property")
	}
	if input["type"] != "string" {
		t.Fatalf("wrapped schema lost type: %v", input)
	}
}

func TestEnsureObjectRoot_NoType_AddsObject(t *testing.T) {
	schema := map[string]any{"properties": map[string]any{"x": map[string]any{}}}
	got := EnsureObjectRoot(schema)
	if got["type"] != "object" {
		t.Fatalf("expected type added, got %v", got["type"])
	}
	// Should not wrap — just add the type field.
	if _, ok := got["properties"].(map[string]any)["x"]; !ok {
		t.Fatal("original properties lost")
	}
}

func TestEnsureObjectRoot_Empty(t *testing.T) {
	got := EnsureObjectRoot(map[string]any{})
	if got["type"] != "object" {
		t.Fatalf("expected object for empty schema, got %v", got)
	}
}

func TestEnsureObjectRoot_ArrayType_Wraps(t *testing.T) {
	schema := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	got := EnsureObjectRoot(schema)
	if got["type"] != "object" {
		t.Fatalf("expected object root, got %v", got["type"])
	}
	props := got["properties"].(map[string]any)
	input := props["input"].(map[string]any)
	if input["type"] != "array" {
		t.Fatalf("wrapped schema lost type: %v", input)
	}
}
