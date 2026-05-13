package setup

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func configuredProviderModel() *model {
	service := setup.NewService(setup.NewFileStore("/tmp/unused"))
	return &model{
		service: service,
		draft: setup.Draft{
			ActiveProviderID: "openai",
			Providers: []setup.ProviderDraft{
				{
					ID:                  "openai",
					CatalogID:           "openai",
					Name:                "OpenAI",
					Type:                providers.TypeOpenAICompat,
					Model:               "gpt-5.4-mini",
					HasStoredAPIKey:     true,
					StoredAPIKeyPreview: "****cret",
				},
			},
		},
		builtInProviders: service.ProviderOptions(),
	}
}

func TestProviderEntriesKeepActionsAndProvidersUngrouped(t *testing.T) {
	m := configuredProviderModel()

	entries := m.providerEntries()
	if len(entries) < 4 {
		t.Fatalf("providerEntries() = %d items, want at least 4", len(entries))
	}
	if entries[0].Kind != providerEntryContinue || entries[0].Title != "Continue" {
		t.Fatalf("first entry = %#v, want Continue action", entries[0])
	}
	if entries[1].Kind != providerEntryConfigured || entries[1].Title != "OpenAI" {
		t.Fatalf("second entry = %#v, want configured OpenAI", entries[1])
	}
	if entries[1].Status != "gpt-5.4-mini · Active" {
		t.Fatalf("configured status = %q", entries[1].Status)
	}
	foundAvailable := false
	foundContinue := false
	for _, entry := range entries {
		switch entry.Kind {
		case providerEntryAvailable:
			foundAvailable = true
		case providerEntryContinue:
			foundContinue = true
		}
	}
	if !foundAvailable || !foundContinue {
		t.Fatalf("entries missing expected groups: %#v", entries)
	}

	for _, entry := range entries {
		if entry.Kind == providerEntryAvailable && entry.Option.ID == "openai" {
			t.Fatal("openai should not appear in available providers once configured")
		}
	}
}

func TestSetupViewDoesNotOverrideTerminalDefaultColors(t *testing.T) {
	m := &model{
		screen: screenDaemonList,
	}

	view := m.View()
	if view.ForegroundColor != nil {
		t.Fatalf("ForegroundColor = %v, want nil", view.ForegroundColor)
	}
	if view.BackgroundColor != nil {
		t.Fatalf("BackgroundColor = %v, want nil", view.BackgroundColor)
	}
}

