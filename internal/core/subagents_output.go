package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// compileOutputSchema reads an agent call's output_schema; nil for none.
func compileOutputSchema(raw json.RawMessage) (*jsonschema.Resolved, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("%w: output_schema is not a JSON schema: %w", ErrInvalidInput, err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("%w: output_schema: %w", ErrInvalidInput, err)
	}
	return resolved, nil
}

// checkOutputSchema checks a child's final answer against the schema and
// returns the answer's JSON value, compacted.
func checkOutputSchema(raw json.RawMessage, answer string) (string, error) {
	schema, err := compileOutputSchema(raw)
	if err != nil || schema == nil {
		return answer, err
	}
	text := answerJSON(answer)
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return "", errors.New("the answer is not a JSON value")
	}
	if err := schema.Validate(value); err != nil {
		return "", err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(text)); err != nil {
		return "", err
	}
	return compact.String(), nil
}

// answerJSON is the JSON in a final answer: the whole answer, or the one
// fenced code block it holds.
func answerJSON(answer string) string {
	answer = strings.TrimSpace(answer)
	_, rest, ok := strings.Cut(answer, "```")
	if !ok {
		return answer
	}
	if newline := strings.IndexByte(rest, '\n'); newline >= 0 {
		rest = rest[newline+1:]
	}
	block, _, ok := strings.Cut(rest, "```")
	if !ok {
		return answer
	}
	return strings.TrimSpace(block)
}

// outputSchemaNote asks a child, once, to answer again when its final answer
// does not match its output schema; "" when it does.
func outputSchemaNote(raw json.RawMessage, answer string) string {
	if len(raw) == 0 {
		return ""
	}
	if _, err := checkOutputSchema(raw, answer); err != nil {
		return "Your final answer must be only a JSON value matching the output_schema of your task, and it does not: " + err.Error() + ". Reply again with only that JSON."
	}
	return ""
}

// outputSchemaPrompt tells a child the form of its final answer.
func outputSchemaPrompt(raw json.RawMessage) string {
	var indented bytes.Buffer
	if json.Indent(&indented, raw, "", "  ") != nil {
		indented.Reset()
		indented.Write(raw)
	}
	return "Final answer: reply with only a JSON value matching this JSON schema, no other text:\n" + indented.String()
}
