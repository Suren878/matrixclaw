package tools

import "encoding/json"

func rawSchema(value string) json.RawMessage {
	return json.RawMessage(value)
}

var (
	readInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "file_path": {"type": "string"},
    "offset": {"type": "integer", "minimum": 0},
    "limit": {"type": "integer", "minimum": 1}
  },
  "required": ["file_path"],
  "additionalProperties": false
}`)
	globInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string"},
    "path": {"type": "string"}
  },
  "required": ["pattern"],
  "additionalProperties": false
}`)
	grepInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string"},
    "path": {"type": "string"},
    "include": {"type": "string"},
    "literal_text": {"type": "boolean"}
  },
  "required": ["pattern"],
  "additionalProperties": false
}`)
	lsInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "ignore": {"type": "array", "items": {"type": "string"}},
    "depth": {"type": "integer", "minimum": 0}
  },
  "additionalProperties": false
}`)
	writeInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "file_path": {"type": "string"},
    "content": {"type": "string"}
  },
  "required": ["file_path", "content"],
  "additionalProperties": false
}`)
	editOperationSchema = `{
    "type": "object",
    "properties": {
      "old_string": {"type": "string"},
      "new_string": {"type": "string"},
      "replace_all": {"type": "boolean"}
    },
    "required": ["old_string", "new_string"],
    "additionalProperties": false
  }`
	editInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "file_path": {"type": "string"},
    "old_string": {"type": "string"},
    "new_string": {"type": "string"},
    "replace_all": {"type": "boolean"}
  },
  "required": ["file_path", "old_string", "new_string"],
  "additionalProperties": false
}`)
	multiEditInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "file_path": {"type": "string"},
    "edits": {
      "type": "array",
      "items": ` + editOperationSchema + `,
      "minItems": 1
    }
  },
  "required": ["file_path", "edits"],
  "additionalProperties": false
}`)
	bashInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "description": {"type": "string"},
    "command": {"type": "string"},
    "working_dir": {"type": "string"},
    "run_in_background": {"type": "boolean"},
    "auto_background_after": {"type": "integer", "minimum": 0}
  },
  "required": ["command"],
  "additionalProperties": false
}`)
	jobOutputInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "shell_id": {"type": "string"},
    "wait": {"type": "boolean"}
  },
  "required": ["shell_id"],
  "additionalProperties": false
}`)
	jobKillInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "shell_id": {"type": "string"}
  },
  "required": ["shell_id"],
  "additionalProperties": false
}`)
)
