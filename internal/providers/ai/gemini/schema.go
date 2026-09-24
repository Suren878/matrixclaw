package gemini

import (
	"encoding/json"
	"strings"
)

// schemaFields are the Schema object fields FunctionDeclaration.parameters accepts
// (https://ai.google.dev/api/generate-content#schema); nested schemas are converted separately.
var schemaFields = map[string]bool{
	"type": true, "format": true, "title": true, "description": true, "nullable": true,
	"maxItems": true, "minItems": true, "required": true, "minProperties": true, "maxProperties": true,
	"minLength": true, "maxLength": true, "pattern": true, "example": true, "propertyOrdering": true,
	"default": true, "minimum": true, "maximum": true,
}

// geminiParameters converts a tool's JSON Schema into Gemini's OpenAPI-subset Schema.
func geminiParameters(raw json.RawMessage) json.RawMessage {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil || root == nil {
		return nil
	}
	defs := map[string]any{}
	for _, key := range []string{"definitions", "$defs"} {
		if group, ok := root[key].(map[string]any); ok {
			for name, def := range group {
				defs["#/"+key+"/"+name] = def
			}
		}
	}
	out, err := json.Marshal(schemaConverter{defs: defs, resolving: map[string]bool{}}.convert(root))
	if err != nil {
		return nil
	}
	return out
}

type schemaConverter struct {
	defs      map[string]any
	resolving map[string]bool
}

func (c schemaConverter) convert(value any) map[string]any {
	schema, _ := value.(map[string]any)
	schema = c.flatten(schema)
	out := map[string]any{}
	for key, child := range schema {
		if schemaFields[key] {
			out[key] = child
		}
	}
	if constant, ok := schema["const"].(string); ok {
		out["type"] = "string"
		schema["enum"] = []any{constant}
	}
	if types, ok := schema["type"].([]any); ok {
		c.convertTypeList(types, out)
	}
	if enum := stringEnum(schema["enum"]); len(enum) > 0 && out["type"] == "string" {
		out["enum"] = enum
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		converted := make(map[string]any, len(properties))
		for name, property := range properties {
			converted[name] = c.convert(property)
		}
		out["properties"] = converted
		out["required"] = presentRequired(schema["required"], converted)
	} else {
		delete(out, "required")
	}
	if required, _ := out["required"].([]any); len(required) == 0 {
		delete(out, "required")
	}
	if items, ok := schema["items"].(map[string]any); ok {
		out["items"] = c.convert(items)
	}
	options, _ := schema["anyOf"].([]any)
	if oneOf, ok := schema["oneOf"].([]any); ok {
		options = append(options, oneOf...)
	}
	if len(options) > 0 {
		converted := make([]any, 0, len(options))
		for _, option := range options {
			converted = append(converted, c.convert(option))
		}
		out["anyOf"] = converted
	}
	if out["type"] == nil && out["anyOf"] == nil {
		out["type"] = inferredType(out)
	}
	return out
}

// flatten inlines local $ref targets and merges allOf members into one schema.
func (c schemaConverter) flatten(schema map[string]any) map[string]any {
	merged := map[string]any{}
	if ref, ok := schema["$ref"].(string); ok && !c.resolving[ref] {
		if target, ok := c.defs[ref].(map[string]any); ok {
			c.resolving[ref] = true
			mergeSchema(merged, c.flatten(target))
			delete(c.resolving, ref)
		}
	}
	for _, member := range asList(schema["allOf"]) {
		if member, ok := member.(map[string]any); ok {
			mergeSchema(merged, c.flatten(member))
		}
	}
	own := make(map[string]any, len(schema))
	for key, value := range schema {
		if key != "$ref" && key != "allOf" {
			own[key] = value
		}
	}
	return mergeSchema(own, merged)
}

// mergeSchema fills dst from src; properties and required are unioned, other fields keep dst's value.
func mergeSchema(dst, src map[string]any) map[string]any {
	for key, value := range src {
		switch key {
		case "properties":
			own, _ := dst[key].(map[string]any)
			added, _ := value.(map[string]any)
			properties := make(map[string]any, len(own)+len(added))
			for name, property := range added {
				properties[name] = property
			}
			for name, property := range own {
				properties[name] = property
			}
			dst[key] = properties
		case "required":
			dst[key] = append(append([]any{}, asList(dst[key])...), asList(value)...)
		default:
			if _, exists := dst[key]; !exists {
				dst[key] = value
			}
		}
	}
	return dst
}

func (c schemaConverter) convertTypeList(types []any, out map[string]any) {
	var names []string
	for _, name := range types {
		if name, ok := name.(string); ok && name != "null" {
			names = append(names, name)
		} else if ok {
			out["nullable"] = true
		}
	}
	delete(out, "type")
	switch len(names) {
	case 0:
		out["type"] = "null"
		delete(out, "nullable")
	case 1:
		out["type"] = names[0]
	default:
		options := make([]any, 0, len(names))
		for _, name := range names {
			options = append(options, map[string]any{"type": name})
		}
		out["anyOf"] = options
	}
}

func inferredType(schema map[string]any) string {
	switch {
	case schema["properties"] != nil:
		return "object"
	case schema["items"] != nil:
		return "array"
	default:
		return "string"
	}
}

func stringEnum(value any) []any {
	var out []any
	for _, item := range asList(value) {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func presentRequired(value any, properties map[string]any) []any {
	seen := map[string]bool{}
	var out []any
	for _, name := range asList(value) {
		if name, ok := name.(string); ok && properties[name] != nil && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func asList(value any) []any {
	list, _ := value.([]any)
	return list
}
