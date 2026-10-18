package prompt

import (
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/workspace"
)

// ProjectInstructions carries the user's and the repository's instruction
// files, outermost first.
func ProjectInstructions(files []workspace.InstructionFile) string {
	if len(files) == 0 {
		return ""
	}
	lines := []string{
		"Project instructions:",
		"These files come from the user and the repository. Follow them; when they conflict, the file closer to the working directory wins, and the user's direct requests win over all of them. Before changing files in a subdirectory that has its own AGENTS.md, read that file.",
	}
	truncated := false
	for _, file := range files {
		if file.Truncated {
			truncated = true
			lines = append(lines, fmt.Sprintf("<instructions path=%q truncated=\"true\">", file.Path))
		} else {
			lines = append(lines, fmt.Sprintf("<instructions path=%q>", file.Path))
		}
		if content := strings.TrimSpace(file.Content); content != "" {
			lines = append(lines, content)
		}
		lines = append(lines, "</instructions>")
	}
	if truncated {
		lines = append(lines, fmt.Sprintf("The instruction files exceed %d KiB together, so those marked truncated are cut short or left out; read them with the read tool before relying on them.", workspace.MaxInstructionBytes>>10))
	}
	return strings.Join(lines, "\n")
}
