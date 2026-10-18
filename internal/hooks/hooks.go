// Package hooks runs user commands at the agent's seams (before and after tool
// calls, when a run would stop, at its start and before a summary) with the
// Claude Code hook protocol, and the built-in checks after file edits.
package hooks

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Event names when a hook runs.
type Event string

const (
	PreToolUse   Event = "PreToolUse"
	PostToolUse  Event = "PostToolUse"
	Stop         Event = "Stop"
	SubagentStop Event = "SubagentStop"
	SessionStart Event = "SessionStart"
	PreCompact   Event = "PreCompact"
	Notification Event = "Notification"
)

// Events is the closed list of events a hook may name.
var Events = []Event{PreToolUse, PostToolUse, Stop, SubagentStop, SessionStart, PreCompact, Notification}

// DefaultTimeout bounds a hook whose timeout is 0.
const DefaultTimeout = 600 * time.Second

// Hook is one configured command. Matcher selects the tool (tool events), the
// source (SessionStart), the trigger (PreCompact) or the notification type;
// empty or "*" matches all. Timeout is in seconds, 0 for DefaultTimeout.
type Hook struct {
	Event   Event  `json:"event"`
	Matcher string `json:"matcher,omitempty"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// Validate reports what makes the hook unusable.
func (h Hook) Validate() error {
	var problems []error
	if !slices.Contains(Events, h.Event) {
		problems = append(problems, fmt.Errorf("event is %q, want one of %q", h.Event, Events))
	}
	if strings.TrimSpace(h.Command) == "" {
		problems = append(problems, errors.New("command is empty"))
	}
	if h.Timeout < 0 {
		problems = append(problems, errors.New("timeout is negative"))
	}
	if (h.Event == Stop || h.Event == SubagentStop) && h.Matcher != "" {
		problems = append(problems, fmt.Errorf("%s hooks take no matcher", h.Event))
	}
	if _, err := compileMatcher(h.Matcher); err != nil {
		problems = append(problems, fmt.Errorf("matcher: %w", err))
	}
	return errors.Join(problems...)
}

func (h Hook) timeout() time.Duration {
	if h.Timeout == 0 {
		return DefaultTimeout
	}
	return time.Duration(h.Timeout) * time.Second
}

// plainNames is a matcher that lists exact names, such as "edit|write".
var plainNames = regexp.MustCompile(`^[A-Za-z0-9_|]+$`)

// compileMatcher reads a matcher: empty or "*" matches everything, a list of
// plain names matches exactly, anything else is a regular expression that must
// match the whole name.
func compileMatcher(matcher string) (func(string) bool, error) {
	matcher = strings.TrimSpace(matcher)
	switch {
	case matcher == "" || matcher == "*":
		return func(string) bool { return true }, nil
	case plainNames.MatchString(matcher):
		names := strings.Split(matcher, "|")
		return func(name string) bool { return slices.Contains(names, name) }, nil
	}
	re, err := regexp.Compile(`^(?:` + matcher + `)$`)
	if err != nil {
		return nil, err
	}
	return re.MatchString, nil
}

// Diagnostics configures the checks after edits: Disabled turns them all off;
// Commands replaces the built-in check of a file extension (".go") with a bash
// command run in the session's working directory, and an empty command turns
// that extension's check off.
type Diagnostics struct {
	Disabled bool              `json:"disabled,omitempty"`
	Commands map[string]string `json:"commands,omitempty"`
}

var extension = regexp.MustCompile(`^\.[a-z0-9]+$`)

// Validate reports extension keys that are not like ".go".
func (d Diagnostics) Validate() error {
	var problems []error
	for _, ext := range slices.Sorted(maps.Keys(d.Commands)) {
		if !extension.MatchString(ext) {
			problems = append(problems, fmt.Errorf("commands key %q is not a lowercase file extension such as \".go\"", ext))
		}
	}
	return errors.Join(problems...)
}
