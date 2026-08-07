package providers

import (
	"encoding/json"
	"testing"
)

func TestNormalizeToolSchemaRemovesPropertyNamesForGemini(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object",
		"propertyNames":{"pattern":"^[a-z]+$"},
		"properties":{
			"nested":{"type":"object","propertyNames":{"pattern":"^[a-z]+$"}}
		}
	}`)

	got := NormalizeToolSchema(schema, ToolSchemaGemini)
	var value map[string]any
	if err := json.Unmarshal(got, &value); err != nil {
		t.Fatalf("unmarshal normalized schema: %v", err)
	}
	if _, exists := value["propertyNames"]; exists {
		t.Fatalf("top-level propertyNames was not removed: %s", got)
	}
	nested := value["properties"].(map[string]any)["nested"].(map[string]any)
	if _, exists := nested["propertyNames"]; exists {
		t.Fatalf("nested propertyNames was not removed: %s", got)
	}
}
