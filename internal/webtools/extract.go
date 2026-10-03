package webtools

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

const (
	// maxPageChars caps the text one fetch returns; core keeps anything past
	// its own limit in a session file.
	maxPageChars = 400_000
	// maxParsedElements bounds the DOM readability scores.
	maxParsedElements = 50_000
	// minArticleChars is the least main content readability must find before
	// the whole body is converted instead.
	minArticleChars = 250
	// scriptShellChars is the visible text below which a page with scripts
	// counts as rendered by JavaScript.
	scriptShellChars = 200
)

// pageContent is the readable form of a response body.
type pageContent struct {
	Title    string
	Text     string
	ScriptJS bool
}

// readContent turns a body into text: HTML through readability and markdown,
// other text decoded by its charset. Binary content is refused.
func readContent(body []byte, contentType string, pageURL *url.URL) (pageContent, error) {
	if strings.TrimSpace(contentType) == "" {
		contentType = http.DetectContentType(body)
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	switch {
	case mediaType == "text/html" || mediaType == "application/xhtml+xml":
		return readHTML(body, contentType, pageURL)
	case isTextMedia(mediaType):
		reader, err := charset.NewReader(bytes.NewReader(body), contentType)
		if err != nil {
			return pageContent{}, fmt.Errorf("decode %s: %w", mediaType, err)
		}
		text, err := io.ReadAll(reader)
		if err != nil {
			return pageContent{}, fmt.Errorf("decode %s: %w", mediaType, err)
		}
		return pageContent{Text: strings.ToValidUTF8(string(text), "")}, nil
	default:
		return pageContent{}, fmt.Errorf("web_fetch reads text pages; this one is %s", mediaType)
	}
}

func isTextMedia(mediaType string) bool {
	return strings.HasPrefix(mediaType, "text/") ||
		strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml") ||
		mediaType == "application/json" || mediaType == "application/xml" ||
		mediaType == "application/javascript" || mediaType == "application/x-ndjson"
}

func readHTML(body []byte, contentType string, pageURL *url.URL) (pageContent, error) {
	reader, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		return pageContent{}, fmt.Errorf("decode html: %w", err)
	}
	doc, err := html.Parse(reader)
	if err != nil {
		return pageContent{}, fmt.Errorf("parse html: %w", err)
	}
	content := pageContent{Title: documentTitle(doc), ScriptJS: scriptShell(doc)}
	parser := readability.NewParser()
	parser.MaxElemsToParse = maxParsedElements
	if article, err := parser.ParseDocument(doc, pageURL); err == nil && article.Node != nil {
		if title := strings.TrimSpace(article.Title()); title != "" {
			content.Title = title
		}
		if text, err := toMarkdown(article.Node, pageURL, false); err == nil && utf8.RuneCountInString(text) >= minArticleChars {
			content.Text = text
			return content, nil
		}
	}
	text, err := toMarkdown(doc, pageURL, true)
	if err != nil {
		return pageContent{}, fmt.Errorf("convert html: %w", err)
	}
	content.Text = text
	return content, nil
}

// toMarkdown converts node with links made absolute; whole drops page chrome
// (navigation, headers, footers, forms) that readability would have dropped.
func toMarkdown(node *html.Node, pageURL *url.URL, whole bool) (string, error) {
	plugins := []converter.Plugin{base.NewBasePlugin(), commonmark.NewCommonmarkPlugin(), table.NewTablePlugin(), strikethrough.NewStrikethroughPlugin()}
	if whole {
		plugins = append(plugins, chromePlugin{})
	}
	var opts []converter.ConvertOptionFunc
	if pageURL != nil {
		opts = append(opts, converter.WithDomain(pageURL.String()))
	}
	out, err := converter.NewConverter(converter.WithPlugins(plugins...)).ConvertNode(node, opts...)
	return strings.TrimSpace(string(out)), err
}

// chromePlugin removes the elements around a page's content.
type chromePlugin struct{}

func (chromePlugin) Name() string { return "chrome" }

func (chromePlugin) Init(conv *converter.Converter) error {
	for _, tag := range []string{"nav", "header", "footer", "aside", "form", "button", "select", "svg", "canvas", "dialog", "template"} {
		conv.Register.TagType(tag, converter.TagTypeRemove, converter.PriorityStandard)
	}
	return nil
}

func documentTitle(doc *html.Node) string {
	var title string
	walk(doc, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.Data == "title" {
			title = strings.Join(strings.Fields(textContent(n)), " ")
			return false
		}
		return title == ""
	})
	return title
}

// scriptShell reports a page that shows almost no text without JavaScript:
// it has scripts, and either a noscript notice or several scripts, while its
// visible text is tiny.
func scriptShell(doc *html.Node) bool {
	scripts, noscript, visible := 0, false, 0
	walk(doc, func(n *html.Node) bool {
		switch {
		case n.Type == html.ElementNode && n.Data == "script":
			scripts++
			return false
		case n.Type == html.ElementNode && n.Data == "noscript":
			noscript = true
			return false
		case n.Type == html.ElementNode && (n.Data == "style" || n.Data == "template" || n.Data == "head"):
			return false
		case n.Type == html.TextNode:
			visible += utf8.RuneCountInString(strings.Join(strings.Fields(n.Data), " "))
		}
		return true
	})
	return scripts > 0 && (noscript || scripts >= 2) && visible < scriptShellChars
}

// walk visits n and its descendants; visit returns false to skip children.
func walk(n *html.Node, visit func(*html.Node) bool) {
	if !visit(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, visit)
	}
}

// capText cuts text to maxPageChars on a rune boundary.
func capText(text string) (string, bool) {
	if len(text) <= maxPageChars {
		return text, false
	}
	cut := maxPageChars
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut], true
}
