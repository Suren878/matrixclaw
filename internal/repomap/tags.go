package repomap

import (
	"bytes"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// definition is one definition of a file: its name, the line it starts on and
// the text the map shows for it.
type definition struct {
	name   string
	line   int
	indent int
	text   string
}

func newDefinition(name string, line int, text string) definition {
	return definition{name: name, line: line, indent: len(text) - len(strings.TrimLeft(text, " \t")), text: text}
}

// fileTags is what a file defines, in line order, and how often it names each
// identifier other than its own definitions.
type fileTags struct {
	defs []definition
	refs map[string]int
}

// maxFileBytes is the largest file read; lexing, slower than parsing Go, stops
// at maxLexedBytes.
const (
	maxFileBytes  = 1 << 20
	maxLexedBytes = 256 << 10
	// maxAverageLine marks minified or generated text.
	maxAverageLine = 300
)

// keyword flags: kwTop defines only on an unindented line, kwBody only when
// a body (`{` or `:`) follows the name, kwAlias only when `=` or `<` does.
const (
	kwAny = iota
	kwTop
	kwBody
	kwAlias
)

// language says how definitions show in a lexer's tokens: a name right after
// one of keywords; with declarators a name followed by `(` after a type on the
// same line (C, C++, Java, C#); with methods a name opening an indented line
// that ends with `{` and is followed by `(` (JavaScript, TypeScript). Files
// reference only definitions of their own family.
type language struct {
	family      string
	lexer       string
	extensions  string
	keywords    map[string]int
	declarators bool
	methods     bool

	chroma chroma.Lexer
}

var (
	jsKeywords = map[string]int{
		"function": kwAny, "class": kwAny, "interface": kwAny, "type": kwAlias, "enum": kwAny,
		"const": kwTop, "let": kwTop, "var": kwTop,
	}
	cKeywords = map[string]int{"struct": kwBody, "union": kwBody, "enum": kwBody, "class": kwBody}
)

// languages maps file extensions to the lexed languages; Go files are parsed
// instead.
var languages = byExtension(
	language{family: "python", lexer: "python", extensions: ".py .pyi", keywords: map[string]int{"def": kwAny, "class": kwAny}},
	language{family: "js", lexer: "javascript", extensions: ".js .jsx .mjs .cjs", keywords: jsKeywords, methods: true},
	language{family: "js", lexer: "typescript", extensions: ".ts .mts .cts", keywords: jsKeywords, methods: true},
	language{family: "js", lexer: "tsx", extensions: ".tsx", keywords: jsKeywords, methods: true},
	language{family: "rust", lexer: "rust", extensions: ".rs", keywords: map[string]int{
		"fn": kwAny, "struct": kwAny, "enum": kwAny, "trait": kwAny, "type": kwAny,
		"union": kwAny, "mod": kwAny, "const": kwAny, "static": kwAny,
	}},
	language{family: "jvm", lexer: "java", extensions: ".java", declarators: true,
		keywords: map[string]int{"class": kwAny, "interface": kwAny, "enum": kwAny, "record": kwAny}},
	language{family: "jvm", lexer: "kotlin", extensions: ".kt .kts",
		keywords: map[string]int{"fun": kwAny, "class": kwAny, "interface": kwAny, "object": kwAny, "typealias": kwAny}},
	language{family: "jvm", lexer: "scala", extensions: ".scala",
		keywords: map[string]int{"def": kwAny, "class": kwAny, "object": kwAny, "trait": kwAny, "type": kwAny}},
	language{family: "c#", lexer: "c#", extensions: ".cs", declarators: true,
		keywords: map[string]int{"class": kwAny, "interface": kwAny, "struct": kwAny, "enum": kwAny, "record": kwAny}},
	language{family: "c", lexer: "c", extensions: ".c .h", declarators: true, keywords: cKeywords},
	language{family: "c", lexer: "c++", extensions: ".cc .cpp .cxx .hh .hpp .hxx", declarators: true, keywords: cKeywords},
	language{family: "swift", lexer: "swift", extensions: ".swift",
		keywords: map[string]int{"func": kwAny, "class": kwAny, "struct": kwAny, "enum": kwAny, "protocol": kwAny, "typealias": kwAny}},
	language{family: "ruby", lexer: "ruby", extensions: ".rb", keywords: map[string]int{"def": kwAny, "class": kwAny, "module": kwAny}},
	language{family: "php", lexer: "php", extensions: ".php",
		keywords: map[string]int{"function": kwAny, "class": kwAny, "interface": kwAny, "trait": kwAny, "enum": kwAny}},
)

func byExtension(specs ...language) map[string]*language {
	byExt := map[string]*language{}
	for _, spec := range specs {
		l := spec
		l.chroma = lexers.Get(l.lexer)
		for ext := range strings.FieldsSeq(l.extensions) {
			byExt[ext] = &l
		}
	}
	return byExt
}

// family is the language family of a file, or empty for files the map does
// not read.
func family(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if ext == ".go" {
		return "go"
	}
	if l := languages[ext]; l != nil {
		return l.family
	}
	return ""
}

// Supported reports whether the map reads a file, by its slash path: one in a
// known language, not vendored, a test fixture or minified.
func Supported(rel string) bool {
	return family(rel) != "" && !skippedPath(rel)
}

// skippedDirs are path elements whose files are vendored or fixtures, not the
// project's own code, even when git tracks them.
var skippedDirs = map[string]bool{"vendor": true, "node_modules": true, "third_party": true, "testdata": true}

func skippedPath(rel string) bool {
	for element := range strings.SplitSeq(rel, "/") {
		if skippedDirs[element] {
			return true
		}
	}
	return strings.HasSuffix(rel, ".min.js")
}

// extract reads the tags of one file's content; binary, minified and
// generated files give none.
func extract(rel string, src []byte) fileTags {
	if bytes.IndexByte(src[:min(len(src), 8<<10)], 0) >= 0 {
		return fileTags{}
	}
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	if lines := bytes.Count(src, []byte("\n")) + 1; len(src)/lines > maxAverageLine {
		return fileTags{}
	}
	ext := strings.ToLower(path.Ext(rel))
	if ext == ".go" {
		return goTags(src)
	}
	if l := languages[ext]; l != nil && len(src) <= maxLexedBytes {
		return l.tags(string(src))
	}
	return fileTags{}
}

// lexeme is a token that is neither space nor comment, with its position.
type lexeme struct {
	kind  chroma.TokenType
	value string
	line  int
	// first is true for the first lexeme of its line.
	first bool
}

func (l *language) tags(src string) fileTags {
	iterator, err := l.chroma.Tokenise(nil, src)
	if err != nil {
		return fileTags{}
	}
	var lexemes []lexeme
	line, first := 1, true
	for token := iterator(); token != chroma.EOF; token = iterator() {
		if !token.Type.InCategory(chroma.Comment) && !token.Type.InCategory(chroma.Text) {
			for _, piece := range pieces(token) {
				lexemes = append(lexemes, lexeme{kind: piece.Type, value: piece.Value, line: line, first: first})
				first = false
			}
		}
		if n := strings.Count(token.Value, "\n"); n > 0 {
			line += n
			first = true
		}
	}
	lines := strings.Split(src, "\n")
	tags := fileTags{refs: map[string]int{}}
	for i, lex := range lexemes {
		switch {
		case !nameToken(lex.kind):
		case l.defines(lexemes, i, lines[lex.line-1]):
			tags.defs = append(tags.defs, newDefinition(lex.value, lex.line, strings.TrimRight(lines[lex.line-1], " \t")))
		default:
			tags.refs[lex.value]++
		}
	}
	return tags
}

// pieces is a token without surrounding space; a name token holding more than
// an identifier (`os.path`, `@decorator`, `App() {` from the TypeScript lexer)
// is split into its identifiers and single punctuation characters.
func pieces(token chroma.Token) []chroma.Token {
	value := strings.TrimSpace(token.Value)
	if value == "" {
		return nil
	}
	if !nameToken(token.Type) || identifier(value) {
		return []chroma.Token{{Type: token.Type, Value: value}}
	}
	var split []chroma.Token
	for value != "" {
		end := strings.IndexFunc(value, notIdentRune)
		if end < 0 {
			end = len(value)
		}
		if end > 0 {
			if identifier(value[:end]) {
				split = append(split, chroma.Token{Type: token.Type, Value: value[:end]})
			}
		} else {
			_, end = utf8.DecodeRuneInString(value)
			if r := value[:end]; strings.TrimSpace(r) != "" {
				split = append(split, chroma.Token{Type: chroma.Punctuation, Value: r})
			}
		}
		value = value[end:]
	}
	return split
}

// defines reports whether the name lexeme at i is a definition on its line.
func (l *language) defines(lexemes []lexeme, i int, text string) bool {
	lex := lexemes[i]
	var prev, next *lexeme
	if i > 0 && !lex.first {
		prev = &lexemes[i-1]
	}
	if i+1 < len(lexemes) && lexemes[i+1].line == lex.line {
		next = &lexemes[i+1]
	}
	indented := text != "" && (text[0] == ' ' || text[0] == '\t')
	if prev != nil && prev.kind.InCategory(chroma.Keyword) {
		if flag, ok := l.keywords[prev.value]; ok {
			switch flag {
			case kwTop:
				return !indented
			case kwBody:
				return next == nil || next.value == "{" || next.value == ":"
			case kwAlias:
				return next != nil && (next.value == "=" || next.value == "<")
			}
			return true
		}
	}
	if next == nil || next.value != "(" {
		return false
	}
	switch {
	case l.declarators && prev != nil:
		scoped := prev.value == "::" || prev.value == ":" && i > 1 && lexemes[i-2].value == ":"
		return typeLike(*prev) || scoped && !indented
	case l.methods && indented && strings.HasSuffix(strings.TrimSpace(text), "{") && !strings.Contains(text, "=>"):
		for j := i - 1; j >= 0 && lexemes[j].line == lex.line; j-- {
			if !modifiers[lexemes[j].value] {
				return false
			}
		}
		return i+2 >= len(lexemes) || !lexemes[i+2].kind.InSubCategory(chroma.LiteralString)
	}
	return false
}

// modifiers may come before a declared name.
var modifiers = map[string]bool{
	"public": true, "private": true, "protected": true, "internal": true, "static": true, "final": true,
	"abstract": true, "virtual": true, "override": true, "inline": true, "extern": true, "async": true,
	"get": true, "set": true, "readonly": true, "export": true, "default": true, "synchronized": true,
}

// typeLike reports whether a lexeme can end the type before a declared name.
func typeLike(lex lexeme) bool {
	switch {
	case lex.kind == chroma.KeywordType, nameToken(lex.kind):
		return true
	case lex.kind.InCategory(chroma.Keyword):
		return modifiers[lex.value]
	}
	return lex.value == "*" || lex.value == "&" || lex.value == ">" || lex.value == "]"
}

func nameToken(kind chroma.TokenType) bool {
	return kind.InCategory(chroma.Name) && !kind.InSubCategory(chroma.NameBuiltin)
}

func identifier(value string) bool {
	return value != "" && !unicode.IsDigit([]rune(value)[0]) && strings.IndexFunc(value, notIdentRune) < 0
}

func notIdentRune(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '$'
}
