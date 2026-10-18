package core

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Suren878/matrixclaw/internal/agent/prompt"
)

// AgentProfile is a kind of child agent, the builtin types and the user's
// agent files alike: the child's prompt, the tools it may use (none listed:
// every tool a child gets), whether it is read-only, its default model and
// step budget. LeanContext leaves large project instructions out.
type AgentProfile struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Prompt      string   `json:"prompt,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	Readonly    bool     `json:"readonly,omitempty"`
	Model       string   `json:"model,omitempty"`
	BudgetSteps int      `json:"budget_steps,omitempty"`
	LeanContext bool     `json:"lean_context,omitempty"`
	// Source is "builtin" or the file the profile was read from.
	Source string `json:"source,omitempty"`
}

const (
	defaultAgentType   = "general"
	builtinAgentSource = "builtin"
	// agentsSubdir holds a project's agent files, relative to its directory.
	agentsSubdir = ".matrixclaw/agents"
)

// allows reports whether a child of this profile may use the tool.
func (p AgentProfile) allows(toolID string) bool {
	return len(p.Tools) == 0 || slices.Contains(p.Tools, strings.ToLower(toolID))
}

// builtinAgentProfiles are the types every parent may start; review and
// verify name checkpoint_diff only when the daemon has it.
func builtinAgentProfiles(checkpointDiff bool) []AgentProfile {
	search := []string{"read", "grep", "glob", "ls", "repo_map", "bash"}
	inspect := slices.Clone(search)
	if checkpointDiff {
		inspect = append(inspect, checkpointDiffToolName)
	}
	return []AgentProfile{
		{Name: defaultAgentType, Description: "every child tool; implementation slices and anything else", Source: builtinAgentSource},
		{Name: "explore", Description: "read-only search: finds files and symbols, reports paths with line numbers", Prompt: prompt.ExploreAgent, Tools: search, Readonly: true, LeanContext: true, Source: builtinAgentSource},
		{Name: "plan", Description: "read-only: returns a numbered implementation plan with files and risks", Prompt: prompt.PlanAgent, Tools: inspect, Readonly: true, Source: builtinAgentSource},
		{Name: "review", Description: "read-only: reviews a diff and reports findings by severity", Prompt: prompt.ReviewAgent(checkpointDiff), Tools: inspect, Readonly: true, Source: builtinAgentSource},
		{Name: "verify", Description: "runs builds, tests and linters without editing files; reports failures with minimal repros", Prompt: prompt.VerifyAgent, Tools: append(slices.Clone(inspect), "task_output", "task_kill", "todo_write"), Source: builtinAgentSource},
	}
}

// agentProfiles lists the types a parent working in workingDir may start:
// the builtins, then, with userAgents, the owner's agent files (which may
// redefine a builtin, as to give explore a cheaper model) and the project's
// (which may not).
func (c *Core) agentProfiles(workingDir string, userAgents bool) []AgentProfile {
	_, checkpointDiff := c.toolSpec(checkpointDiffToolName)
	profiles := builtinAgentProfiles(checkpointDiff)
	if !userAgents {
		return profiles
	}
	builtin := len(profiles)
	add := func(profile AgentProfile, project bool) {
		i := slices.IndexFunc(profiles, func(p AgentProfile) bool { return p.Name == profile.Name })
		switch {
		case i < 0:
			profiles = append(profiles, profile)
		case project && i < builtin:
			c.logAgentFile(profile.Source, errors.New("a project agent cannot redefine the builtin type "+profile.Name))
		default:
			profiles[i] = profile
		}
	}
	for _, profile := range c.readAgentDir(c.promptEnv.AgentsDir) {
		add(profile, false)
	}
	for _, profile := range c.readAgentDir(projectAgentsDir(workingDir)) {
		add(profile, true)
	}
	slices.SortStableFunc(profiles[builtin:], func(a, b AgentProfile) int { return strings.Compare(a.Name, b.Name) })
	return profiles
}

// agentProfile is the type named name ("" is general) among agentProfiles.
func (c *Core) agentProfile(workingDir string, userAgents bool, name string) (AgentProfile, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = defaultAgentType
	}
	profiles := c.agentProfiles(workingDir, userAgents)
	i := slices.IndexFunc(profiles, func(p AgentProfile) bool { return p.Name == name })
	if i < 0 {
		return AgentProfile{}, false
	}
	return profiles[i], true
}

func agentTypeNames(profiles []AgentProfile) []string {
	names := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		names = append(names, profile.Name)
	}
	return names
}

// projectAgentsDir is the nearest agentsSubdir from dir up to the root of
// the git repository dir is in; outside a repository only dir's.
func projectAgentsDir(dir string) string {
	if dir = normalizeWorkingDir(dir); dir == "" {
		return ""
	}
	top := dir
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			top = d
			break
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	for d := dir; ; d = filepath.Dir(d) {
		path := filepath.Join(d, agentsSubdir)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
		if d == top || filepath.Dir(d) == d {
			return ""
		}
	}
}

// readAgentDir reads the *.md agent files of dir in name order; a file that
// cannot be read is logged once and left out.
func (c *Core) readAgentDir(dir string) []AgentProfile {
	if dir == "" {
		return nil
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	var out []AgentProfile
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err == nil {
			var profile AgentProfile
			if profile, err = parseAgentFile(path, data); err == nil {
				out = append(out, profile)
				continue
			}
		}
		c.logAgentFile(path, err)
	}
	return out
}

func (c *Core) logAgentFile(path string, err error) {
	if _, logged := c.agentFileErrors.LoadOrStore(path+"\x00"+err.Error(), true); !logged {
		log.Printf("core: agent file %s left out: %v", path, err)
	}
}

var agentNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// agentFileHeader is an agent file's YAML frontmatter; keys it does not
// name, such as those of Claude Code's agent files, are ignored.
type agentFileHeader struct {
	Name        string    `yaml:"name"`
	Description string    `yaml:"description"`
	Tools       yaml.Node `yaml:"tools"`
	Readonly    bool      `yaml:"readonly"`
	Model       string    `yaml:"model"`
	BudgetSteps int       `yaml:"budget_steps"`
}

// parseAgentFile reads an agent file: YAML frontmatter between "---" lines,
// then the prompt. The name defaults to the file's; tools is a list or a
// comma-separated string; model "inherit" is the parent's model.
func parseAgentFile(path string, data []byte) (AgentProfile, error) {
	data = bytes.TrimPrefix(data, []byte("\ufeff"))
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return AgentProfile{}, errors.New("no YAML frontmatter")
	}
	head, body, ok := strings.Cut(rest, "\n---")
	if !ok {
		return AgentProfile{}, errors.New("frontmatter is not closed with ---")
	}
	_, body, _ = strings.Cut(body, "\n")
	var header agentFileHeader
	if err := yaml.Unmarshal([]byte(head), &header); err != nil {
		return AgentProfile{}, fmt.Errorf("frontmatter: %w", err)
	}
	profile := AgentProfile{
		Name:        strings.ToLower(strings.TrimSpace(header.Name)),
		Description: strings.Join(strings.Fields(header.Description), " "),
		Prompt:      strings.TrimSpace(body),
		Readonly:    header.Readonly,
		Model:       strings.TrimSpace(header.Model),
		BudgetSteps: header.BudgetSteps,
		Source:      path,
	}
	if profile.Name == "" {
		profile.Name = strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	}
	if !agentNamePattern.MatchString(profile.Name) {
		return AgentProfile{}, fmt.Errorf("name %q: use lowercase letters, digits, - and _ (at most 40)", profile.Name)
	}
	if profile.Prompt == "" {
		return AgentProfile{}, errors.New("the prompt after the frontmatter is empty")
	}
	if profile.Description == "" {
		profile.Description = truncateForTitle(profile.Prompt, 80)
	}
	if strings.EqualFold(profile.Model, "inherit") {
		profile.Model = ""
	}
	if profile.BudgetSteps < 0 {
		return AgentProfile{}, errors.New("budget_steps must not be negative")
	}
	tools, err := agentFileTools(header.Tools)
	if err != nil {
		return AgentProfile{}, err
	}
	profile.Tools = tools
	return profile, nil
}

// agentFileTools reads the tools key: a YAML list or "read, grep".
func agentFileTools(node yaml.Node) ([]string, error) {
	var names []string
	switch node.Kind {
	case 0:
		return nil, nil
	case yaml.ScalarNode:
		names = strings.Split(node.Value, ",")
	case yaml.SequenceNode:
		if err := node.Decode(&names); err != nil {
			return nil, fmt.Errorf("tools: %w", err)
		}
	default:
		return nil, errors.New("tools: give a list of tool names")
	}
	var tools []string
	for _, name := range names {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" && !slices.Contains(tools, name) {
			tools = append(tools, name)
		}
	}
	return tools, nil
}
