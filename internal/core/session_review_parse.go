package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Suren878/matrixclaw/internal/memory"
)

// Limits on what a session review may propose; an item over them is dropped.
const (
	reviewMaxSkills          = 3
	reviewMaxMemories        = 5
	reviewSkillNameRunes     = 64
	reviewDescriptionRunes   = 60
	reviewSkillBodyRunes     = 8_000
	reviewMemoryContentRunes = 300
	reviewSkillShownRunes    = 4_000
)

const reviewSystemPrompt = `You review a finished working session between a user and matrixclaw, an AI assistant, so that it does better next time. Look for what will matter again:
1. Corrections: the user corrected the assistant's style, tone, workflow or tool use, or stated a preference.
2. Techniques: a non-trivial approach that worked after some effort, such as a debugging path, a command sequence or a project convention found along the way.
3. Skills: a loaded skill whose instructions proved wrong, outdated or missing a step.
Most sessions teach nothing durable; then return empty lists. Never invent lessons, and never record task progress, one-off details, secrets or credentials.

Reply with one JSON object and nothing else:
{"skills":[{"name":"...","description":"Use when ...","body":"...","updates":""}],"memories":[{"scope":"user","content":"..."}]}

skills: at most 3. Prefer improving a loaded skill (set updates to its id and write its whole new body) over a new skill. description starts with "Use when" and has at most 60 characters. body is Markdown instructions: lessons, not logs, meaning the steps and pitfalls to follow next time rather than what happened in this session.
memories: at most 5 facts of one sentence each (at most 300 characters) worth knowing in every later run, such as the user's preferences and project conventions. scope is "project" for a fact about the project in the working directory, otherwise "user".`

// reviewInput is the review's user message: the session's skills, what is
// remembered already and the conversation tail.
func reviewInput(session Session, skills []ReviewedSkill, remembered []memory.Entry, tail string) string {
	var b strings.Builder
	if len(skills) > 0 {
		b.WriteString("Skills loaded in this session:\n")
		for _, skill := range skills {
			fmt.Fprintf(&b, "<skill id=%q name=%q description=%q>\n%s\n</skill>\n", skill.ID, skill.Name, skill.Description, cutRunes(skill.Body, reviewSkillShownRunes))
		}
		b.WriteString("\n")
	}
	if len(remembered) > 0 {
		b.WriteString("Already remembered:\n")
		for _, entry := range remembered {
			fmt.Fprintf(&b, "- %s\n", entry.Content)
		}
		b.WriteString("\n")
	}
	if session.WorkingDir != "" {
		fmt.Fprintf(&b, "Working directory: %s\n\n", session.WorkingDir)
	}
	b.WriteString("Conversation, oldest first:\n\n")
	b.WriteString(tail)
	return b.String()
}

// sessionReview is what a review proposes, validated.
type sessionReview struct {
	Skills   []SkillProposal
	Memories []memory.Entry
}

// parseSessionReview reads the review's JSON reply, a fenced block too. A
// reply that holds no JSON object fails; items breaking the limits are
// dropped, and an update of a skill the session does not use becomes a new
// skill.
func parseSessionReview(text string, loaded []ReviewedSkill, workingDir string) (sessionReview, error) {
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return sessionReview{}, errors.New("no JSON object")
	}
	var reply struct {
		Skills []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Body        string `json:"body"`
			Updates     string `json:"updates"`
		} `json:"skills"`
		Memories []struct {
			Scope   string `json:"scope"`
			Content string `json:"content"`
		} `json:"memories"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &reply); err != nil {
		return sessionReview{}, err
	}
	var review sessionReview
	for _, item := range reply.Skills {
		proposal := SkillProposal{
			Name:        strings.TrimSpace(item.Name),
			Description: strings.Join(strings.Fields(item.Description), " "),
			Body:        strings.TrimSpace(item.Body),
			Updates:     strings.TrimSpace(item.Updates),
		}
		index := slices.IndexFunc(loaded, func(skill ReviewedSkill) bool { return skill.ID == proposal.Updates })
		if index < 0 {
			proposal.Updates = ""
		} else if proposal.Name == "" {
			proposal.Name = loaded[index].Name
		}
		if validSkillProposal(proposal) && len(review.Skills) < reviewMaxSkills {
			review.Skills = append(review.Skills, proposal)
		}
	}
	for _, item := range reply.Memories {
		content := strings.Join(strings.Fields(item.Content), " ")
		if content == "" || utf8.RuneCountInString(content) > reviewMemoryContentRunes || len(review.Memories) >= reviewMaxMemories {
			continue
		}
		entry := memory.Entry{Scope: memory.ScopeUser, Content: content}
		if strings.EqualFold(strings.TrimSpace(item.Scope), string(memory.ScopeProject)) && workingDir != "" {
			entry.Scope, entry.WorkingDir = memory.ScopeProject, workingDir
		}
		review.Memories = append(review.Memories, entry)
	}
	return review, nil
}

func validSkillProposal(proposal SkillProposal) bool {
	return proposal.Name != "" && utf8.RuneCountInString(proposal.Name) <= reviewSkillNameRunes &&
		strings.HasPrefix(strings.ToLower(proposal.Description), "use when ") && utf8.RuneCountInString(proposal.Description) <= reviewDescriptionRunes &&
		proposal.Body != "" && utf8.RuneCountInString(proposal.Body) <= reviewSkillBodyRunes
}

// cutRunes keeps the first n runes of text, marking a cut.
func cutRunes(text string, n int) string {
	if runes := []rune(text); len(runes) > n {
		return string(runes[:n]) + "…"
	}
	return text
}
