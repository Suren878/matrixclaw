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
    "pattern": {"type": "string", "description": "Glob matched against paths relative to path, e.g. **/*_test.go or src/**/*.{ts,tsx}."},
    "path": {"type": "string", "description": "Directory to search, absolute or relative to the working directory; default the working directory."}
  },
  "required": ["pattern"],
  "additionalProperties": false
}`)
	grepInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "Go RE2 regular expression, or the exact text when literal_text is true."},
    "path": {"type": "string", "description": "File or directory to search; default the working directory."},
    "include": {"type": "string", "description": "Glob limiting the files searched: *.go or *.{ts,tsx} match names at any depth, src/**/*.ts matches paths relative to path."},
    "literal_text": {"type": "boolean", "description": "Search for pattern as plain text, with no regular expression syntax."},
    "case_insensitive": {"type": "boolean", "description": "Ignore letter case."},
    "context": {"type": "integer", "minimum": 0, "maximum": 10, "description": "Lines to show before and after each match in content mode."},
    "output_mode": {"type": "string", "enum": ["content", "files_with_matches", "count"], "description": "content (default) shows matching lines, files_with_matches only file paths, count matches per file."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 1000, "description": "Most results to show: matching lines in content mode, files otherwise; default 100."},
    "offset": {"type": "integer", "minimum": 0, "description": "Results to skip, to page past a truncated result."}
  },
  "required": ["pattern"],
  "additionalProperties": false
}`)
	lsInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Directory to list; default the working directory."},
    "ignore": {"type": "array", "items": {"type": "string"}, "description": "Globs to leave out, e.g. *.log or testdata; a glob with / matches paths relative to path."},
    "depth": {"type": "integer", "minimum": 1, "description": "Levels to show; default 3, 1 lists only the directory's own entries."}
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
