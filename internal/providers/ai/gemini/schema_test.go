package gemini

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Suren878/matrixclaw/internal/automation"
	"github.com/Suren878/matrixclaw/internal/core"
	deliverymodule "github.com/Suren878/matrixclaw/internal/modules/delivery"
	"github.com/Suren878/matrixclaw/internal/modules/geo"
	storagemodule "github.com/Suren878/matrixclaw/internal/modules/storage"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/skills"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/webtools"
)

var documentedSchemaFields = map[string]bool{
	"type": true, "format": true, "title": true, "description": true, "nullable": true, "enum": true,
	"maxItems": true, "minItems": true, "properties": true, "required": true, "minProperties": true,
	"maxProperties": true, "minLength": true, "maxLength": true, "pattern": true, "example": true,
	"anyOf": true, "propertyOrdering": true, "default": true, "items": true, "minimum": true, "maximum": true,
}

// assertGeminiSchema fails on any field outside the documented Schema object or an unusable enum.
func assertGeminiSchema(t *testing.T, path string, value any) {
	t.Helper()
	schema, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s: schema is %T, want object", path, value)
	}
	if _, ok := schema["type"].(string); !ok && schema["anyOf"] == nil {
		t.Errorf("%s: type=%v, want a single type name or anyOf", path, schema["type"])
	}
	for key, child := range schema {
		if !documentedSchemaFields[key] {
			t.Errorf("%s: unsupported field %q", path, key)
		}
		switch key {
		case "enum":
			values, _ := child.([]any)
			if len(values) == 0 {
				t.Errorf("%s: empty enum", path)
			}
			for _, v := range values {
				if s, ok := v.(string); !ok || s == "" {
					t.Errorf("%s: enum value %#v", path, v)
				}
			}
		case "properties":
			for name, property := range child.(map[string]any) {
				assertGeminiSchema(t, path+"."+name, property)
			}
		case "items":
			assertGeminiSchema(t, path+"[]", child)
		case "anyOf":
			for _, option := range child.([]any) {
				assertGeminiSchema(t, path+"|", option)
			}
		}
	}
}

func decodeParameters(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("parameters %s: %v", raw, err)
	}
	return value
}

// registeredToolDefinitions mirrors the daemon's registry, minus modules that import this package.
func registeredToolDefinitions(t *testing.T) []providers.ToolDefinition {
	t.Helper()
	app := core.New(nil)
	registry := tools.NewRegistry(append(tools.CoreExecutors(),
		automation.NewReminderTool(nil),
		automation.NewScheduledAITaskTool(nil),
		deliverymodule.NewSendFileTool(nil, nil),
		webtools.NewFetchTool(),
		webtools.NewSearchTool(nil),
	)...)
	storage, err := storagemodule.New(storagemodule.Config{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, executors := range [][]tools.Executor{
		tools.NewShellExecutors(app),
		core.TodoToolExecutors(app),
		core.AwaitToolExecutors(app),
		core.MemoryToolExecutors(app),
		core.AgentToolExecutors(app),
		geo.NewOSMGeoExecutors(geo.NewOSMService(geo.OSMConfig{})),
		skills.ToolExecutors(nil),
	} {
		if err := registry.Register(executors...); err != nil {
			t.Fatal(err)
		}
	}
	if err := storage.RegisterTools(registry); err != nil {
		t.Fatal(err)
	}
	var definitions []providers.ToolDefinition
	for _, spec := range registry.List() {
		definitions = append(definitions, providers.ToolDefinition{Name: spec.ID, Description: spec.Description, InputSchema: spec.InputJSONSchema})
	}
	return definitions
}

func TestRegisteredToolSchemasReachTheWireAsGeminiSchemas(t *testing.T) {
	definitions := registeredToolDefinitions(t)
	payload := capturePayloads(t, providers.Request{
		Messages: []providers.Message{{Role: "user", Content: "hello"}},
		Tools:    definitions,
	})[0]
	declarations := payload.Tools[0].FunctionDeclarations
	if len(declarations) != len(definitions) {
		t.Fatalf("declarations=%d, tools=%d", len(declarations), len(definitions))
	}
	for _, declaration := range declarations {
		assertGeminiSchema(t, declaration.Name, decodeParameters(t, declaration.Parameters))
	}
}

func TestJSONSchemaConstructsBecomeGeminiSchema(t *testing.T) {
	schema := json.RawMessage(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"additionalProperties": false,
		"$defs": {"point": {"type": "object", "properties": {"x": {"type": "number", "exclusiveMinimum": 0}}, "required": ["x"]}},
		"properties": {
			"mode": {"type": "string", "enum": [""]},
			"note": {"type": ["string", "null"], "description": "optional"},
			"id": {"type": ["string", "integer"]},
			"kind": {"const": "fixed"},
			"origin": {"$ref": "#/$defs/point", "description": "start"},
			"shape": {"oneOf": [{"type": "string"}, {"$ref": "#/$defs/point"}]},
			"merged": {"allOf": [{"type": "object", "properties": {"a": {"type": "string"}}}, {"properties": {"b": {"type": "integer"}}, "required": ["b"]}]},
			"count": {"type": "integer", "enum": [1, 2]}
		},
		"required": ["mode", "missing"]
	}`)
	payload := capturePayloads(t, providers.Request{
		Messages: []providers.Message{{Role: "user", Content: "hello"}},
		Tools:    []providers.ToolDefinition{{Name: "probe", InputSchema: schema}},
	})[0]
	got := decodeParameters(t, payload.Tools[0].FunctionDeclarations[0].Parameters)
	assertGeminiSchema(t, "probe", got)
	want := decodeParameters(t, json.RawMessage(`{
		"type": "object",
		"properties": {
			"mode": {"type": "string"},
			"note": {"type": "string", "nullable": true, "description": "optional"},
			"id": {"anyOf": [{"type": "string"}, {"type": "integer"}]},
			"kind": {"type": "string", "enum": ["fixed"]},
			"origin": {"type": "object", "description": "start", "properties": {"x": {"type": "number"}}, "required": ["x"]},
			"shape": {"anyOf": [{"type": "string"}, {"type": "object", "properties": {"x": {"type": "number"}}, "required": ["x"]}]},
			"merged": {"type": "object", "properties": {"a": {"type": "string"}, "b": {"type": "integer"}}, "required": ["b"]},
			"count": {"type": "integer"}
		},
		"required": ["mode"]
	}`))
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.Marshal(got)
		t.Fatalf("parameters=%s", gotJSON)
	}
}
