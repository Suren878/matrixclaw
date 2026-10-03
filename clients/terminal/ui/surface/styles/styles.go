package styles

import (
	"image/color"

	"charm.land/bubbles/v2/help"
	"charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/diffview"
)

const (
	ModelIcon string = "◇"

	ArrowRightIcon string = "→"

	ToolPending string = "●"
	ToolSuccess string = "●"
	ToolError   string = "●"

	SectionSeparator string = "─"

	ImageIcon string = "■"
	TextIcon  string = "≡"

	ScrollbarThumb string = "┃"
	ScrollbarTrack string = "│"
)

const (
	defaultMargin     = 2
	defaultListIndent = 2
)

type Styles struct {
	Base      lipgloss.Style
	Muted     lipgloss.Style
	HalfMuted lipgloss.Style
	Subtle    lipgloss.Style

	TagBase  lipgloss.Style
	TagError lipgloss.Style

	Header struct {
		Title lipgloss.Style
		Meta  lipgloss.Style
	}

	PanelMuted lipgloss.Style

	LineNumber lipgloss.Style

	TextSelection lipgloss.Style

	Markdown ansi.StyleConfig

	TextInput TextInputStyles
	TextArea  TextAreaStyles

	Help help.Styles

	Diff diffview.Style

	EditorPromptNormalFocused lipgloss.Style
	EditorPromptNormalBlurred lipgloss.Style

	Primary   color.Color
	Secondary color.Color
	BgSubtle  color.Color
	FgBase    color.Color
	White     color.Color
	BlueLight color.Color
	Blue      color.Color

	Section struct {
		Title lipgloss.Style
		Line  lipgloss.Style
	}

	Files struct {
		Path      lipgloss.Style
		Additions lipgloss.Style
		Deletions lipgloss.Style
	}

	Chat struct {
		Message struct {
			AssistantMarker lipgloss.Style
			UserMarker      lipgloss.Style
			ToolMarker      lipgloss.Style
			FocusedMarker   lipgloss.Style
			FocusedLine     lipgloss.Style
			ErrorTag        lipgloss.Style
			ErrorTitle      lipgloss.Style
			ErrorDetails    lipgloss.Style
			SectionHeader   lipgloss.Style

			AssistantInfoIcon     lipgloss.Style
			AssistantInfoModel    lipgloss.Style
			AssistantInfoProvider lipgloss.Style
			AssistantInfoDuration lipgloss.Style
		}
	}

	Tool struct {
		IconPending   lipgloss.Style
		IconSuccess   lipgloss.Style
		IconError     lipgloss.Style
		IconCancelled lipgloss.Style

		NameNormal lipgloss.Style

		ParamMain lipgloss.Style

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

		JobToolName    lipgloss.Style
		JobAction      lipgloss.Style
		JobPID         lipgloss.Style
		JobDescription lipgloss.Style

		ResourceLoadedText      lipgloss.Style
		ResourceLoadedIndicator lipgloss.Style
		ResourceSize            lipgloss.Style
		MediaType               lipgloss.Style
	}

	Dialog struct {
		Title lipgloss.Style

		View lipgloss.Style

		Help struct {
			Ellipsis       lipgloss.Style
			ShortKey       lipgloss.Style
			ShortDesc      lipgloss.Style
			ShortSeparator lipgloss.Style
			FullKey        lipgloss.Style
			FullDesc       lipgloss.Style
			FullSeparator  lipgloss.Style
		}

		ContentPanel lipgloss.Style

		ScrollbarThumb lipgloss.Style
		ScrollbarTrack lipgloss.Style
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

	Attachments struct {
		Normal   lipgloss.Style
		Image    lipgloss.Style
		Text     lipgloss.Style
		Deleting lipgloss.Style
	}
}

// DialogHelpStyles returns the styles for dialog help.
func (s *Styles) DialogHelpStyles() help.Styles {
	return help.Styles(s.Dialog.Help)
}
