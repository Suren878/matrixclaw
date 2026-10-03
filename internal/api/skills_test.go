package api

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/daemonclient"
	"github.com/Suren878/matrixclaw/internal/skills"
)

func TestSkillsLibraryAndSessionSkillsOverTheClient(t *testing.T) {
	dir := t.TempDir()
	service, err := skills.NewService(skills.Config{DBPath: filepath.Join(dir, "skills.db"), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	server := httptest.NewServer(New(Deps{Skills: service}).Handler())
	t.Cleanup(server.Close)
	client := daemonclient.New(server.URL, "test", "key")
	ctx := context.Background()

	draft, err := client.CreateSkillDraft(ctx, skills.DraftRequest{Name: "Review", Description: "Review a diff"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.UpdateSkillBody(ctx, draft.ID, "Read the diff twice."); err != nil {
		t.Fatal(err)
	}
	detail, err := client.GetSkill(ctx, draft.ID)
	if err != nil || detail.Body == "" {
		t.Fatalf("detail = %+v, %v", detail, err)
	}
	for _, action := range []string{"trust", "enable", "pin"} {
		if err := client.SkillAction(ctx, draft.ID, action); err != nil {
			t.Fatal(action, err)
		}
	}
	if err := client.SkillAction(ctx, draft.ID, "bogus"); !daemonclient.IsAPIStatus(err, 404) {
		t.Fatalf("unknown action error = %v, want 404", err)
	}
	listed, err := client.ListSkills(ctx, skills.SearchOptions{IncludeQuarantined: true, IncludeDisabled: true})
	if err != nil || len(listed) != 1 {
		t.Fatalf("listed = %+v, %v", listed, err)
	}
	if usage, err := client.SkillUsage(ctx); err != nil || len(usage) != 1 {
		t.Fatalf("usage = %+v, %v", usage, err)
	}

	if _, err := client.UseSkill(ctx, "s1", draft.ID); err != nil {
		t.Fatal(err)
	}
	if active, err := client.SessionSkills(ctx, "s1"); err != nil || len(active) != 1 {
		t.Fatalf("session skills = %+v, %v", active, err)
	}
	if err := client.UnloadSkill(ctx, "s1", draft.ID); err != nil {
		t.Fatal(err)
	}
	if active, _ := client.SessionSkills(ctx, "s1"); len(active) != 0 {
		t.Fatalf("session skills after unload = %+v", active)
	}

	if err := client.RemoveSkill(ctx, draft.ID); err != nil {
		t.Fatal(err)
	}
	if listed, _ := client.ListSkills(ctx, skills.SearchOptions{IncludeQuarantined: true, IncludeDisabled: true, IncludeArchived: true}); len(listed) != 0 {
		t.Fatalf("listed after remove = %+v", listed)
	}
}
