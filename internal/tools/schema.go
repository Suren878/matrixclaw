package tools

import "encoding/json"

func rawSchema(value string) json.RawMessage {
	return json.RawMessage(value)
}

var (
	readInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "file_path": {"type": "string", "description": "Absolute path, or relative to the working directory."},
    "offset": {"type": "integer", "minimum": 0, "description": "Number of lines to skip (0 = start). To start at line N pass N-1; to read on, pass the offset the previous result names."},
    "limit": {"type": "integer", "minimum": 1, "description": "Maximum lines to return (default 2000; a page also stops at about 24 KB)."}
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
    "file_path": {"type": "string", "description": "Absolute path, or relative to the working directory."},
    "content": {"type": "string", "description": "The complete new content of the file."}
  },
  "required": ["file_path", "content"],
  "additionalProperties": false
}`)
	editOperationSchema = `{
    "type": "object",
    "properties": {
      "old_string": {"type": "string", "description": "Text to replace, as in the file at this point of the edits."},
      "new_string": {"type": "string", "description": "Replacement text; must differ from old_string."},
      "replace_all": {"type": "boolean", "description": "Replace every occurrence instead of exactly one."}
    },
    "required": ["old_string", "new_string"],
    "additionalProperties": false
  }`
	editInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "file_path": {"type": "string", "description": "Absolute path, or relative to the working directory."},
    "old_string": {"type": "string", "description": "Exact text to replace, copied from read output without the line-number prefix. Empty only to fill an empty file."},
    "new_string": {"type": "string", "description": "Replacement text; must differ from old_string."},
    "replace_all": {"type": "boolean", "description": "Replace every occurrence (for example renaming a variable within the file); matches byte for byte only."}
  },
  "required": ["file_path", "old_string", "new_string"],
  "additionalProperties": false
}`)
	multiEditInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "file_path": {"type": "string", "description": "Absolute path, or relative to the working directory."},
    "edits": {
      "type": "array",
      "description": "Replacements applied in order, each to the result of the one before.",
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
    "run_in_background": {"type": "boolean", "description": "Start the command as a background task and return its task id at once."},
    "timeout": {"type": "integer", "minimum": 0, "maximum": 3600, "description": "Seconds before a foreground command is killed; default 600."},
    "auto_background_after": {"type": "integer", "minimum": 0, "description": "Seconds after which a foreground command still running becomes a background task; default 120."}
  },
  "required": ["command"],
  "additionalProperties": false
}`)
	taskOutputInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "The background task id."},
    "wait_seconds": {"type": "integer", "minimum": 0, "maximum": 600, "description": "Wait up to this long for the task to finish first."},
    "filter": {"type": "string", "description": "Regular expression; only matching lines of the new output are returned."}
  },
  "required": ["id"],
  "additionalProperties": false
}`)
	taskKillInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "The background task id."}
  },
  "required": ["id"],
  "additionalProperties": false
}`)
)
