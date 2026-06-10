package styles

import (
	"charm.land/bubbles/v2/filepicker"
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
		primary   = tone(charmtone.Guac)
		secondary = tone(charmtone.Bok)
		tertiary  = tone(charmtone.Julep)

		bgBase        = tone(charmtone.Pepper)
		bgBaseLighter = tone(charmtone.BBQ)
		bgSubtle      = tone(charmtone.Charcoal)
		bgOverlay     = tone(charmtone.Iron)

		fgBase      = tone(charmtone.Ash)
		fgMuted     = lipgloss.Color(theme.MutedLight)
		fgHalfMuted = lipgloss.Color(theme.MutedLight)
		fgSubtle    = tone(charmtone.Oyster)

		border      = tone(charmtone.Charcoal)
		borderFocus = tone(charmtone.Guac)

		error   = tone(charmtone.Sriracha)
		warning = tone(charmtone.Zest)
		info    = tone(charmtone.Malibu)

		white = tone(charmtone.Butter)

		blueLight = tone(charmtone.Sardine)
		blue      = tone(charmtone.Malibu)
		blueDark  = tone(charmtone.Damson)

		yellow = tone(charmtone.Mustard)

		greenLight = tone(charmtone.Bok)
		green      = tone(charmtone.Julep)
		greenDark  = tone(charmtone.Guac)

		red     = tone(charmtone.Coral)
		redDark = tone(charmtone.Sriracha)

		userMessageBg     = lipgloss.Color(semanticUserMessageBg)
		toolOutputCodeBg  = bgBase
		selectedControlBg = lipgloss.Color(semanticControlSelected)
		selectedControlFg = lipgloss.Color(semanticControlText)
		focusedMarkerBg   = lipgloss.Color(semanticMarkerFocused)
	)

	normalBorder := lipgloss.NormalBorder()

	base := lipgloss.NewStyle().Foreground(fgBase)

	s := Styles{}

	s.Background = bgBase

	s.Primary = primary
	s.Secondary = secondary
	s.Tertiary = tertiary
	s.BgBase = bgBase
	s.BgBaseLighter = bgBaseLighter
	s.BgSubtle = bgSubtle
	s.BgOverlay = bgOverlay
	s.FgBase = fgBase
	s.FgMuted = fgMuted
	s.FgHalfMuted = fgHalfMuted
	s.FgSubtle = fgSubtle
	s.Border = border
	s.BorderColor = borderFocus
	s.Error = error
	s.Warning = warning
	s.Info = info
	s.White = white
	s.BlueLight = blueLight
	s.Blue = blue
	s.BlueDark = blueDark
	s.GreenLight = greenLight
	s.Green = green
	s.GreenDark = greenDark
	s.Red = red
	s.RedDark = redDark
	s.Yellow = yellow

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
	s.PlainMarkdown = defaultPlainMarkdownStyles(bgBaseLighter, fgMuted, green)

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

	s.FilePicker = filepicker.Styles{
		DisabledCursor:   base.Foreground(fgMuted),
		Cursor:           base.Foreground(fgBase),
		Symlink:          base.Foreground(fgSubtle),
		Directory:        base.Foreground(primary),
		File:             base.Foreground(fgBase),
		DisabledFile:     base.Foreground(fgMuted),
		DisabledSelected: base.Background(bgOverlay).Foreground(fgMuted),
		Permission:       base.Foreground(fgMuted),
		Selected:         base.Background(primary).Foreground(fgBase),
		FileSize:         base.Foreground(fgMuted),
		EmptyDirectory:   base.Foreground(fgMuted).PaddingLeft(2).SetString("Empty directory"),
	}

	s.Base = lipgloss.NewStyle().Foreground(fgBase)
	s.Muted = lipgloss.NewStyle().Foreground(fgMuted)
	s.HalfMuted = lipgloss.NewStyle().Foreground(fgHalfMuted)
	s.Subtle = lipgloss.NewStyle().Foreground(fgSubtle)

	s.WindowTooSmall = s.Muted

	s.TagBase = lipgloss.NewStyle().Padding(0, 1).Foreground(white)
	s.TagError = s.TagBase.Background(redDark)
	s.TagInfo = s.TagBase.Background(blueLight)

	s.Header.Charm = base.Foreground(secondary)
	s.Header.Diagonals = base.Foreground(primary)
	s.Header.Title = base.Foreground(primary).Bold(true)
	s.Header.Meta = base.Foreground(fgMuted)
	s.Header.Percentage = s.Muted
	s.Header.Keystroke = s.Muted
	s.Header.KeystrokeTip = s.Subtle
	s.Header.WorkingDir = s.Muted
	s.Header.Separator = base.Foreground(borderFocus)

	s.PanelMuted = s.Muted.Background(bgBaseLighter)
	s.PanelBase = lipgloss.NewStyle().Background(bgBase)

	s.LineNumber = lipgloss.NewStyle().Foreground(fgMuted).Background(bgBase).PaddingRight(1).PaddingLeft(1)

	s.ToolCallPending = lipgloss.NewStyle().Foreground(greenDark).SetString(ToolPending)
	s.ToolCallError = lipgloss.NewStyle().Foreground(redDark).SetString(ToolError)
	s.ToolCallSuccess = lipgloss.NewStyle().Foreground(green).SetString(ToolSuccess)
	s.ToolCallCancelled = s.Muted.SetString(ToolPending)
	s.EarlyStateMessage = s.Subtle.PaddingLeft(2)

	s.Tool.IconPending = base.Foreground(greenDark).SetString(ToolPending)
	s.Tool.IconSuccess = base.Foreground(green).SetString(ToolSuccess)
	s.Tool.IconError = base.Foreground(redDark).SetString(ToolError)
	s.Tool.IconCancelled = s.Muted.SetString(ToolPending)

	s.Tool.NameNormal = base.Foreground(white).Bold(true)
	s.Tool.NameNested = base.Foreground(white).Bold(true)

	s.Tool.ParamMain = base.Foreground(fgBase)
	s.Tool.ParamKey = s.Subtle

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

	s.Tool.DiffTruncation = s.Muted.Background(bgBaseLighter).PaddingLeft(2)
	s.Tool.NoteTag = base.Padding(0, 1).Background(info).Foreground(white)
	s.Tool.NoteMessage = base.Foreground(fgHalfMuted)

	s.Tool.JobIconPending = base.Foreground(greenDark)
	s.Tool.JobIconError = base.Foreground(redDark)
	s.Tool.JobIconSuccess = base.Foreground(green)
	s.Tool.JobToolName = base.Foreground(white).Bold(true)
	s.Tool.JobAction = base.Foreground(white).Bold(true)
	s.Tool.JobPID = s.Muted
	s.Tool.JobDescription = s.Subtle

	s.Tool.AgentTaskTag = base.Bold(true).Padding(0, 1).MarginLeft(2).Background(blueLight).Foreground(white)
	s.Tool.AgentPrompt = s.Muted

	s.Tool.AgenticFetchPromptTag = base.Bold(true).Padding(0, 1).MarginLeft(2).Background(green).Foreground(border)

	s.Tool.TodoRatio = base.Foreground(blueDark)
	s.Tool.TodoCompletedIcon = base.Foreground(green)
	s.Tool.TodoInProgressIcon = base.Foreground(greenDark)
	s.Tool.TodoPendingIcon = base.Foreground(fgMuted)

	s.Tool.MCPName = base.Foreground(blue)
	s.Tool.MCPToolName = base.Foreground(blueDark)
	s.Tool.MCPArrow = base.Foreground(blue).SetString(ArrowRightIcon)

	s.Tool.ResourceLoadedText = base.Foreground(green)
	s.Tool.ResourceLoadedIndicator = base.Foreground(greenDark)
	s.Tool.ResourceName = base
	s.Tool.MediaType = base
	s.Tool.ResourceSize = base.Foreground(fgMuted)

	s.Tool.DockerMCPActionAdd = base.Foreground(greenLight)
	s.Tool.DockerMCPActionDel = base.Foreground(red)

	s.ButtonFocus = lipgloss.NewStyle().
		Foreground(selectedControlFg).
		Background(selectedControlBg).
		Bold(true)
	s.ButtonBlur = s.Base.Background(bgSubtle)

	s.BorderFocus = lipgloss.NewStyle().BorderForeground(borderFocus).Border(lipgloss.RoundedBorder()).Padding(1, 2)

	s.EditorPromptNormalFocused = lipgloss.NewStyle().Foreground(greenDark).Bold(true)
	s.EditorPromptNormalBlurred = lipgloss.NewStyle().Foreground(fgMuted)

	s.RadioOn = s.HalfMuted.SetString(RadioOn)
	s.RadioOff = s.HalfMuted.SetString(RadioOff)

	s.LogoFieldColor = primary
	s.LogoTitleColorA = secondary
	s.LogoTitleColorB = primary
	s.LogoCharmColor = secondary
	s.LogoVersionColor = primary

	s.Section.Title = s.Subtle
	s.Section.Line = s.Base.Foreground(tone(charmtone.Charcoal))

	s.Initialize.Header = s.Base
	s.Initialize.Content = s.Muted
	s.Initialize.Accent = s.Base.Foreground(greenDark)

	s.ResourceGroupTitle = lipgloss.NewStyle().Foreground(tone(charmtone.Oyster))
	s.ResourceOfflineIcon = lipgloss.NewStyle().Foreground(tone(charmtone.Iron)).SetString("●")
	s.ResourceBusyIcon = s.ResourceOfflineIcon.Foreground(tone(charmtone.Citron))
	s.ResourceErrorIcon = s.ResourceOfflineIcon.Foreground(tone(charmtone.Coral))
	s.ResourceOnlineIcon = s.ResourceOfflineIcon.Foreground(tone(charmtone.Guac))
	s.ResourceName = lipgloss.NewStyle().Foreground(tone(charmtone.Squid))
	s.ResourceStatus = lipgloss.NewStyle().Foreground(tone(charmtone.Oyster))
	s.ResourceAdditionalText = lipgloss.NewStyle().Foreground(tone(charmtone.Oyster))

	s.LSP.ErrorDiagnostic = s.Base.Foreground(redDark)
	s.LSP.WarningDiagnostic = s.Base.Foreground(warning)
	s.LSP.HintDiagnostic = s.Base.Foreground(fgHalfMuted)
	s.LSP.InfoDiagnostic = s.Base.Foreground(info)

	s.Files.Path = s.Muted
	s.Files.Additions = s.Base.Foreground(greenDark)
	s.Files.Deletions = s.Base.Foreground(redDark)

	messageFocussedBorder := lipgloss.Border{
		Left: "▌",
	}

	s.Chat.Message.NoContent = lipgloss.NewStyle().Foreground(fgBase)
	s.Chat.Message.UserBlurred = s.Chat.Message.NoContent.PaddingLeft(1).BorderLeft(true).
		BorderForeground(primary).BorderStyle(normalBorder)
	s.Chat.Message.UserFocused = s.Chat.Message.NoContent.PaddingLeft(1).BorderLeft(true).
		BorderForeground(white).BorderStyle(messageFocussedBorder)
	s.Chat.Message.AssistantBlurred = s.Chat.Message.NoContent.PaddingLeft(2)
	s.Chat.Message.AssistantFocused = s.Chat.Message.NoContent.PaddingLeft(1).BorderLeft(true).
		BorderForeground(white).BorderStyle(messageFocussedBorder)
	s.Chat.Message.AssistantMarker = lipgloss.NewStyle().Foreground(fgBase)
	s.Chat.Message.UserMarker = lipgloss.NewStyle().Foreground(fgHalfMuted).Background(userMessageBg)
	s.Chat.Message.ToolMarker = lipgloss.NewStyle().Foreground(white)
	s.Chat.Message.FocusedMarker = lipgloss.NewStyle().Foreground(fgBase).Background(focusedMarkerBg)
	s.Chat.Message.FocusedLine = lipgloss.NewStyle().Background(userMessageBg)
	s.Chat.Message.Thinking = lipgloss.NewStyle().MaxHeight(10)
	s.Chat.Message.ErrorTag = lipgloss.NewStyle().Padding(0, 1).
		Background(red).Foreground(white)
	s.Chat.Message.ErrorTitle = lipgloss.NewStyle().Foreground(fgHalfMuted)
	s.Chat.Message.ErrorDetails = lipgloss.NewStyle().Foreground(fgSubtle)

	s.Chat.Message.ToolCallFocused = s.Muted.PaddingLeft(1).
		BorderStyle(messageFocussedBorder).
		BorderLeft(true).
		BorderForeground(white)
	s.Chat.Message.ToolCallBlurred = s.Muted.PaddingLeft(2)
	s.Chat.Message.ToolCallCompact = s.Muted
	s.Chat.Message.SectionHeader = s.Base.PaddingLeft(2)
	s.Chat.Message.AssistantInfoIcon = s.Subtle
	s.Chat.Message.AssistantInfoModel = s.Muted
	s.Chat.Message.AssistantInfoProvider = s.Subtle
	s.Chat.Message.AssistantInfoDuration = s.Subtle

	s.Chat.Message.ThinkingBox = s.Subtle.Background(bgBaseLighter)
	s.Chat.Message.ThinkingTruncationHint = s.Muted
	s.Chat.Message.ThinkingFooterTitle = s.Muted
	s.Chat.Message.ThinkingFooterDuration = s.Subtle

	s.TextSelection = lipgloss.NewStyle().Foreground(tone(charmtone.Salt)).Background(tone(charmtone.Charple))

	s.Dialog.Title = base.Padding(0, 1).Foreground(primary)
	s.Dialog.TitleText = base.Foreground(primary)
	s.Dialog.TitleError = base.Foreground(red)
	s.Dialog.TitleAccent = base.Foreground(green).Bold(true)
	s.Dialog.View = base.Border(lipgloss.RoundedBorder()).BorderForeground(borderFocus)
	s.Dialog.PrimaryText = base.Padding(0, 1).Foreground(primary)
	s.Dialog.SecondaryText = base.Padding(0, 1).Foreground(fgSubtle)
	s.Dialog.HelpView = base.Padding(0, 1).AlignHorizontal(lipgloss.Left)
	s.Dialog.Help.ShortKey = base.Foreground(fgMuted)
	s.Dialog.Help.ShortDesc = base.Foreground(fgSubtle)
	s.Dialog.Help.ShortSeparator = base.Foreground(border)
	s.Dialog.Help.Ellipsis = base.Foreground(border)
	s.Dialog.Help.FullKey = base.Foreground(fgMuted)
	s.Dialog.Help.FullDesc = base.Foreground(fgSubtle)
	s.Dialog.Help.FullSeparator = base.Foreground(border)
	s.Dialog.NormalItem = base.Padding(0, 1).Foreground(fgBase)
	s.Dialog.SelectedItem = base.Padding(0, 1).Background(selectedControlBg).Foreground(selectedControlFg).Bold(true)
	s.Dialog.InputPrompt = base.Margin(1, 1)

	s.Dialog.List = base.Margin(0, 0, 1, 0)
	s.Dialog.ContentPanel = base.Background(userMessageBg).Foreground(fgBase).Padding(1, 2)
	s.Dialog.Spinner = base.Foreground(secondary)
	s.Dialog.ScrollbarThumb = base.Foreground(secondary)
	s.Dialog.ScrollbarTrack = base.Foreground(border)

	s.Dialog.ImagePreview = lipgloss.NewStyle().Padding(0, 1).Foreground(fgSubtle)

	s.Dialog.Arguments.Content = base.Padding(1)
	s.Dialog.Arguments.Description = base.MarginBottom(1).MaxHeight(3)
	s.Dialog.Arguments.InputLabelBlurred = base.Foreground(fgMuted)
	s.Dialog.Arguments.InputLabelFocused = base.Bold(true)
	s.Dialog.Arguments.InputRequiredMarkBlurred = base.Foreground(fgMuted).SetString("*")
	s.Dialog.Arguments.InputRequiredMarkFocused = base.Foreground(primary).Bold(true).SetString("*")

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

	s.Completions.Normal = base.Background(bgSubtle).Foreground(fgBase)
	s.Completions.Focused = base.Background(primary).Foreground(white)
	s.Completions.Match = base.Underline(true)

	attachmentIconStyle := base.Foreground(bgSubtle).Background(green).Padding(0, 1)
	s.Attachments.Image = attachmentIconStyle.SetString(ImageIcon)
	s.Attachments.Text = attachmentIconStyle.SetString(TextIcon)
	s.Attachments.Normal = base.Padding(0, 1).MarginRight(1).Background(fgMuted).Foreground(fgBase)
	s.Attachments.Deleting = base.Padding(0, 1).Bold(true).Background(red).Foreground(fgBase)

	s.Pills.Base = base.Padding(0, 1)
	s.Pills.Focused = base.Padding(0, 1).BorderStyle(lipgloss.RoundedBorder()).BorderForeground(bgOverlay)
	s.Pills.Blurred = base.Padding(0, 1).BorderStyle(lipgloss.HiddenBorder())
	s.Pills.QueueItemPrefix = s.Muted.SetString("  •")
	s.Pills.HelpKey = s.Muted
	s.Pills.HelpText = s.Subtle
	s.Pills.Area = base
	s.Pills.TodoSpinner = base.Foreground(greenDark)

	return s
}
