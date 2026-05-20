package commandmenu

import (
	"testing"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestEntriesPlaceSystemActionsInFooter(t *testing.T) {
	entries := Entries(State{PermissionMode: core.PermissionModeDefault})
	if entries[len(entries)-1].ID != "quit" {
		t.Fatalf("last command = %q, want quit", entries[len(entries)-1].ID)
	}
	if !entries[len(entries)-1].Footer {
		t.Fatalf("exit entry = %#v, want footer action", entries[len(entries)-1])
	}
	if entries[len(entries)-1].Role != commandui.RoleExit {
		t.Fatalf("exit role = %q, want exit", entries[len(entries)-1].Role)
	}
	for _, entry := range entries {
		if entry.ID == string(controlplane.CommandNewSession) {
			t.Fatal("main commands should not include New Session")
		}
	}
}

func TestEntriesUseSharedMenuCatalog(t *testing.T) {
	state := State{PermissionMode: core.PermissionModeDefault}
	entries := Entries(state)
	got := map[string]bool{}
	for _, entry := range entries {
		got[entry.ID] = true
	}

	for _, item := range controlplane.BuildCommandView(controlplane.MenuState{
		SessionTitle:   state.SessionTitle,
		ProviderID:     state.ProviderID,
		ModelID:        state.ModelID,
		PermissionMode: state.PermissionMode,
	}) {
		if !item.Menu {
			continue
		}
		if item.ID == string(controlplane.CommandNewSession) {
			continue
		}
		if !got[item.ID] {
			t.Fatalf("terminal commands missing shared menu item %q", item.ID)
		}
	}
}

func TestEntriesExposeCompactAction(t *testing.T) {
	entries := Entries(State{PermissionMode: core.PermissionModeDefault})
	for _, entry := range entries {
		if entry.ID != string(controlplane.CommandContext) {
			continue
		}
		action, ok := entry.Action.(surfacedialog.ActionRunControlplaneCommand)
		if !ok || action.Command != "/context compact" {
			t.Fatalf("compact action = %#v, want /context compact", entry.Action)
		}
		return
	}
	t.Fatalf("entries missing compact command: %#v", entries)
}

func TestEntriesDisableMatrixclawOnlyCommandsForExternalAgent(t *testing.T) {
	entries := Entries(State{
		PermissionMode: core.PermissionModeFullAuto,
		Capabilities:   core.SessionCapabilities{ExternalAgent: true},
	})
	byID := map[string]surfacedialog.CommandEntry{}
	for _, entry := range entries {
		byID[entry.ID] = entry
	}
	for _, id := range []controlplane.CommandID{controlplane.CommandProvider, controlplane.CommandPermissions, controlplane.CommandPlan} {
		entry := byID[string(id)]
		if !entry.Disabled || entry.Status != "Matrixclaw only" {
			t.Fatalf("%s entry = %#v, want disabled Matrixclaw only", id, entry)
		}
	}
}

func TestPickerEntriesKeepSelectedItemDescription(t *testing.T) {
	entries := PickerEntries(controlplane.PickerData{
		Kind: controlplane.PickerPermissions,
		Items: []controlplane.PickerItem{
			{ID: "default", Title: "Ask First"},
			{ID: "accept_edits", Title: "Edits Only", Selected: true},
		},
	})
	if entries[1].Status != "Active" {
		t.Fatalf("selected status = %q, want active marker only", entries[1].Status)
	}
}

func TestPickerEntriesUseFocusedItemWithoutActiveMarker(t *testing.T) {
	entries := PickerEntries(controlplane.PickerData{
		Kind: controlplane.PickerProviderCustom,
		Items: []controlplane.PickerItem{
			{ID: "native", Title: "Enabled", Focused: true},
			{ID: "disabled", Title: "Disabled"},
		},
	})
	if !entries[0].Selected {
		t.Fatalf("focused entry = %#v, want initial selection", entries[0])
	}
	if entries[0].Status != "" || entries[0].Tone != commandui.RowToneNormal {
		t.Fatalf("focused entry presentation = %#v, want no active marker", entries[0])
	}
}

func TestPickerEntriesRenderBackAsFooter(t *testing.T) {
	picker := controlplane.PickerData{
		Kind:        controlplane.PickerProviderActions,
		ContextID:   "local-ai",
		BackCommand: "/provider",
		HasBack:     true,
		Items: []controlplane.PickerItem{
			{ID: "use", Title: "Use"},
			controlplane.BackItem("Return"),
		},
	}
	entries := PickerEntries(picker)
	if len(entries) != 2 || entries[1].ID != "back" || entries[1].Title != "Return" || !entries[1].Footer {
		t.Fatalf("entries = %#v, want back as footer", entries)
	}
	if entries[1].Role != commandui.RoleBack {
		t.Fatalf("back role = %q, want back", entries[1].Role)
	}
	action, ok := entries[1].Action.(surfacedialog.ActionRunControlplaneCommand)
	if !ok || action.Command != "/provider" {
		t.Fatalf("back action = %#v, want /provider", entries[1].Action)
	}
}

func TestPickerEntriesCanHideBack(t *testing.T) {
	picker := controlplane.PickerData{
		Kind:         controlplane.PickerProviderActions,
		ContextID:    "local-ai",
		BackCommand:  "/provider",
		HasBack:      true,
		HideBackItem: true,
		Items: []controlplane.PickerItem{
			{ID: "use", Title: "Use"},
			{ID: "back", Title: "Back", Role: controlplane.PickerItemRoleBack},
		},
	}
	entries := PickerEntries(picker)
	if len(entries) != 1 || entries[0].ID != "use" {
		t.Fatalf("entries = %#v, want only non-back item", entries)
	}
	action, ok := PickerCloseAction(picker).(surfacedialog.ActionRunControlplaneCommand)
	if !ok {
		t.Fatalf("close action = %#v, want ActionRunControlplaneCommand", action)
	}
	if action.Command != "/provider" {
		t.Fatalf("close command = %q, want /provider", action.Command)
	}
}

func TestPickerEntriesHideBackDoesNotHideExplicitClose(t *testing.T) {
	picker := controlplane.PickerData{
		Kind:         controlplane.PickerModules,
		HideBackItem: true,
		Items: []controlplane.PickerItem{
			{ID: "stt", Title: "Speech to Text"},
			controlplane.CloseItem("Back"),
		},
	}
	entries := PickerEntries(picker)
	if len(entries) != 2 || entries[1].ID != "cancel" || entries[1].Title != "Back" || !entries[1].Footer {
		t.Fatalf("entries = %#v, want explicit close footer to remain visible", entries)
	}
}

func TestPickerEntriesRenderCloseAsFooter(t *testing.T) {
	picker := controlplane.PickerData{
		Kind: controlplane.PickerSessions,
		Items: []controlplane.PickerItem{
			{ID: "session-1", Title: "Docs"},
			controlplane.CloseItem("Exit"),
		},
	}
	closeAction := surfacedialog.ActionOpenCommands{}
	entries := PickerEntriesWithCloseAction(picker, closeAction)
	if len(entries) != 2 || entries[1].ID != "cancel" || entries[1].Title != "Exit" || !entries[1].Footer {
		t.Fatalf("entries = %#v, want close item as footer", entries)
	}
	if entries[1].Role != commandui.RoleBack {
		t.Fatalf("close role = %q, want back", entries[1].Role)
	}
	if _, ok := entries[1].Action.(surfacedialog.ActionOpenCommands); !ok {
		t.Fatalf("close action = %#v, want ActionOpenCommands", entries[1].Action)
	}
}

func TestPickerEntriesVoiceProviderBackDoesNotRunStatus(t *testing.T) {
	picker := controlplane.NewPickerData(controlplane.PickerVoiceProvider, "Whisper.cpp").
		Context("stt").
		Back("/modules stt provider").
		Item(controlplane.PickerItem{
			ID:      "status",
			Title:   "Status",
			Command: "/modules stt provider-status whispercpp",
		}).
		Build()
	entries := PickerEntries(picker)
	if len(entries) != 2 {
		t.Fatalf("entries = %#v, want status and back", entries)
	}
	if entries[1].ID != "back" || !entries[1].Footer {
		t.Fatalf("back entry = %#v, want footer back", entries[1])
	}
	action, ok := entries[1].Action.(surfacedialog.ActionRunControlplaneCommand)
	if !ok {
		t.Fatalf("back action = %T, want ActionRunControlplaneCommand", entries[1].Action)
	}
	if action.Command != "/modules stt provider" {
		t.Fatalf("back command = %q, want provider list", action.Command)
	}
	if action.Command == "/modules stt provider-status whispercpp" {
		t.Fatal("back must not reuse the status command")
	}
}

func TestPickerEntriesDoNotAddSessionActionsHeader(t *testing.T) {
	entries := PickerEntries(controlplane.PickerData{
		Kind:      controlplane.PickerSessionActions,
		ContextID: "session-1",
		Items: []controlplane.PickerItem{
			{ID: "use", Title: "Use"},
			controlplane.BackItem(),
		},
	})
	for _, entry := range entries {
		if entry.Kind != surfacedialog.ListEntryRow {
			t.Fatalf("entries = %#v, want only real picker rows", entries)
		}
		if entry.Title == "Actions" {
			t.Fatalf("entries = %#v, want no synthetic Actions header", entries)
		}
	}
}
