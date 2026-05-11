package styles

import (
	"image/color"

	"charm.land/glamour/v2/ansi"
	"github.com/charmbracelet/x/exp/charmtone"
)

func defaultMarkdownStyles(green color.Color) ansi.StyleConfig {
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: new(charmtone.Smoke.Hex()),
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
				Color:       new(charmtone.Malibu.Hex()),
				Bold:        new(true),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Color:           new(charmtone.Zest.Hex()),
				BackgroundColor: new(charmtone.Charple.Hex()),
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
				Color:  new(charmtone.Guac.Hex()),
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
			Color:  new(charmtone.Charcoal.Hex()),
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
			Color:     new(charmtone.Zinc.Hex()),
			Underline: new(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: new(hex(green)),
			Bold:  new(true),
		},
		Image: ansi.StylePrimitive{
			Color:     new(charmtone.Cheeky.Hex()),
			Underline: new(true),
		},
		ImageText: ansi.StylePrimitive{
			Color:  new(charmtone.Squid.Hex()),
			Format: "Image: {{.text}} →",
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: " ",
				Suffix: " ",
				Color:  new(hex(green)),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: new(charmtone.Charcoal.Hex()),
				},
				Margin: new(uint(defaultMargin)),
			},
			Chroma: &ansi.Chroma{
				Text: ansi.StylePrimitive{
					Color: new(charmtone.Smoke.Hex()),
				},
				Error: ansi.StylePrimitive{
					Color:           new(charmtone.Butter.Hex()),
					BackgroundColor: new(charmtone.Sriracha.Hex()),
				},
				Comment: ansi.StylePrimitive{
					Color: new(charmtone.Oyster.Hex()),
				},
				CommentPreproc: ansi.StylePrimitive{
					Color: new(charmtone.Bengal.Hex()),
				},
				Keyword: ansi.StylePrimitive{
					Color: new(charmtone.Malibu.Hex()),
				},
				KeywordReserved: ansi.StylePrimitive{
					Color: new(charmtone.Pony.Hex()),
				},
				KeywordNamespace: ansi.StylePrimitive{
					Color: new(charmtone.Pony.Hex()),
				},
				KeywordType: ansi.StylePrimitive{
					Color: new(charmtone.Guppy.Hex()),
				},
				Operator: ansi.StylePrimitive{
					Color: new(charmtone.Salmon.Hex()),
				},
				Punctuation: ansi.StylePrimitive{
					Color: new(charmtone.Zest.Hex()),
				},
				Name: ansi.StylePrimitive{
					Color: new(charmtone.Smoke.Hex()),
				},
				NameBuiltin: ansi.StylePrimitive{
					Color: new(charmtone.Cheeky.Hex()),
				},
				NameTag: ansi.StylePrimitive{
					Color: new(charmtone.Mauve.Hex()),
				},
				NameAttribute: ansi.StylePrimitive{
					Color: new(charmtone.Hazy.Hex()),
				},
				NameClass: ansi.StylePrimitive{
					Color:     new(charmtone.Salt.Hex()),
					Underline: new(true),
					Bold:      new(true),
				},
				NameDecorator: ansi.StylePrimitive{
					Color: new(charmtone.Citron.Hex()),
				},
				NameFunction: ansi.StylePrimitive{
					Color: new(charmtone.Guac.Hex()),
				},
				LiteralNumber: ansi.StylePrimitive{
					Color: new(charmtone.Julep.Hex()),
				},
				LiteralString: ansi.StylePrimitive{
					Color: new(charmtone.Cumin.Hex()),
				},
				LiteralStringEscape: ansi.StylePrimitive{
					Color: new(charmtone.Bok.Hex()),
				},
				GenericDeleted: ansi.StylePrimitive{
					Color: new(charmtone.Coral.Hex()),
				},
				GenericEmph: ansi.StylePrimitive{
					Italic: new(true),
				},
				GenericInserted: ansi.StylePrimitive{
					Color: new(charmtone.Guac.Hex()),
				},
				GenericStrong: ansi.StylePrimitive{
					Bold: new(true),
				},
				GenericSubheading: ansi.StylePrimitive{
					Color: new(charmtone.Squid.Hex()),
				},
				Background: ansi.StylePrimitive{
					BackgroundColor: new(charmtone.Charcoal.Hex()),
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

func defaultPlainMarkdownStyles(bgBaseLighter, fgMuted, green color.Color) ansi.StyleConfig {
	plainBg := new(hex(bgBaseLighter))
	plainFg := new(hex(fgMuted))
	primitive := func() ansi.StylePrimitive {
		return ansi.StylePrimitive{
			Color:           plainFg,
			BackgroundColor: plainBg,
		}
	}

	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: primitive(),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: primitive(),
			Indent:         new(uint(1)),
			IndentToken:    new("│ "),
		},
		List: ansi.StyleList{
			LevelIndent: defaultListIndent,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockSuffix:     "\n",
				Bold:            new(true),
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Bold:            new(true),
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          "## ",
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          "### ",
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          "#### ",
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          "##### ",
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          "###### ",
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
		},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut:      new(true),
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		Emph: ansi.StylePrimitive{
			Italic:          new(true),
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		Strong: ansi.StylePrimitive{
			Bold:            new(true),
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		HorizontalRule: ansi.StylePrimitive{
			Format:          "\n--------\n",
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		Item: ansi.StylePrimitive{
			BlockPrefix:     "- ",
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix:     ". ",
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		Task: ansi.StyleTask{
			StylePrimitive: primitive(),
			Ticked:         "[✓] ",
			Unticked:       "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Underline:       new(true),
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		LinkText: ansi.StylePrimitive{
			Bold:            new(true),
			Color:           new(hex(green)),
			BackgroundColor: plainBg,
		},
		Image: ansi.StylePrimitive{
			Underline:       new(true),
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		ImageText: ansi.StylePrimitive{
			Format:          "Image: {{.text}} →",
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Color:           new(hex(green)),
				BackgroundColor: plainBg,
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: primitive(),
				Margin:         new(uint(defaultMargin)),
			},
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: primitive(),
			},
		},
		DefinitionDescription: ansi.StylePrimitive{
			BlockPrefix:     "\n ",
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
	}
}
