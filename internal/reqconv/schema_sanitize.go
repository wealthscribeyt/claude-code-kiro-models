package reqconv

import (
	"maps"
	"strings"
)

// dollarKeys lists the only JSON Schema keywords the Kiro API rejects.
// Proven live (Sept 2026): every other keyword — pattern, format,
// min/max*, additionalProperties, if/then/else, not, anyOf/oneOf branches,
// propertyNames, $id/$anchor/$comment, readOnly, etc. — passes through on
// both Claude and GPT-5.6 models. A property literally named $schema crashes
// the backend (502); $defs/$ref remnants are dropped after local
// dereferencing (see DereferenceSchema).
var unsupportedKeywords = map[string]struct{}{
	"$schema": {},
	"$defs":   {},
	"$ref":    {},
}

// DereferenceSchema inlines local JSON-Schema $refs ("#/$defs/X",
// "#/properties/y/...") so MCP servers that factor schemas through $defs
// don't arrive gutted (the sanitizer drops $ref/$defs otherwise). Remote
// refs (http...) are left untouched. Cycles resolve to an empty schema
// rather than recursing forever. Purely client-side: zero backend risk.
func DereferenceSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{}
	}
	root := schema
	var resolve func(node any, seen map[string]bool) any
	resolve = func(node any, seen map[string]bool) any {
		m, ok := node.(map[string]any)
		if !ok {
			if arr, ok := node.([]any); ok {
				out := make([]any, len(arr))
				for i, item := range arr {
					out[i] = resolve(item, seen)
				}
				return out
			}
			return node
		}
		if ref, ok := m["$ref"].(string); ok && ref != "" {
			if !isLocalRef(ref) || seen[ref] {
				out := make(map[string]any, len(m))
				for k, v := range m {
					if k == "$ref" {
						continue
					}
					out[k] = resolve(v, seen)
				}
				return out
			}
			target, ok := lookupRef(root, ref)
			if !ok {
				out := make(map[string]any, len(m))
				for k, v := range m {
					if k == "$ref" {
						continue
					}
					out[k] = resolve(v, seen)
				}
				return out
			}
			seen[ref] = true
			merged := make(map[string]any)
			if tm, ok := resolve(target, seen).(map[string]any); ok {
				maps.Copy(merged, tm)
			}
			delete(seen, ref)
			for k, v := range m {
				if k == "$ref" || k == "$defs" {
					continue
				}
				merged[k] = resolve(v, seen)
			}
			return merged
		}
		out := make(map[string]any, len(m))
		for k, v := range m {
			if k == "$defs" {
				continue
			}
			out[k] = resolve(v, seen)
		}
		return out
	}
	if resolved, ok := resolve(schema, map[string]bool{}).(map[string]any); ok {
		return resolved
	}
	return map[string]any{}
}

func isLocalRef(ref string) bool {
	return len(ref) > 0 && ref[0] == '#'
}

