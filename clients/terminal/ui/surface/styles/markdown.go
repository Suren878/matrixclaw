package styles

import (
	"charm.land/glamour/v2/ansi"

	"github.com/Suren878/matrixclaw/clients/terminal/theme"
)

func defaultMarkdownStyles() ansi.StyleConfig {
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: new(theme.CodeText),
			},
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{},
			Indent:         new(uint(1)),
			IndentToken:    new("│ "),
		},
		List: ansi.StyleList{
			LevelIndent: defaultListIndent,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockSuffix: "\n",
				Color:       new(theme.InfoStrong),
				Bold:        new(true),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Color:           new(theme.Highlight),
				BackgroundColor: new(theme.SelectionBg),
				Bold:            new(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "## ",
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "### ",
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "#### ",
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "##### ",
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "###### ",
				Color:  new(theme.Accent),
				Bold:   new(false),
			},
		},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: new(true),
		},
		Emph: ansi.StylePrimitive{
			Italic: new(true),
		},
		Strong: ansi.StylePrimitive{
			Bold: new(true),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color:  new(theme.Line),
			Format: "\n--------\n",
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "- ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Task: ansi.StyleTask{
			StylePrimitive: ansi.StylePrimitive{},
			Ticked:         "[✓] ",
			Unticked:       "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Color:     new(theme.CodeLink),
			Underline: new(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: new(theme.Success),
			Bold:  new(true),
		},
		Image: ansi.StylePrimitive{
			Color:     new(theme.CodeBuiltin),
			Underline: new(true),
		},
		ImageText: ansi.StylePrimitive{
			Color:  new(theme.CodeMuted),
			Format: "Image: {{.text}} →",
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: " ",
				Suffix: " ",
				Color:  new(theme.Success),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: new(theme.Line),
				},
				Margin: new(uint(defaultMargin)),
			},
			Chroma: &ansi.Chroma{
				Text: ansi.StylePrimitive{
					Color: new(theme.CodeText),
				},
				Error: ansi.StylePrimitive{
					Color:           new(theme.Strong),
					BackgroundColor: new(theme.ErrorStrong),
				},
				Comment: ansi.StylePrimitive{
					Color: new(theme.Subtle),
				},
				CommentPreproc: ansi.StylePrimitive{
					Color: new(theme.CodePreproc),
				},
				Keyword: ansi.StylePrimitive{
					Color: new(theme.InfoStrong),
				},
				KeywordReserved: ansi.StylePrimitive{
					Color: new(theme.CodeReserved),
				},
				KeywordNamespace: ansi.StylePrimitive{
					Color: new(theme.CodeReserved),
				},
				KeywordType: ansi.StylePrimitive{
					Color: new(theme.CodeType),
				},
				Operator: ansi.StylePrimitive{
					Color: new(theme.CodeOperator),
				},
				Punctuation: ansi.StylePrimitive{
					Color: new(theme.Highlight),
				},
				Name: ansi.StylePrimitive{
					Color: new(theme.CodeText),
				},
				NameBuiltin: ansi.StylePrimitive{
					Color: new(theme.CodeBuiltin),
				},
				NameTag: ansi.StylePrimitive{
					Color: new(theme.CodeTag),
				},
				NameAttribute: ansi.StylePrimitive{
					Color: new(theme.CodeAttribute),
				},
				NameClass: ansi.StylePrimitive{
					Color:     new(theme.SelectionFg),
					Underline: new(true),
					Bold:      new(true),
				},
				NameDecorator: ansi.StylePrimitive{
					Color: new(theme.CodeDecorator),
				},
				NameFunction: ansi.StylePrimitive{
					Color: new(theme.Accent),
				},
				LiteralNumber: ansi.StylePrimitive{
					Color: new(theme.Success),
				},
				LiteralString: ansi.StylePrimitive{
					Color: new(theme.CodeString),
				},
				LiteralStringEscape: ansi.StylePrimitive{
					Color: new(theme.AccentBright),
				},
				GenericDeleted: ansi.StylePrimitive{
					Color: new(theme.Error),
				},
				GenericEmph: ansi.StylePrimitive{
					Italic: new(true),
				},
				GenericInserted: ansi.StylePrimitive{
					Color: new(theme.Accent),
				},
				GenericStrong: ansi.StylePrimitive{
					Bold: new(true),
				},
				GenericSubheading: ansi.StylePrimitive{
					Color: new(theme.CodeMuted),
				},
				Background: ansi.StylePrimitive{
					BackgroundColor: new(theme.Line),
				},
			},
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{},
			},
		},
		DefinitionDescription: ansi.StylePrimitive{
			BlockPrefix: "\n ",
		},
	}
}
