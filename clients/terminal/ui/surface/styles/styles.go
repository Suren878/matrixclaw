package styles

import (
	"image/color"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/help"
	"charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/diffview"
)

const (
	CheckIcon   string = "✓"
	SpinnerIcon string = "⋯"
	LoadingIcon string = "⟳"
	ModelIcon   string = "◇"

	ArrowRightIcon string = "→"

	ToolPending string = "●"
	ToolSuccess string = "●"
	ToolError   string = "●"

	RadioOn  string = "◉"
	RadioOff string = "○"

	BorderThin  string = "│"
	BorderThick string = "▌"

	SectionSeparator string = "─"

	TodoCompletedIcon  string = "✓"
	TodoPendingIcon    string = "•"
	TodoInProgressIcon string = "→"

	ImageIcon string = "■"
	TextIcon  string = "≡"

	ScrollbarThumb string = "┃"
	ScrollbarTrack string = "│"

	LSPErrorIcon   string = "E"
	LSPWarningIcon string = "W"
	LSPInfoIcon    string = "I"
	LSPHintIcon    string = "H"
)

const (
	defaultMargin     = 2
	defaultListIndent = 2
)

type Styles struct {
	WindowTooSmall lipgloss.Style

	Base      lipgloss.Style
	Muted     lipgloss.Style
	HalfMuted lipgloss.Style
	Subtle    lipgloss.Style

	TagBase  lipgloss.Style
	TagError lipgloss.Style
	TagInfo  lipgloss.Style

	Header struct {
		Charm        lipgloss.Style
		Diagonals    lipgloss.Style
		Title        lipgloss.Style
		Meta         lipgloss.Style
		Percentage   lipgloss.Style
		Keystroke    lipgloss.Style
		KeystrokeTip lipgloss.Style
		WorkingDir   lipgloss.Style
		Separator    lipgloss.Style
	}

	PanelMuted lipgloss.Style
	PanelBase  lipgloss.Style

	LineNumber lipgloss.Style

	ToolCallPending   lipgloss.Style
	ToolCallError     lipgloss.Style
	ToolCallSuccess   lipgloss.Style
	ToolCallCancelled lipgloss.Style
	EarlyStateMessage lipgloss.Style

	TextSelection lipgloss.Style

	ResourceGroupTitle     lipgloss.Style
	ResourceOfflineIcon    lipgloss.Style
	ResourceBusyIcon       lipgloss.Style
	ResourceErrorIcon      lipgloss.Style
	ResourceOnlineIcon     lipgloss.Style
	ResourceName           lipgloss.Style
	ResourceStatus         lipgloss.Style
	ResourceAdditionalText lipgloss.Style

	Markdown      ansi.StyleConfig
	PlainMarkdown ansi.StyleConfig

	TextInput TextInputStyles
	TextArea  TextAreaStyles

	Help help.Styles

	Diff diffview.Style

	FilePicker filepicker.Styles

	ButtonFocus lipgloss.Style
	ButtonBlur  lipgloss.Style

	BorderFocus lipgloss.Style

	EditorPromptNormalFocused lipgloss.Style
	EditorPromptNormalBlurred lipgloss.Style

	RadioOn  lipgloss.Style
	RadioOff lipgloss.Style

	Background color.Color

	LogoFieldColor   color.Color
	LogoTitleColorA  color.Color
	LogoTitleColorB  color.Color
	LogoCharmColor   color.Color
	LogoVersionColor color.Color

	Primary       color.Color
	Secondary     color.Color
	Tertiary      color.Color
	BgBase        color.Color
	BgBaseLighter color.Color
	BgSubtle      color.Color
	BgOverlay     color.Color
	FgBase        color.Color
	FgMuted       color.Color
	FgHalfMuted   color.Color
	FgSubtle      color.Color
	Border        color.Color
	BorderColor   color.Color
	Error         color.Color
	Warning       color.Color
	Info          color.Color
	White         color.Color
	BlueLight     color.Color
	Blue          color.Color
	BlueDark      color.Color
	GreenLight    color.Color
	Green         color.Color
	GreenDark     color.Color
	Red           color.Color
	RedDark       color.Color
	Yellow        color.Color

	Section struct {
		Title lipgloss.Style
		Line  lipgloss.Style
	}

	Initialize struct {
		Header  lipgloss.Style
		Content lipgloss.Style
		Accent  lipgloss.Style
	}

	LSP struct {
		ErrorDiagnostic   lipgloss.Style
		WarningDiagnostic lipgloss.Style
		HintDiagnostic    lipgloss.Style
		InfoDiagnostic    lipgloss.Style
	}

	Files struct {
		Path      lipgloss.Style
		Additions lipgloss.Style
		Deletions lipgloss.Style
	}

	Chat struct {
		Message struct {
			UserBlurred      lipgloss.Style
			UserFocused      lipgloss.Style
			AssistantBlurred lipgloss.Style
			AssistantFocused lipgloss.Style
			AssistantMarker  lipgloss.Style
			UserMarker       lipgloss.Style
			ToolMarker       lipgloss.Style
			FocusedMarker    lipgloss.Style
			FocusedLine      lipgloss.Style
			NoContent        lipgloss.Style
			Thinking         lipgloss.Style
			ErrorTag         lipgloss.Style
			ErrorTitle       lipgloss.Style
			ErrorDetails     lipgloss.Style
			ToolCallFocused  lipgloss.Style
			ToolCallCompact  lipgloss.Style
			ToolCallBlurred  lipgloss.Style
			SectionHeader    lipgloss.Style

			ThinkingBox            lipgloss.Style
			ThinkingTruncationHint lipgloss.Style
			ThinkingFooterTitle    lipgloss.Style
			ThinkingFooterDuration lipgloss.Style
			AssistantInfoIcon      lipgloss.Style
			AssistantInfoModel     lipgloss.Style
			AssistantInfoProvider  lipgloss.Style
			AssistantInfoDuration  lipgloss.Style
		}
	}

	Tool struct {
		IconPending   lipgloss.Style
		IconSuccess   lipgloss.Style
		IconError     lipgloss.Style
		IconCancelled lipgloss.Style

		NameNormal lipgloss.Style
		NameNested lipgloss.Style

		ParamMain lipgloss.Style
		ParamKey  lipgloss.Style

		ContentLine           lipgloss.Style
		ContentTruncation     lipgloss.Style
		ContentCodeLine       lipgloss.Style
		ContentCodeTruncation lipgloss.Style
		ContentCodeBg         color.Color
		Body                  lipgloss.Style

		StateWaiting   lipgloss.Style
		StateCancelled lipgloss.Style

		ErrorTag     lipgloss.Style
		ErrorMessage lipgloss.Style

		DiffTruncation lipgloss.Style

		NoteTag     lipgloss.Style
		NoteMessage lipgloss.Style

		JobIconPending lipgloss.Style
		JobIconError   lipgloss.Style
		JobIconSuccess lipgloss.Style
		JobToolName    lipgloss.Style
		JobAction      lipgloss.Style
		JobPID         lipgloss.Style
		JobDescription lipgloss.Style

		AgentTaskTag lipgloss.Style
		AgentPrompt  lipgloss.Style

		AgenticFetchPromptTag lipgloss.Style

		TodoRatio          lipgloss.Style
		TodoCompletedIcon  lipgloss.Style
		TodoInProgressIcon lipgloss.Style
		TodoPendingIcon    lipgloss.Style

		MCPName     lipgloss.Style
		MCPToolName lipgloss.Style
		MCPArrow    lipgloss.Style

		ResourceLoadedText      lipgloss.Style
		ResourceLoadedIndicator lipgloss.Style
		ResourceName            lipgloss.Style
		ResourceSize            lipgloss.Style
		MediaType               lipgloss.Style

		DockerMCPActionAdd lipgloss.Style
		DockerMCPActionDel lipgloss.Style
	}

	Dialog struct {
		Title       lipgloss.Style
		TitleText   lipgloss.Style
		TitleError  lipgloss.Style
		TitleAccent lipgloss.Style

		View          lipgloss.Style
		PrimaryText   lipgloss.Style
		SecondaryText lipgloss.Style

		HelpView lipgloss.Style
		Help     struct {
			Ellipsis       lipgloss.Style
			ShortKey       lipgloss.Style
			ShortDesc      lipgloss.Style
			ShortSeparator lipgloss.Style
			FullKey        lipgloss.Style
			FullDesc       lipgloss.Style
			FullSeparator  lipgloss.Style
		}

		NormalItem   lipgloss.Style
		SelectedItem lipgloss.Style
		InputPrompt  lipgloss.Style

		List lipgloss.Style

		Spinner lipgloss.Style

		ContentPanel lipgloss.Style

		ScrollbarThumb lipgloss.Style
		ScrollbarTrack lipgloss.Style

		Arguments struct {
			Content                  lipgloss.Style
			Description              lipgloss.Style
			InputLabelBlurred        lipgloss.Style
			InputLabelFocused        lipgloss.Style
			InputRequiredMarkBlurred lipgloss.Style
			InputRequiredMarkFocused lipgloss.Style
		}

		Commands struct{}

		ImagePreview lipgloss.Style
	}

	Status struct {
		Help lipgloss.Style

		ErrorIndicator   lipgloss.Style
		WarnIndicator    lipgloss.Style
		InfoIndicator    lipgloss.Style
		UpdateIndicator  lipgloss.Style
		SuccessIndicator lipgloss.Style

		ErrorMessage   lipgloss.Style
		WarnMessage    lipgloss.Style
		InfoMessage    lipgloss.Style
		UpdateMessage  lipgloss.Style
		SuccessMessage lipgloss.Style
	}

	Completions struct {
		Normal  lipgloss.Style
		Focused lipgloss.Style
		Match   lipgloss.Style
	}

	Attachments struct {
		Normal   lipgloss.Style
		Image    lipgloss.Style
		Text     lipgloss.Style
		Deleting lipgloss.Style
	}

	Pills struct {
		Base            lipgloss.Style
		Focused         lipgloss.Style
		Blurred         lipgloss.Style
		QueueItemPrefix lipgloss.Style
		HelpKey         lipgloss.Style
		HelpText        lipgloss.Style
		Area            lipgloss.Style
		TodoSpinner     lipgloss.Style
	}
}

// DialogHelpStyles returns the styles for dialog help.
func (s *Styles) DialogHelpStyles() help.Styles {
	return help.Styles(s.Dialog.Help)
}