func TestDaemonTimezoneListRendersOffsetAndSelectsTimezone(t *testing.T) {
	m := &model{
		screen:         screenDaemonTimezoneList,
		draft:          setup.Draft{Timezone: "UTC"},
		timezoneCursor: 0,
	}
	if view := ansi.Strip(m.renderDaemonTimezoneList()); !strings.Contains(view, "UTC+03:00 · Moscow") {
		t.Fatalf("timezone list = %q, want Moscow UTC offset", view)
	}

	next, _ := m.updateDaemonTimezoneList(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated := next.(*model)
	if updated.draft.Timezone != "Europe/Moscow" {
		t.Fatalf("Timezone = %q, want Europe/Moscow", updated.draft.Timezone)
	}
	if updated.screen != screenDaemonForm {
		t.Fatalf("screen = %v, want screenDaemonForm", updated.screen)
	}
}

func TestCustomProviderFormUsesManualFields(t *testing.T) {
	m := &model{
		screen: screenProviderForm,
		editingProvider: setup.ProviderDraft{
			ID:      "custom-openai-compatible",
			Name:    "Local AI",
			Type:    providers.TypeOpenAICompat,
			BaseURL: "http://127.0.0.1:11434/v1",
		},
	}

	wantItems := []struct {
		title  string
		target textEditTarget
	}{
		{"Provider name", textEditProviderName},
		{"Base URL", textEditProviderBaseURL},
		{"API key", textEditProviderAPIKey},
		{"Model", textEditProviderModel},
		{"Tool use", textEditNone},
	}
	items := m.providerFormItems()
	if len(items) != len(wantItems) {
		t.Fatalf("providerFormItems() = %d items, want %d", len(items), len(wantItems))
	}
	for i, want := range wantItems {
		if got := items[i].Row.Title; got != want.title {
			t.Fatalf("providerFormItems()[%d].Title = %q, want %q", i, got, want.title)
		}
		if got := items[i].Target; got != want.target {
			t.Fatalf("providerFormItems()[%d].Target = %v, want %v", i, got, want.target)
		}
	}

	m.formFocus = 3
	next, _ := m.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated := next.(*model)
	if updated.screen != screenTextEditor || updated.textEditorTarget != textEditProviderModel {
		t.Fatalf("custom model enter screen/target = %v/%v, want text editor/model", updated.screen, updated.textEditorTarget)
	}
}

func TestBuiltInProviderFormUsesProviderSpecFields(t *testing.T) {
	m := &model{
		screen: screenProviderForm,
		editingProvider: setup.ProviderDraft{
			ID:        "openai",
			CatalogID: "openai",
			Name:      "OpenAI",
			Type:      providers.TypeOpenAICompat,
			Model:     "gpt-5.4-mini",
		},
	}

	wantItems := []struct {
		title     string
		target    textEditTarget
		reasoning bool
		toolUse   bool
	}{
		{"API key", textEditProviderAPIKey, false, false},
		{"Model", textEditProviderModel, false, false},
		{"Reasoning effort", textEditNone, true, false},
		{"Tool use", textEditNone, false, true},
	}
	items := m.providerFormItems()
	if len(items) != len(wantItems) {
		t.Fatalf("providerFormItems() = %d items, want %d", len(items), len(wantItems))
	}
	for i, want := range wantItems {
		if got := items[i].Row.Title; got != want.title {
			t.Fatalf("providerFormItems()[%d].Title = %q, want %q", i, got, want.title)
		}
		if got := items[i].Target; got != want.target {
			t.Fatalf("providerFormItems()[%d].Target = %v, want %v", i, got, want.target)
		}
		if items[i].Reasoning != want.reasoning || items[i].ToolUse != want.toolUse {
			t.Fatalf("providerFormItems()[%d] flags = reasoning:%v tool:%v, want reasoning:%v tool:%v", i, items[i].Reasoning, items[i].ToolUse, want.reasoning, want.toolUse)
		}
	}
	if !m.providerModelUsesPicker() {
		t.Fatal("built-in OpenAI provider should use model picker")
	}
	if got := m.providerReasoningEfforts(); !reflect.DeepEqual(got, []string{"none", "minimal", "low", "medium", "high", "xhigh"}) {
		t.Fatalf("providerReasoningEfforts() = %#v, want OpenAI options", got)
	}
	m.formFocus = 3
	next, _ := m.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated := next.(*model)
	if updated.screen != screenProviderToolUseList {
		t.Fatalf("tool use enter screen = %v, want picker", updated.screen)
	}

	next, _ = updated.updateProviderToolUseList(tea.KeyPressMsg{Code: tea.KeyDown})
	updated = next.(*model)
	next, _ = updated.updateProviderToolUseList(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated = next.(*model)
	if updated.screen != screenProviderForm || updated.editingProvider.ToolUseMode != providers.ToolUseDisabled {
		t.Fatalf("tool use picker result = screen:%v mode:%q, want provider form/disabled", updated.screen, updated.editingProvider.ToolUseMode)
	}
}

func TestToolUsePickerHasNoBackItemAndEscReturnsToProviderForm(t *testing.T) {
	m := &model{
		screen: screenProviderForm,
		editingProvider: setup.ProviderDraft{
			ID:        "qwen",
			CatalogID: "qwen",
			Name:      "Qwen / DashScope",
			Type:      providers.TypeOpenAICompat,
			Model:     "qwen-plus",
		},
		formFocus: 3,
	}

	next, _ := m.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated := next.(*model)
	if updated.screen != screenProviderToolUseList {
		t.Fatalf("screen = %v, want tool use picker", updated.screen)
	}
	if view := ansi.Strip(updated.renderProviderToolUseList()); strings.Contains(view, "Back") {
		t.Fatalf("tool use picker should not render a Back item:\n%s", view)
	}

	next, _ = updated.updateProviderToolUseList(tea.KeyPressMsg{Code: tea.KeyEsc})
	updated = next.(*model)
	if updated.screen != screenProviderForm {
		t.Fatalf("screen after esc = %v, want provider form", updated.screen)
	}
}

func TestQwenUsesModelPickerCapability(t *testing.T) {
	m := &model{
		screen: screenProviderForm,
		editingProvider: setup.ProviderDraft{
			ID:        "qwen",
			CatalogID: "qwen",
			Name:      "Qwen / DashScope",
			Type:      providers.TypeOpenAICompat,
			Model:     "qwen-plus",
		},
		formFocus: 2,
	}

	if !m.providerModelUsesPicker() {
		t.Fatal("qwen should use model picker")
	}
}

func TestQwenProviderFormIncludesEndpointPicker(t *testing.T) {
	m := &model{
		screen: screenProviderForm,
		editingProvider: setup.ProviderDraft{
			ID:        "qwen",
			CatalogID: "qwen",
			Name:      "Qwen / DashScope",
			Type:      providers.TypeOpenAICompat,
			BaseURL:   "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
			Model:     "qwen-plus",
		},
		formFocus: 0,
	}

	items := m.providerFormItems()
	if len(items) < 4 || items[0].Row.Title != "Endpoint" || !items[0].BaseURL {
		t.Fatalf("qwen first form item = %#v, want endpoint picker", items)
	}

	next, _ := m.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated := next.(*model)
	if updated.screen != screenProviderBaseURLList {
		t.Fatalf("screen = %v, want endpoint picker", updated.screen)
	}
	view := ansi.Strip(updated.renderProviderBaseURLList())
	for _, want := range []string{"Singapore / International", "US (Virginia)", "China (Beijing)", "Hong Kong (China)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("endpoint picker missing %q:\n%s", want, view)
		}
	}

	next, _ = updated.updateProviderBaseURLList(tea.KeyPressMsg{Code: tea.KeyDown})
	updated = next.(*model)
	next, _ = updated.updateProviderBaseURLList(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated = next.(*model)
	if updated.screen != screenProviderForm || updated.editingProvider.BaseURL != "https://dashscope-us.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("endpoint picker result = screen:%v base:%q", updated.screen, updated.editingProvider.BaseURL)
	}
}

func TestProviderContinueWithoutConfiguredProviderAsksConfirmation(t *testing.T) {
	service := setup.NewService(setup.NewFileStore("/tmp/unused"))
	m := &model{
		service:          service,
		screen:           screenProviderList,
		draft:            setup.Draft{},
		builtInProviders: service.ProviderOptions(),
	}

	next, _ := m.updateProviderList(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated := next.(*model)
	if updated.screen != screenProviderNoProviderConfirm {
		t.Fatalf("screen = %v, want no-provider confirmation", updated.screen)
	}
	rendered := updated.renderProviderNoProviderConfirm()
	for _, want := range []string{"No provider is configured", "Yes", "No"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("confirmation view missing %q: %q", want, rendered)
		}
	}

	next, _ = updated.updateProviderNoProviderConfirm(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated = next.(*model)
	if updated.screen != screenAssistantForm {
		t.Fatalf("screen after confirm = %v, want assistant form", updated.screen)
	}
}

func TestProviderNoProviderConfirmationNoReturnsToProviders(t *testing.T) {
	m := &model{
		screen:                   screenProviderNoProviderConfirm,
		providerNoProviderCursor: 1,
	}

	next, _ := m.updateProviderNoProviderConfirm(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated := next.(*model)
	if updated.screen != screenProviderList {
		t.Fatalf("screen after no = %v, want provider list", updated.screen)
	}
}

func TestCustomProviderAPIKeyDoesNotLoadRemoteModels(t *testing.T) {
	m := &model{
		editingProvider: setup.ProviderDraft{
			ID:      "custom-openai-compatible",
			Name:    "Local AI",
			Type:    providers.TypeOpenAICompat,
			BaseURL: "http://127.0.0.1:11434/v1",
		},
		textEditorTarget: textEditProviderAPIKey,
		textEditorInput:  newTextField("API key", "", true),
	}
	m.textEditorInput.Update(tea.PasteMsg{Content: "test-api-key"})

	if err := m.applyTextEditorValue(); err != nil {
		t.Fatalf("applyTextEditorValue() error = %v", err)
	}
	moved, err := m.afterTextEditorApply(context.Background())
	if err != nil {
		t.Fatalf("afterTextEditorApply() error = %v", err)
	}
	if moved {
		t.Fatalf("afterTextEditorApply() moved to model picker for custom provider")
	}
	if !m.editingProvider.HasStoredAPIKey || m.editingProvider.StoredAPIKeyPreview != "****-key" {
		t.Fatalf("custom provider key state = stored:%v preview:%q", m.editingProvider.HasStoredAPIKey, m.editingProvider.StoredAPIKeyPreview)
	}
}

func TestProviderTextEditorUsesSharedTextFieldWithPaste(t *testing.T) {
	m := &model{}
	m.openTextEditor(textEditProviderAPIKey, "API Key", "Enter your API key", "", true)

	if m.textEditorInput.Placeholder() != "Enter your API key" {
		t.Fatalf("placeholder = %q, want provider placeholder", m.textEditorInput.Placeholder())
	}
	next, _ := m.updateTextEditor(tea.PasteMsg{Content: "sk-pasted-key"})
	updated := next.(*model)
	if got := updated.textEditorInput.Value(); got != "sk-pasted-key" {
		t.Fatalf("text field value = %q, want pasted key", got)
	}
}

func TestBuiltInProviderAPIKeyOpensManualModelInputWhenModelsFail(t *testing.T) {
	m := &model{
		service: setup.NewService(setup.NewFileStore("/tmp/unused")),
		editingProvider: setup.ProviderDraft{
			ID:        "openai",
			CatalogID: "openai",
			Name:      "OpenAI",
			Type:      providers.TypeOpenAICompat,
			BaseURL:   ":",
			Model:     "gpt-5.4-mini",
		},
		textEditorTarget: textEditProviderAPIKey,
		textEditorInput:  newTextField("API key", "", true),
	}
	m.textEditorInput.Update(tea.PasteMsg{Content: "sk-test"})

	if err := m.applyTextEditorValue(); err != nil {
		t.Fatalf("applyTextEditorValue() error = %v", err)
	}
	moved, err := m.afterTextEditorApply(context.Background())
	if err != nil {
		t.Fatalf("afterTextEditorApply() error = %v", err)
	}
	if !moved || m.screen != screenTextEditor || m.textEditorTarget != textEditProviderModel {
		t.Fatalf("afterTextEditorApply() screen/target/moved = %v/%v/%v, want manual model editor", m.screen, m.textEditorTarget, moved)
	}
	if !strings.Contains(m.formError, "Enter the model manually") {
		t.Fatalf("formError = %q, want manual model prompt", m.formError)
	}
	if !m.editingProvider.HasStoredAPIKey || m.editingProvider.StoredAPIKeyPreview != "****test" {
		t.Fatalf("built-in provider key state = stored:%v preview:%q", m.editingProvider.HasStoredAPIKey, m.editingProvider.StoredAPIKeyPreview)
	}
}

func TestOpenProviderModelTextEditorShowsDiscoveryError(t *testing.T) {
	m := &model{editingProvider: setup.ProviderDraft{Model: "known-model"}}

	m.openProviderModelTextEditor(modelDiscoveryErrorMessage(errors.New("remote unavailable")))

	if m.screen != screenTextEditor || m.textEditorTarget != textEditProviderModel {
		t.Fatalf("screen/target = %v/%v, want model text editor", m.screen, m.textEditorTarget)
	}
	if got := m.textEditorInput.Value(); got != "known-model" {
		t.Fatalf("model input = %q, want existing model", got)
	}
	if !strings.Contains(m.formError, "remote unavailable") {
		t.Fatalf("formError = %q, want discovery error", m.formError)
	}
}

func TestProviderModelCursorClampsToVisibleFilteredRows(t *testing.T) {
	m := &model{
		providerModels:      []string{"gpt-5", "claude-sonnet", "o3"},
		providerModelCursor: 0,
		filterInput:         textinput.New(),
	}
	m.filterInput.SetValue("claude")

	rows := m.providerModelRows()
	if got := m.currentProviderModelRowIndex(rows); got != -1 {
		t.Fatalf("currentProviderModelRowIndex() = %d, want -1 for hidden cursor", got)
	}

	m.clampProviderModelCursor(rows)
	if m.providerModelCursor != 1 {
		t.Fatalf("providerModelCursor = %d, want visible model index 1", m.providerModelCursor)
	}
	if got := m.currentProviderModelRowIndex(rows); got != 0 {
		t.Fatalf("currentProviderModelRowIndex() after clamp = %d, want visible row 0", got)
	}
}

func TestProviderModelListViewportKeepsSelectedRowVisible(t *testing.T) {
	m := &model{
		width:               90,
		height:              24,
		screen:              screenProviderModelList,
		providerModels:      []string{"model-00", "model-01", "model-02", "model-03", "model-04", "model-05", "model-06", "model-07", "model-08", "model-09", "model-10", "model-11", "model-12", "model-13", "model-14", "model-15", "model-16", "model-17", "model-18", "model-19"},
		providerModelCursor: 18,
		filterInput:         textinput.New(),
	}

	view := ansi.Strip(m.renderProviderModelList())
	if !strings.Contains(view, "model-18") {
		t.Fatalf("model list should keep selected model visible:\n%s", view)
	}
	if !strings.Contains(view, "↑ more") {
		t.Fatalf("model list should show previous page marker:\n%s", view)
	}

	rawView := m.renderProviderModelList()
	selectedLine := rawLineContaining(rawView, "model-18")
	if selectedLine == "" {
		t.Fatalf("model list should render selected row:\n%s", view)
	}
	m.providerModelCursor = 17
	unselectedLine := rawLineContaining(m.renderProviderModelList(), "model-18")
	if unselectedLine == "" {
		t.Fatalf("model list should keep neighboring row visible when cursor moves:\n%s", ansi.Strip(m.renderProviderModelList()))
	}
	if selectedLine == unselectedLine {
		t.Fatalf("selected model row should have distinct styling:\n%s", view)
	}
}

func TestRenderProviderListLayout(t *testing.T) {
	m := configuredProviderModel()
	m.filterInput = textinput.New()

	view := m.renderProviderList()
	if strings.Contains(view, "Configured providers stay on top.") {
		t.Fatalf("provider list still renders verbose subtitle")
	}
	if strings.Contains(view, "type filter") {
		t.Fatalf("provider list still renders type filter hint")
	}
	if strings.Contains(view, "3/5 · 1/8") {
		t.Fatalf("provider list still renders compact count in the header")
	}
	if !strings.Contains(view, "Step 2/5") {
		t.Fatalf("provider list = %q, want step label in header", view)
	}
	if strings.Contains(view, "Configured") {
		t.Fatalf("provider list should not render Configured label:\n%s", ansi.Strip(view))
	}
	if !strings.Contains(view, "Search providers") {
		t.Fatalf("provider list = %q, want search placeholder", view)
	}
	if !strings.Contains(view, "ctrl+a add") {
		t.Fatalf("provider list help = %q, want add hotkey hint", view)
	}

	lines := strings.Split(ansi.Strip(view), "\n")
	assertBefore(t, lines, "Continue", "OpenAI")
	assertBefore(t, lines, "Continue", "Search providers")
	assertBefore(t, lines, "Search providers", "OpenAI")
}

func TestRenderAssistantFormKeepsContinueAboveFields(t *testing.T) {
	m := &model{}

	lines := strings.Split(ansi.Strip(m.renderAssistantForm()), "\n")
	assertBefore(t, lines, "Continue", "Name")
	if lineIndex(lines, "matrixclaw") < 0 {
		t.Fatalf("assistant form should render matrixclaw default:\n%s", strings.Join(lines, "\n"))
	}
	oldBrand := "Matrix" + "Claw"
	if lineIndex(lines, oldBrand) >= 0 {
		t.Fatalf("assistant form should not render old brand casing:\n%s", strings.Join(lines, "\n"))
	}
	if lineIndex(lines, "System prompt is managed by matrixclaw.") < lineIndex(lines, "Refresh project context") {
		t.Fatalf("assistant note should stay below editable fields:\n%s", strings.Join(lines, "\n"))
	}
}

func TestRenderAssistantPromptUsesEditableTextView(t *testing.T) {
	m := &model{
		width:  90,
		height: 24,
	}
	m.openTextEditor(textEditAssistantCustomPrompt, "User Prompt", "User instructions for every run", "line one\nline two", false)

	view := ansi.Strip(m.renderTextEditor())
	for _, want := range []string{"User Prompt", "line one", "line two", "Save", "Cancel", "ctrl+s save"} {
		if !strings.Contains(view, want) {
			t.Fatalf("user prompt editor missing %q:\n%s", want, view)
		}
	}
}

func assertBefore(t *testing.T, lines []string, first string, second string) {
	t.Helper()
	firstIndex := lineIndex(lines, first)
	if firstIndex < 0 {
		t.Fatalf("missing %q:\n%s", first, strings.Join(lines, "\n"))
	}
	secondIndex := lineIndex(lines, second)
	if secondIndex < 0 {
		t.Fatalf("missing %q:\n%s", second, strings.Join(lines, "\n"))
	}
	if firstIndex >= secondIndex {
		t.Fatalf("%q should render before %q:\n%s", first, second, strings.Join(lines, "\n"))
	}
}

func lineIndex(lines []string, contains string) int {
	for i, line := range lines {
		if strings.Contains(line, contains) {
			return i
		}
	}
	return -1
}

func rawLineContaining(view string, contains string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(ansi.Strip(line), contains) {
			return line
		}
	}
	return ""
}