// lookupRef follows a local JSON pointer ("#/$defs/X", "#/properties/a/b")
// from the schema root. Array indices supported; "~0"/"~1" unescaped.
func lookupRef(root map[string]any, ref string) (any, bool) {
	if !isLocalRef(ref) {
		return nil, false
	}
	var cur any = root
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch node := cur.(type) {
		case map[string]any:
			var ok bool
			cur, ok = node[part]
			if !ok {
				return nil, false
			}
		case []any:
			var idx int
			for _, c := range part {
				if c < '0' || c > '9' {
					return nil, false
				}
				idx = idx*10 + int(c-'0')
			}
			if idx >= len(node) {
				return nil, false
			}
			cur = node[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

// SanitizeJSONSchema recursively removes fields that Kiro API rejects.
func SanitizeJSONSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{}
	}

	result := make(map[string]any, len(schema))

	// First pass: process all non-combinator keys.
	for key, value := range schema {
		if _, drop := unsupportedKeywords[key]; drop {
			continue
		}
		switch key {
		case "const":
			// Deterministic when the schema carries both const and enum:
			// the explicit enum wins regardless of map iteration order.
			if _, hasEnum := schema["enum"]; !hasEnum {
				result["enum"] = []any{value}
			}
		case "required":
			if arr, ok := value.([]any); ok && len(arr) == 0 {
				continue
			}
			result[key] = value
		case "anyOf", "oneOf", "allOf":
			// Handled in second pass.
		default:
			switch v := value.(type) {
			case map[string]any:
				result[key] = SanitizeJSONSchema(v)
			case []any:
				sanitized := make([]any, len(v))
				for i, item := range v {
					if m, ok := item.(map[string]any); ok {
						sanitized[i] = SanitizeJSONSchema(m)
					} else {
						sanitized[i] = item
					}
				}
				result[key] = sanitized
			default:
				result[key] = value
			}
		}
	}

	// Second pass: apply combinators last so they deterministically override.
	// anyOf/oneOf multi-branch schemas pass through verbatim (proven accepted
	// by the backend on Claude and GPT models); only identically-semantic
	// rewrites remain: enum flattening and null-branch dropping.
	for key, value := range schema {
		switch key {
		case "anyOf", "oneOf":
			if arr, ok := value.([]any); ok && len(arr) > 0 {
				if merged := flattenEnumBranches(arr); merged != nil {
					maps.Copy(result, merged)
				} else if nonNull := dropNullBranches(arr); len(nonNull) == 1 {
					if m, ok := nonNull[0].(map[string]any); ok {
						maps.Copy(result, SanitizeJSONSchema(m))
					}
				} else {
					sanitized := make([]any, len(arr))
					for i, item := range arr {
						if m, ok := item.(map[string]any); ok {
							sanitized[i] = SanitizeJSONSchema(m)
						} else {
							sanitized[i] = item
						}
					}
					result[key] = sanitized
				}
			}
		case "allOf":
			// allOf means every branch applies simultaneously, so branches
			// must be MERGED, not overwritten: a flat maps.Copy would let a
			// later branch's top-level key ("properties", "required", ...)
			// clobber an earlier branch's, silently dropping tool schema data
			// (e.g. allOf:[{properties:{a}},{properties:{b}}] losing "a").
			if arr, ok := value.([]any); ok {
				for _, item := range arr {
					if m, ok := item.(map[string]any); ok {
						mergeSchemas(result, SanitizeJSONSchema(m))
					}
				}
			}
		}
	}

	return result
}

// mergeSchemas deep-merges src into dst for allOf composition. Nested maps
// merge recursively (properties of both branches survive); arrays append
// (required lists of both branches survive); any other conflict is resolved
// in favor of the later branch, matching the previous last-wins behavior.
func mergeSchemas(dst, src map[string]any) {
	for k, v := range src {
		if existing, ok := dst[k]; ok {
			if em, ok := existing.(map[string]any); ok {
				if sm, ok := v.(map[string]any); ok {
					mergeSchemas(em, sm)
					continue
				}
			}
			if ea, ok := existing.([]any); ok {
				if sa, ok := v.([]any); ok {
					dst[k] = append(ea, sa...)
					continue
				}
			}
		}
		dst[k] = v
	}
}

// EnsureObjectRoot wraps a sanitized schema in an object envelope if its root
// type is not "object". Call this on the final schema passed to Kiro, not during
// recursive sanitization of nested properties.
//
// Kiro/Bedrock rejects any tool whose inputSchema.json.type is not "object":
//
//	ValidationException: The value at toolConfig.tools.0.toolSpec.inputSchema.json.type
//	must be one of the following: object. reason: TOOL_SCHEMA_INVALID
//
// Anthropic's API has no such constraint, so clients (including Claude Code's
// built-in tools like WebSearch) may send schemas with type:"string" or no type
// at all. This wrapper satisfies the validation without altering semantics for
// the model.
func EnsureObjectRoot(schema map[string]any) map[string]any {
	if len(schema) == 0 {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	t, _ := schema["type"].(string)
	if t == "object" {
		return schema
	}
	if t == "" {
		// No type declared — add it rather than wrapping.
		schema["type"] = "object"
		return schema
	}
	// Non-object type: wrap in an object envelope.
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": schema,
		},
	}
}

// dropNullBranches returns branches that are not {type: "null"}.
func dropNullBranches(branches []any) []any {
	var result []any
	for _, b := range branches {
		m, ok := b.(map[string]any)
		if !ok || m["type"] != "null" {
			result = append(result, b)
		}
	}
	return result
}

// flattenEnumBranches merges anyOf/oneOf branches when all branches have enum values.
// Each branch is sanitized exactly once and the sanitized result is reused for
// enum/type extraction, avoiding the double SanitizeJSONSchema call that the
// previous combinator pass performed per branch.
// Returns a merged schema with combined enum, or nil if not all branches are enum-based.
func flattenEnumBranches(branches []any) map[string]any {
	if len(branches) == 0 {
		return nil
	}
	var allEnums []any
	var typ string
	typConsistent := true
	for _, branch := range branches {
		m, ok := branch.(map[string]any)
		if !ok {
			return nil
		}
		sanitized := SanitizeJSONSchema(m)
		enumVal, hasEnum := sanitized["enum"]
		if !hasEnum {
			return nil
		}
		arr, ok := enumVal.([]any)
		if !ok {
			return nil
		}
		allEnums = append(allEnums, arr...)
		if t, ok := sanitized["type"].(string); ok {
			if typ == "" {
				typ = t
			} else if typ != t {
				typConsistent = false
			}
		} else {
			typConsistent = false
		}
	}
	merged := map[string]any{"enum": allEnums}
	if typ != "" && typConsistent {
		merged["type"] = typ
	}
	return merged
}
