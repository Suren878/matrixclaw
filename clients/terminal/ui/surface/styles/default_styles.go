package styles

import (
	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"

	"github.com/Suren878/matrixclaw/clients/terminal/theme"
	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/diffview"
)

const (
	semanticUserMessageBg   = theme.UserBubble
	semanticControlSelected = theme.Bright
	semanticControlText     = theme.SelectedFg
	semanticMarkerFocused   = theme.Marker
)

// DefaultStyles returns the default styles for the UI.
func DefaultStyles() Styles {
	var (
		primary   = charmtone.Guac
		secondary = charmtone.Bok
		tertiary  = charmtone.Julep

		bgBase        = charmtone.Pepper
		bgBaseLighter = charmtone.BBQ
		bgSubtle      = charmtone.Charcoal
		bgOverlay     = charmtone.Iron

		fgBase      = charmtone.Ash
		fgMuted     = lipgloss.Color(theme.MutedLight)
		fgHalfMuted = lipgloss.Color(theme.MutedLight)
		fgSubtle    = charmtone.Oyster

		border      = charmtone.Charcoal
		borderFocus = charmtone.Guac

		warning = charmtone.Zest

		white = charmtone.Butter

		blueLight = charmtone.Sardine
		blue      = charmtone.Malibu

		yellow = charmtone.Mustard

		green     = charmtone.Julep
		greenDark = charmtone.Guac

		red     = charmtone.Coral
		redDark = charmtone.Sriracha

		userMessageBg    = lipgloss.Color(semanticUserMessageBg)
		toolOutputCodeBg = bgBase
		focusedMarkerBg  = lipgloss.Color(semanticMarkerFocused)
	)

	base := lipgloss.NewStyle().Foreground(fgBase)

	s := Styles{}

	s.Primary = primary
	s.Secondary = secondary
	s.BgSubtle = bgSubtle
	s.FgBase = fgBase
	s.White = white
	s.BlueLight = blueLight
	s.Blue = blue

	s.TextInput = TextInputStyles{
		Focused: TextInputStyleState{
			Text:        base,
			Placeholder: base.Foreground(fgSubtle),
			Prompt:      base.Foreground(tertiary),
			Suggestion:  base.Foreground(fgSubtle),
		},
		Blurred: TextInputStyleState{
			Text:        base.Foreground(fgMuted),
			Placeholder: base.Foreground(fgSubtle),
			Prompt:      base.Foreground(fgMuted),
			Suggestion:  base.Foreground(fgSubtle),
		},
		Cursor: TextInputCursorStyle{
			Color: greenDark,
			Shape: CursorBlock,
			Blink: true,
		},
	}

	s.TextArea = TextAreaStyles{
		Focused: TextAreaStyleState{
			Base:             base,
			Text:             base,
			LineNumber:       base.Foreground(fgSubtle),
			CursorLine:       base,
			CursorLineNumber: base.Foreground(fgSubtle),
			Placeholder:      base.Foreground(fgSubtle),
			Prompt:           base.Foreground(tertiary),
		},
		Blurred: TextAreaStyleState{
			Base:             base,
			Text:             base.Foreground(fgMuted),
			LineNumber:       base.Foreground(fgMuted),
			CursorLine:       base,
			CursorLineNumber: base.Foreground(fgMuted),
			Placeholder:      base.Foreground(fgSubtle),
			Prompt:           base.Foreground(fgMuted),
		},
		Cursor: TextAreaCursorStyle{
			Color: greenDark,
			Shape: CursorBlock,
			Blink: true,
		},
	}

	s.Markdown = defaultMarkdownStyles(green)

	s.Help = help.Styles{
		ShortKey:       base.Foreground(fgMuted),
		ShortDesc:      base.Foreground(fgSubtle),
		ShortSeparator: base.Foreground(border),
		Ellipsis:       base.Foreground(border),
		FullKey:        base.Foreground(fgMuted),
		FullDesc:       base.Foreground(fgSubtle),
		FullSeparator:  base.Foreground(border),
	}

	s.Diff = diffview.Style{
		DividerLine: diffview.LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(fgHalfMuted).
				Background(bgBaseLighter),
			Code: lipgloss.NewStyle().
				Foreground(fgHalfMuted).
				Background(bgBaseLighter),
		},
		MissingLine: diffview.LineStyle{
			LineNumber: lipgloss.NewStyle().
				Background(bgBaseLighter),
			Code: lipgloss.NewStyle().
				Background(bgBaseLighter),
		},
		EqualLine: diffview.LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(fgMuted).
				Background(bgBase),
			Code: lipgloss.NewStyle().
				Foreground(fgMuted).
				Background(bgBase),
		},
		InsertLine: diffview.LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(lipgloss.Color(theme.DiffAddNum)).
				Background(lipgloss.Color(theme.DiffAddBg)),
			Symbol: lipgloss.NewStyle().
				Foreground(lipgloss.Color(theme.DiffAddMark)).
				Background(lipgloss.Color(theme.DiffAddBg)),
			Code: lipgloss.NewStyle().
				Foreground(lipgloss.Color(theme.DiffAddFg)).
				Background(lipgloss.Color(theme.DiffAddBg)),
		},
		DeleteLine: diffview.LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(lipgloss.Color(theme.DiffDeleteFg)).
				Background(lipgloss.Color(theme.DiffDeleteBg)),
			Symbol: lipgloss.NewStyle().
				Foreground(lipgloss.Color(theme.DiffDeleteFg)).
				Background(lipgloss.Color(theme.DiffDeleteBg)),
			Code: lipgloss.NewStyle().
				Foreground(lipgloss.Color(theme.DiffDeleteFg)).
				Background(lipgloss.Color(theme.DiffDeleteBg)),
		},
	}

	s.Base = lipgloss.NewStyle().Foreground(fgBase)
	s.Muted = lipgloss.NewStyle().Foreground(fgMuted)
	s.HalfMuted = lipgloss.NewStyle().Foreground(fgHalfMuted)
	s.Subtle = lipgloss.NewStyle().Foreground(fgSubtle)

	s.TagBase = lipgloss.NewStyle().Padding(0, 1).Foreground(white)
	s.TagError = s.TagBase.Background(redDark)

	s.Header.Title = base.Foreground(primary).Bold(true)
	s.Header.Meta = base.Foreground(fgMuted)

	s.PanelMuted = s.Muted.Background(bgBaseLighter)

	s.LineNumber = lipgloss.NewStyle().Foreground(fgMuted).Background(bgBase).PaddingRight(1).PaddingLeft(1)

	s.Tool.IconPending = base.Foreground(greenDark).SetString(ToolPending)
	s.Tool.IconSuccess = base.Foreground(green).SetString(ToolSuccess)
	s.Tool.IconError = base.Foreground(redDark).SetString(ToolError)
	s.Tool.IconCancelled = s.Muted.SetString(ToolPending)

	s.Tool.NameNormal = base.Foreground(white).Bold(true)

	s.Tool.ParamMain = base.Foreground(fgBase)

	s.Tool.ContentLine = s.Muted
	s.Tool.ContentTruncation = s.Muted
	s.Tool.ContentCodeLine = s.Base.PaddingLeft(2)
	s.Tool.ContentCodeTruncation = s.Muted.PaddingLeft(2)
	s.Tool.ContentCodeBg = toolOutputCodeBg
	s.Tool.Body = base.PaddingLeft(2)

	s.Tool.StateWaiting = base.Foreground(fgSubtle)
	s.Tool.StateCancelled = base.Foreground(fgSubtle)

	s.Tool.ErrorTag = base.Padding(0, 1).Background(red).Foreground(white)
	s.Tool.ErrorMessage = base.Foreground(fgHalfMuted)

	s.Tool.JobToolName = base.Foreground(white).Bold(true)
	s.Tool.JobAction = base.Foreground(white).Bold(true)
	s.Tool.JobPID = s.Muted
	s.Tool.JobDescription = s.Subtle

	s.Tool.ResourceLoadedText = base.Foreground(green)
	s.Tool.ResourceLoadedIndicator = base.Foreground(greenDark)
	s.Tool.MediaType = base
	s.Tool.ResourceSize = base.Foreground(fgMuted)

	s.EditorPromptNormalFocused = lipgloss.NewStyle().Foreground(greenDark).Bold(true)
	s.EditorPromptNormalBlurred = lipgloss.NewStyle().Foreground(fgMuted)

	s.Section.Title = s.Subtle
	s.Section.Line = s.Base.Foreground(charmtone.Charcoal)

	s.Files.Path = s.Muted
	s.Files.Additions = s.Base.Foreground(greenDark)
	s.Files.Deletions = s.Base.Foreground(redDark)

	s.Chat.Message.AssistantMarker = lipgloss.NewStyle().Foreground(fgBase)
	s.Chat.Message.UserMarker = lipgloss.NewStyle().Foreground(fgHalfMuted).Background(userMessageBg)
	s.Chat.Message.ToolMarker = lipgloss.NewStyle().Foreground(white)
	s.Chat.Message.FocusedMarker = lipgloss.NewStyle().Foreground(fgBase).Background(focusedMarkerBg)
	s.Chat.Message.FocusedLine = lipgloss.NewStyle().Background(userMessageBg)
	s.Chat.Message.ErrorTag = lipgloss.NewStyle().Padding(0, 1).
		Background(red).Foreground(white)
	s.Chat.Message.ErrorTitle = lipgloss.NewStyle().Foreground(fgHalfMuted)
	s.Chat.Message.ErrorDetails = lipgloss.NewStyle().Foreground(fgSubtle)

	s.Chat.Message.SectionHeader = s.Base.PaddingLeft(2)
	s.Chat.Message.AssistantInfoIcon = s.Subtle
	s.Chat.Message.AssistantInfoModel = s.Muted
	s.Chat.Message.AssistantInfoProvider = s.Subtle
	s.Chat.Message.AssistantInfoDuration = s.Subtle

	s.TextSelection = lipgloss.NewStyle().Foreground(charmtone.Salt).Background(charmtone.Charple)

	s.Dialog.Title = base.Padding(0, 1).Foreground(primary)
	s.Dialog.View = base.Border(lipgloss.RoundedBorder()).BorderForeground(borderFocus)
	s.Dialog.Help.ShortKey = base.Foreground(fgMuted)
	s.Dialog.Help.ShortDesc = base.Foreground(fgSubtle)
	s.Dialog.Help.ShortSeparator = base.Foreground(border)
	s.Dialog.Help.Ellipsis = base.Foreground(border)
	s.Dialog.Help.FullKey = base.Foreground(fgMuted)
	s.Dialog.Help.FullDesc = base.Foreground(fgSubtle)
	s.Dialog.Help.FullSeparator = base.Foreground(border)

	s.Dialog.ContentPanel = base.Background(userMessageBg).Foreground(fgBase).Padding(1, 2)
	s.Dialog.ScrollbarThumb = base.Foreground(secondary)
	s.Dialog.ScrollbarTrack = base.Foreground(border)

	s.Status.Help = lipgloss.NewStyle().Padding(0, 1)
	s.Status.SuccessIndicator = base.Foreground(bgSubtle).Background(green).Padding(0, 1).Bold(true).SetString("OK")
	s.Status.InfoIndicator = s.Status.SuccessIndicator.SetString("INFO")
	s.Status.UpdateIndicator = s.Status.SuccessIndicator.SetString("UPDATE")
	s.Status.WarnIndicator = s.Status.SuccessIndicator.Foreground(bgOverlay).Background(yellow).SetString("WARNING")
	s.Status.ErrorIndicator = s.Status.SuccessIndicator.Foreground(bgBase).Background(red).SetString("ERROR")
	s.Status.SuccessMessage = base.Foreground(bgSubtle).Background(greenDark).Padding(0, 1)
	s.Status.InfoMessage = s.Status.SuccessMessage
	s.Status.UpdateMessage = s.Status.SuccessMessage
	s.Status.WarnMessage = s.Status.SuccessMessage.Foreground(bgOverlay).Background(warning)
	s.Status.ErrorMessage = s.Status.SuccessMessage.Foreground(white).Background(redDark)

	attachmentIconStyle := base.Foreground(bgSubtle).Background(green).Padding(0, 1)
	s.Attachments.Image = attachmentIconStyle.SetString(ImageIcon)
	s.Attachments.Text = attachmentIconStyle.SetString(TextIcon)
	s.Attachments.Normal = base.Padding(0, 1).MarginRight(1).Background(fgMuted).Foreground(fgBase)
	s.Attachments.Deleting = base.Padding(0, 1).Bold(true).Background(red).Foreground(fgBase)

	return s
}
