package webtools

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

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"

	"golang.org/x/net/html"
)

var webFetchTransport = func() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialPublicAddress
	return transport
}()

// fetchClient follows at most five redirects, each to a public host that the
// call's permission rules allow.
func fetchClient(recheck func(context.Context, permission.Subject) error) *http.Client {
	return &http.Client{
		Transport: webFetchTransport,
		Timeout:   webFetchTimeout * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if recheck != nil {
				if err := recheck(req.Context(), domainSubject(req.URL)); err != nil {
					return fmt.Errorf("redirect to %s: %w", req.URL.Hostname(), err)
				}
			}
			if err := validatePublicURL(req.Context(), req.URL.String()); err != nil {
				return fmt.Errorf("unsafe redirect: %w", err)
			}
			return nil
		},
	}
}

func (webFetchExecutor) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var params WebFetchParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return tools.Result{}, tools.InvalidArgs(webFetchToolName, err)
	}
	page, err := fetchPage(ctx, strings.TrimSpace(params.URL), call.Recheck)
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("web_fetch failed for %s: %v", params.URL, err), Status: tools.ResultStatusError, IsError: true}, nil
	}
	return tools.Result{Content: page.String()}, nil
}

// fetchedPage is one page as the model reads it.
type fetchedPage struct {
	URL   string
	Title string
	Text  string
}

func (p fetchedPage) String() string {
	var b strings.Builder
	if p.Title != "" {
		b.WriteString("# " + strings.Join(strings.Fields(p.Title), " ") + "\n")
	}
	b.WriteString(p.URL + "\n\n")
	b.WriteString(p.Text)
	return strings.TrimSpace(b.String())
}

// maxFetchBytes caps what one fetch downloads.
const maxFetchBytes = 5 << 20

func fetchPage(ctx context.Context, rawURL string, recheck func(context.Context, permission.Subject) error) (fetchedPage, error) {
	if err := validatePublicURL(ctx, rawURL); err != nil {
		return fetchedPage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fetchedPage{}, fmt.Errorf("cannot build request: %w", err)
	}
	req.Header.Set("User-Agent", "matrixclaw/1.0 (+https://github.com/Suren878/matrixclaw)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,ru;q=0.8")

	resp, err := fetchClient(recheck).Do(req)
	if err != nil {
		return fetchedPage{}, fmt.Errorf("fetch failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	page := fetchedPage{URL: resp.Request.URL.String()}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return page, fmt.Errorf("server returned %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
	if err != nil {
		return page, fmt.Errorf("reading response: %w", err)
	}
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml") {
		page.Title, page.Text = extractHTMLContent(body)
	} else {
		page.Text = string(body)
	}
	return page, nil
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
