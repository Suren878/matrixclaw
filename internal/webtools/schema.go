package webtools

import "encoding/json"

var (
	webFetchInputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "url": {"type": "string", "description": "URL to fetch (http or https)"}
  },
  "required": ["url"],
  "additionalProperties": false
}`)
	webSearchInputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Search query"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 20, "description": "Number of results (default 8)"}
  },
  "required": ["query"],
  "additionalProperties": false
}`)
)
