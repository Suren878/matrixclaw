package core

import (
	"strings"
	"testing"
)

func TestParseSessionReviewKeepsValidItemsOnly(t *testing.T) {
	loaded := []ReviewedSkill{{ID: "deploy", Name: "Deploy"}}
	reply := "Here it is:\n```json\n" + `{
		"skills": [
			{"name": "Go tests", "description": "Use when running Go tests", "body": "Run go test -race first."},
			{"updates": "deploy", "description": "Use when deploying", "body": "Build, then restart."},
			{"name": "Ghost", "updates": "missing", "description": "Use when haunted", "body": "Run."},
			{"name": "No trigger", "description": "Deploy things", "body": "x"},
			{"name": "Too long", "description": "Use when ` + strings.Repeat("x", 60) + `", "body": "x"},
			{"name": "Empty body", "description": "Use when nothing", "body": "  "},
			{"name": "Fourth valid", "description": "Use when counting", "body": "x"}
		],
		"memories": [
			{"scope": "user", "content": "The user prefers  pnpm over npm."},
			{"scope": "project", "content": "Tests need the docker daemon."},
			{"content": ""},
			{"content": "` + strings.Repeat("y", 301) + `"}
		]
	}` + "\n```"

	review, err := parseSessionReview(reply, loaded, "/work/app")
	if err != nil {
		t.Fatal(err)
	}

	want := []SkillProposal{
		{Name: "Go tests", Description: "Use when running Go tests", Body: "Run go test -race first."},
		{Name: "Deploy", Description: "Use when deploying", Body: "Build, then restart.", Updates: "deploy"},
		{Name: "Ghost", Description: "Use when haunted", Body: "Run."},
	}
	if len(review.Skills) != len(want) {
		t.Fatalf("skills = %+v", review.Skills)
	}
	for i := range want {
		if review.Skills[i] != want[i] {
			t.Errorf("skill %d = %+v, want %+v", i, review.Skills[i], want[i])
		}
	}
	if len(review.Memories) != 2 ||
		review.Memories[0] != (MemoryEntry{Scope: MemoryScopeUser, Content: "The user prefers pnpm over npm."}) ||
		review.Memories[1] != (MemoryEntry{Scope: MemoryScopeProject, Content: "Tests need the docker daemon.", WorkingDir: "/work/app"}) {
		t.Fatalf("memories = %+v", review.Memories)
	}
}

func TestParseSessionReviewRejectsReplyWithoutJSON(t *testing.T) {
	for _, reply := range []string{"", "Nothing to learn here.", `{"skills": [`, `{"skills": "none"}`} {
		if review, err := parseSessionReview(reply, nil, ""); err == nil {
			t.Errorf("%q parsed as %+v", reply, review)
		}
	}
}

func TestParseSessionReviewKeepsProjectMemoryOfASessionWithoutDirectoryAsUser(t *testing.T) {
	review, err := parseSessionReview(`{"memories":[{"scope":"project","content":"Use make."}]}`, nil, "")
	if err != nil || len(review.Memories) != 1 || review.Memories[0].Scope != MemoryScopeUser || review.Memories[0].WorkingDir != "" {
		t.Fatalf("review = %+v err = %v", review, err)
	}
}
