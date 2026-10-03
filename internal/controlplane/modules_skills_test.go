package controlplane

import (
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/api"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/skills"
)

func skillsDaemon(t *testing.T) (*apiDaemon, *skills.Service, skills.Skill) {
	service, err := skills.NewService(skills.Config{DBPath: filepath.Join(t.TempDir(), "skills.db"), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	draft, err := service.CreateDraft("Review", "Review a diff", nil, "Read the diff twice.")
	if err != nil {
		t.Fatal(err)
	}
	return newAPIDaemon(t, api.Deps{Skills: service}), service, draft
}

func pickerItemIDs(result Result) []string {
	if result.Picker == nil {
		return nil
	}
	var ids []string
	for _, item := range result.Picker.Items {
		ids = append(ids, item.ID)
	}
	return ids
}

func TestSkillsReviewTrustUseAndRemove(t *testing.T) {
	daemon, service, draft := skillsDaemon(t)

	root := daemon.run("/modules skills")
	if ids := pickerItemIDs(root); len(ids) != 4 || ids[2] != "review" {
		t.Fatalf("skills root = %v, want library, add, review, usage", ids)
	}
	review := daemon.run("/modules skills review")
	if ids := pickerItemIDs(review); len(ids) != 1 || ids[0] != draft.ID {
		t.Fatalf("review queue = %v", ids)
	}

	trusted := daemon.run("/modules skills review " + draft.ID + " trust-enable")
	if trusted.Picker == nil || trusted.Picker.Title != "Review" {
		t.Fatalf("trust-enable = %+v", trusted)
	}
	if detail, _ := service.Get(draft.ID); detail.Skill.TrustState != skills.TrustTrusted || !detail.Skill.Enabled {
		t.Fatalf("skill after trust-enable = %+v", detail.Skill)
	}

	daemon.run("/skills " + draft.ID + " use")
	if active, _ := service.SessionSkills("s1"); len(active) != 1 {
		t.Fatalf("session skills = %+v", active)
	}

	daemon.run("/modules skills library " + draft.ID + " set-enabled off")
	if detail, _ := service.Get(draft.ID); detail.Skill.Enabled {
		t.Fatal("skill still enabled")
	}

	asked := daemon.run("/modules skills library " + draft.ID + " remove")
	if asked.Confirm == nil {
		t.Fatalf("remove = %+v, want a confirmation", asked)
	}
	removed := daemon.run(asked.Confirm.ConfirmCommand)
	if _, err := service.Get(draft.ID); err == nil || removed.Picker == nil {
		t.Fatalf("after remove: picker %+v, skill still there", removed)
	}
}

func TestSkillLibraryWritesAreTheOwners(t *testing.T) {
	daemon, service, draft := skillsDaemon(t)

	result := daemon.runAs(core.RoleMember, "/modules skills review "+draft.ID+" trust-enable")

	if detail, _ := service.Get(draft.ID); detail.Skill.TrustState == skills.TrustTrusted || result.Text == "" {
		t.Fatalf("member trusted a skill: %+v", result)
	}
}
