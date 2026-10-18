package prompt

import (
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/workspace"
)

// Environment is the fixed part of what a session with a working directory is
// told about where it runs; gitRoot is empty outside a repository.
func Environment(workingDir string, gitRoot string, platform string) string {
	lines := []string{
		"Environment:",
		fmt.Sprintf("- Working directory: %s. Relative paths in tool calls resolve against it; work inside it unless asked otherwise.", workingDir),
	}
	if gitRoot != "" {
		lines = append(lines, "- Git repository root: "+gitRoot+". The branch and status as of this run are in the context update.")
	} else {
		lines = append(lines, "- Not a git repository.")
	}
	if platform != "" {
		lines = append(lines, "- Platform: "+platform+".")
	}
	return strings.Join(lines, "\n")
}

// RunContext is the state a run started in, sent in the context note so the
// fixed prompt stays cacheable. It carries the date, not the time, so the
// note does not change with every run of a quiet chat.
type RunContext struct {
	Now            time.Time
	Client         string
	PermissionMode string
	Git            *workspace.Git
}

// Text renders the run context.
func (r RunContext) Text() string {
	lines := []string{"Run context (as of the start of this run):"}
	if !r.Now.IsZero() {
		lines = append(lines, "- Today: "+r.Now.Format("Monday 2006-01-02")+", timezone "+timezoneName(r.Now)+".")
	}
	if client := clientLine(r.Client); client != "" {
		lines = append(lines, client)
	}
	if mode := permissionModeLine(r.PermissionMode); mode != "" {
		lines = append(lines, mode)
	}
	if r.Git != nil {
		lines = append(lines, gitLines(*r.Git)...)
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func timezoneName(t time.Time) string {
	name := t.Location().String()
	offset := t.Format("-07:00")
	if abbreviation := t.Format("MST"); name == "Local" || name == abbreviation {
		return "UTC" + offset
	}
	return name + " (UTC" + offset + ")"
}

func clientLine(client string) string {
	kind, _, _ := strings.Cut(strings.TrimSpace(client), ":")
	switch kind {
	case "":
		return ""
	case "terminal":
		return "- Client: terminal; Markdown is rendered."
	case "telegram":
		return "- Client: Telegram, a phone chat; keep replies short and use only basic Markdown."
	default:
		return "- Client: " + kind + "."
	}
}

func permissionModeLine(mode string) string {
	switch mode {
	case "default":
		return "- Permission mode default: bash, write, edit and other changing calls wait for the user's approval, so group related changes and skip commands you do not need; reading and searching run at once."
	case "accept_edits":
		return "- Permission mode accept_edits: file writes and edits inside the working directory run without approval; bash and other changing calls still wait for the user."
	case "full_auto":
		return "- Permission mode full_auto: nothing waits for approval, so you alone guard against destructive or irreversible actions."
	case "plan":
		return "- Permission mode plan: every call that may change something is blocked. Explore with read-only tools, then call exit_plan_mode with your plan; once the user approves it, edits are allowed and you carry it out."
	default:
		return ""
	}
}

func gitLines(git workspace.Git) []string {
	if git.Branch == "" && len(git.Status) == 0 && len(git.Commits) == 0 {
		return nil
	}
	head := "- Git: " + git.Branch
	switch changed := len(git.Status) + git.MoreStatus; {
	case git.StatusErr != "":
		head += "; status unavailable (" + git.StatusErr + ")."
	case changed == 0:
		head += "; working tree clean."
	default:
		head += fmt.Sprintf("; %d changed paths:", changed)
	}
	lines := []string{head}
	for _, entry := range git.Status {
		lines = append(lines, "  "+entry)
	}
	if git.MoreStatus > 0 {
		lines = append(lines, fmt.Sprintf("  ... and %d more", git.MoreStatus))
	}
	if len(git.Commits) > 0 {
		lines = append(lines, "- Recent commits:")
		for _, commit := range git.Commits {
			lines = append(lines, "  "+commit)
		}
	}
	return lines
}
