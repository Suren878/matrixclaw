package providers

import "encoding/json"

var unsupportedGeminiSchemaKeys = map[string]struct{}{
	"$defs":                 {},
	"$ref":                  {},
	"additionalProperties":  {},
	"allOf":                 {},
	"anyOf":                 {},
	"const":                 {},
	"definitions":           {},
	"exclusiveMaximum":      {},
	"exclusiveMinimum":      {},
	"maxItems":              {},
	"maximum":               {},
	"minItems":              {},
	"minimum":               {},
	"oneOf":                 {},
	"pattern":               {},
	"patternProperties":     {},
	"unevaluatedProperties": {},
}

func sanitizeSchema(schema json.RawMessage, unsupported map[string]struct{}) json.RawMessage {
	if len(schema) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(schema, &value); err != nil {
		return schema
	}
	cleaned := sanitizeSchemaValue(value, unsupported)
	out, err := json.Marshal(cleaned)
	if err != nil {
		return schema
	}
	return out
}

func sanitizeSchemaValue(value any, unsupported map[string]struct{}) any {
	switch typed := value.(type) {
	case map[string]any:
		for key := range unsupported {
			delete(typed, key)
		}
		for key, child := range typed {
			if key == "properties" {
				if props, ok := child.(map[string]any); ok {
					for propName, propSchema := range props {
						props[propName] = sanitizeSchemaValue(propSchema, unsupported)
					}
					typed[key] = props
					continue
				}
			}
			typed[key] = sanitizeSchemaValue(child, unsupported)
		}
		return typed
	case []any:
		for i, child := range typed {
			typed[i] = sanitizeSchemaValue(child, unsupported)
		}
		return typed
	default:
		return value
	}
}
