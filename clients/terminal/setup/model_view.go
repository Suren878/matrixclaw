package setup

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (m *model) View() tea.View {
	view := tea.NewView(m.viewContent())
	view.AltScreen = true
	// Do not set View.ForegroundColor/BackgroundColor here. Bubble Tea v2 maps
	// those fields to terminal default colors (OSC 10/11), which leaks into all
	// child rendering and makes setup/chat palettes drift globally.
	return view
}

func (m *model) viewContent() string {
	switch m.screen {
	case screenIntro:
		return m.renderSplash()
	case screenDaemonList:
		summary := setup.SummaryFromDraft(m.draft)
		return m.renderStepList("Daemon", "Step 1/5", "Server", daemonListStatus(summary.Daemon))
	case screenDaemonForm:
		return m.renderDaemonForm()
	case screenProviderList:
		return m.renderProviderList()
	case screenProviderTypeList:
		return m.renderProviderTypeList()
	case screenProviderNoProviderConfirm:
		return m.renderProviderNoProviderConfirm()
	case screenProviderForm:
		return m.renderProviderForm()
	case screenProviderBaseURLList:
		return m.renderProviderBaseURLList()
	case screenProviderModelList:
		return m.renderProviderModelList()
	case screenProviderEffortList:
		return m.renderProviderEffortList()
	case screenProviderToolUseList:
		return m.renderProviderToolUseList()
	case screenDaemonTimezoneList:
		return m.renderDaemonTimezoneList()
	case screenAssistantForm:
		return m.renderAssistantForm()
	case screenChannelsList:
		summary := setup.SummaryFromDraft(m.draft)
		return m.renderStepList("Channels", "Step 4/5", "Telegram", summary.Telegram.Status)
	case screenTelegramForm:
		return m.renderTelegramForm()
	case screenBoolPicker:
		return m.renderBoolPicker()
	case screenTextEditor:
		return m.renderTextEditor()
	case screenSummary:
		return m.renderSummary()
	case screenSuccess:
		return m.renderSuccess()
	default:
		return ""
	}
}
