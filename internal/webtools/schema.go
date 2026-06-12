package webtools

import "encoding/json"

func rawSchema(value string) json.RawMessage {
	return json.RawMessage(value)
}

var (
	webFetchInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "url": {"type": "string", "description": "URL to fetch (http or https)"},
    "task": {"type": "string", "description": "Optional extraction task; when set, fetch through web research and return compact facts/result"},
    "max_length": {"type": "integer", "minimum": 1000, "maximum": 100000, "description": "Maximum characters to read internally (default 20000); raw text is stored as an artifact, not returned by default"}
  },
  "required": ["url"],
  "additionalProperties": false
}`)
	webSearchInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Search query"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 20, "description": "Number of results (default 8)"}
  },
  "required": ["query"],
  "additionalProperties": false
}`)
	webResearchInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "task": {"type": "string", "description": "Research task or question"},
    "query": {"type": "string", "description": "Optional search query; defaults to task"},
    "urls": {"type": "array", "items": {"type": "string"}, "description": "Optional URLs to fetch before or instead of search"},
    "max_sources": {"type": "integer", "minimum": 1, "maximum": 12, "description": "Maximum sources to read (default 5)"},
    "depth": {"type": "string", "enum": ["", "quick", "standard", "deep"], "description": "Research depth hint"},
    "browser": {"type": "string", "enum": ["", "auto", "always", "never"], "description": "Browser fallback policy"},
    "freshness": {"type": "string", "enum": ["", "auto", "refresh", "cache"], "description": "Freshness policy"},
    "async": {"type": "string", "enum": ["", "auto", "true", "false"], "description": "auto waits briefly then backgrounds long jobs"}
  },
  "additionalProperties": false
}`)
	webResearchAskInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "research_id": {"type": "string"},
    "question": {"type": "string"},
    "freshness": {"type": "string", "enum": ["", "auto", "refresh", "cache"]},
    "browser": {"type": "string", "enum": ["", "auto", "always", "never"]}
  },
  "required": ["research_id", "question"],
  "additionalProperties": false
}`)
	webResearchStatusInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "research_id": {"type": "string"}
  },
  "required": ["research_id"],
  "additionalProperties": false
}`)
)
