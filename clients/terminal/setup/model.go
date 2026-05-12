package setup

import (
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	commandui "github.com/Suren878/matrixclaw/clients/terminal/commandmenu/ui"
	terminaltextfield "github.com/Suren878/matrixclaw/clients/terminal/ui/textfield"
	"github.com/Suren878/matrixclaw/internal/setup"
)

type screen int

const (
	screenIntro screen = iota
	screenDaemonList
	screenDaemonForm
	screenProviderList
	screenProviderTypeList
	screenProviderNoProviderConfirm
	screenProviderForm
	screenProviderBaseURLList
	screenProviderModelList
	screenProviderEffortList
	screenProviderToolUseList
	screenDaemonTimezoneList
	screenAssistantForm
	screenChannelsList
	screenTelegramForm
	screenBoolPicker
	screenTextEditor
	screenSummary
	screenSuccess
)

type textEditTarget int

const (
	textEditNone textEditTarget = iota
	textEditDaemonHTTPAddr
	textEditDaemonDBPath
	textEditDaemonTimezone
	textEditProviderName
	textEditProviderAPIKey
	textEditProviderModel
	textEditProviderBaseURL
	textEditAssistantName
	textEditAssistantCustomPrompt
	textEditTelegramBotToken
	textEditTelegramAllowedUID
)

type boolEditTarget int

const (
	boolEditNone boolEditTarget = iota
	boolEditDaemonAutostart
	boolEditTelegramEnabled
	boolEditTelegramProviderSetup
)

type providerEntryKind int

const (
	providerEntryConfigured providerEntryKind = iota
	providerEntryAvailable
	providerEntryContinue
)

type listItem = commandui.Row

type providerListEntry struct {
	Kind     providerEntryKind
	Title    string
	Status   string
	Provider setup.ProviderDraft
	Option   setup.ProviderOption
}

type model struct {
	service *setup.Service

	width  int
	height int
	rain   []rainColumn

	screen    screen
	cursor    int
	tickCount int

	filterInput textinput.Model

	draft            setup.Draft
	builtInProviders []setup.ProviderOption
	editingProvider  setup.ProviderDraft
	result           setup.ApplyResult
	err              error
	aborted          bool
	hasExisting      bool

	providerTypeCursor       int
	providerNoProviderCursor int
	providerBaseURLCursor    int
	providerModelCursor      int
	providerEffortCursor     int
	providerToolUseCursor    int
	timezoneCursor           int
	boolPickerCursor         int
	formFocus                int
	formAction               int
	formError                string
	draftSnapshot            setup.Draft

	textEditorInput  terminaltextfield.Model
	textAreaInput    textarea.Model
	textEditState    commandui.TextEditState
	textEditorTitle  string
	textEditorTarget textEditTarget
	boolPickerTarget boolEditTarget
	providerModels   []string
}

func newModel(service *setup.Service) (*model, error) {
	draft, err := service.Draft()
	if err != nil {
		return nil, err
	}

	hasExisting, err := service.IsConfigured()
	if err != nil {
		return nil, err
	}

	filter := textinput.New()
	filter.Prompt = ""
	filter.Placeholder = "Find a provider"
	filter.CharLimit = 128
	filter.Focus()

	m := &model{
		service:          service,
		screen:           screenIntro,
		draft:            draft,
		builtInProviders: service.ProviderOptions(),
		hasExisting:      hasExisting,
		filterInput:      filter,
	}
	return m, nil
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, tickCmd())
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncRain(msg.Width, msg.Height)
		return m, nil
	case tickMsg:
		m.updateRain()
		return m, tickCmd()
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.aborted = true
			return m, tea.Quit
		}
	}

	switch m.screen {
	case screenIntro:
		return m.updateIntro(msg)
	case screenDaemonList:
		return m.updateStepList(msg, screenIntro, screenProviderList, screenDaemonForm)
	case screenDaemonForm:
		return m.updateDaemonForm(msg)
	case screenProviderList:
		return m.updateProviderList(msg)
	case screenProviderTypeList:
		return m.updateProviderTypeList(msg)
	case screenProviderNoProviderConfirm:
		return m.updateProviderNoProviderConfirm(msg)
	case screenProviderForm:
		return m.updateProviderForm(msg)
	case screenProviderBaseURLList:
		return m.updateProviderBaseURLList(msg)
	case screenProviderModelList:
		return m.updateProviderModelList(msg)
	case screenProviderEffortList:
		return m.updateProviderEffortList(msg)
	case screenProviderToolUseList:
		return m.updateProviderToolUseList(msg)
	case screenDaemonTimezoneList:
		return m.updateDaemonTimezoneList(msg)
	case screenAssistantForm:
		return m.updateAssistantForm(msg)
	case screenChannelsList:
		return m.updateStepList(msg, screenAssistantForm, screenSummary, screenTelegramForm)
	case screenTelegramForm:
		return m.updateTelegramForm(msg)
	case screenBoolPicker:
		return m.updateBoolPicker(msg)
	case screenTextEditor:
		return m.updateTextEditor(msg)
	case screenSummary:
		return m.updateSummary(msg)
	case screenSuccess:
		return m.updateSuccess(msg)
	default:
		return m, nil
	}
}
