package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
)

var webFetchClient = &http.Client{
	Timeout: webFetchTimeout * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

func (e *webFetchExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params WebFetchParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(webFetchToolName, err)
	}

	params.URL = strings.TrimSpace(params.URL)
	if params.MaxLength <= 0 {
		params.MaxLength = defaultWebFetchMaxLength
	}
	if params.MaxLength > maxWebFetchMaxLength {
		params.MaxLength = maxWebFetchMaxLength
	}

	if err := validateFetchURL(params.URL); err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, params.URL, nil)
	if err != nil {
		return Result{Content: fmt.Sprintf("cannot build request: %v", err), IsError: true}, nil
	}
	req.Header.Set("User-Agent", "matrixclaw/1.0 (+https://github.com/Suren878/matrixclaw)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,ru;q=0.8")

	resp, err := webFetchClient.Do(req)
	if err != nil {
		return Result{Content: fmt.Sprintf("fetch failed: %v", err), IsError: true}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{
			Content: fmt.Sprintf("server returned %d %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
			Metadata: WebFetchResponseMetadata{URL: params.URL, StatusCode: resp.StatusCode},
			IsError:  true,
		}, nil
	}

	contentType := resp.Header.Get("Content-Type")
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return Result{Content: fmt.Sprintf("reading response: %v", err), IsError: true}, nil
	}

	var title, text string
	if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml") {
		title, text = extractHTMLContent(body)
	} else {
		text = string(body)
	}

	truncated := false
	if len(text) > params.MaxLength {
		text = text[:params.MaxLength]
		truncated = true
	}

	content := fmt.Sprintf("<web_page url=%q", params.URL)
	if title != "" {
		content += fmt.Sprintf(" title=%q", title)
	}
	content += ">\n" + text + "\n</web_page>"

	return Result{
		Content: content,
		Metadata: WebFetchResponseMetadata{
			URL:         params.URL,
			Title:       title,
			StatusCode:  resp.StatusCode,
			ContentType: contentType,
			Truncated:   truncated,
			CharCount:   len(text),
		},
	}, nil
}

// extractHTMLContent parses HTML and returns (title, readable text as markdown).
func extractHTMLContent(body []byte) (string, string) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return "", cleanWhitespace(string(body))
	}

	var title string
	if t := findTitle(doc); t != "" {
		title = t
	}

	var buf strings.Builder
	extractNode(doc, &buf, 0)
	return title, cleanWhitespace(buf.String())
}

// skipTags are HTML elements whose subtrees we skip entirely.
var skipTags = map[string]bool{
	"script": true, "style": true, "noscript": true,
	"head": true, "nav": true, "footer": true, "aside": true,
	"svg": true, "canvas": true, "iframe": true, "form": true,
	"button": true, "input": true, "select": true, "textarea": true,
}

// blockTags are HTML elements that produce a line break before/after.
var blockTags = map[string]bool{
	"p": true, "div": true, "section": true, "article": true,
	"main": true, "header": true, "figure": true, "figcaption": true,
	"blockquote": true, "pre": true, "li": true, "dt": true, "dd": true,
	"tr": true, "td": true, "th": true, "caption": true, "address": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
}

func extractNode(n *html.Node, buf *strings.Builder, depth int) {
	switch n.Type {
	case html.TextNode:
		t := strings.TrimSpace(n.Data)
		if t != "" {
			buf.WriteString(t)
			buf.WriteByte(' ')
		}
		return
	case html.ElementNode:
		tag := strings.ToLower(n.Data)
		if skipTags[tag] {
			return
		}
		if blockTags[tag] {
			buf.WriteByte('\n')
		}
		switch tag {
		case "h1":
			buf.WriteString("# ")
		case "h2":
			buf.WriteString("## ")
		case "h3":
			buf.WriteString("### ")
		case "h4", "h5", "h6":
			buf.WriteString("#### ")
		case "li":
			buf.WriteString("- ")
		case "a":
			href := attrVal(n, "href")
			var linkBuf strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				extractNode(c, &linkBuf, depth+1)
			}
			linkText := strings.TrimSpace(linkBuf.String())
			if href != "" && linkText != "" && !strings.HasPrefix(href, "javascript:") {
				buf.WriteString("[")
				buf.WriteString(linkText)
				buf.WriteString("](")
				buf.WriteString(href)
				buf.WriteString(")")
			} else {
				buf.WriteString(linkText)
			}
			return
		case "strong", "b":
			buf.WriteString("**")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				extractNode(c, buf, depth+1)
			}
			buf.WriteString("**")
			return
		case "em", "i":
			buf.WriteString("*")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				extractNode(c, buf, depth+1)
			}
			buf.WriteString("*")
			return
		case "code":
			buf.WriteString("`")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				extractNode(c, buf, depth+1)
			}
			buf.WriteString("`")
			return
		case "br":
			buf.WriteByte('\n')
			return
		case "hr":
			buf.WriteString("\n---\n")
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extractNode(c, buf, depth+1)
		}
		if blockTags[tag] {
			buf.WriteByte('\n')
		}
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractNode(c, buf, depth+1)
	}
}

func findTitle(n *html.Node) string {
	if n.Type == html.ElementNode && strings.ToLower(n.Data) == "title" {
		if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
			return strings.TrimSpace(n.FirstChild.Data)
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t := findTitle(c); t != "" {
			return t
		}
	}
	return ""
}

func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

var multiNewline = regexp.MustCompile(`\n{3,}`)
var multiSpace = regexp.MustCompile(`[ \t]+`)

func cleanWhitespace(s string) string {
	s = multiSpace.ReplaceAllString(s, " ")
	s = multiNewline.ReplaceAllString(s, "\n\n")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}
