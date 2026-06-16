package webtools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Suren878/matrixclaw/internal/tools"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/Suren878/matrixclaw/internal/webresearch"

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

func (e *webFetchExecutor) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var params WebFetchParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return tools.Result{}, tools.InvalidArgs(webFetchToolName, err)
	}
	params.URL = strings.TrimSpace(params.URL)
	params.Task = strings.TrimSpace(params.Task)

	if params.Task != "" {
		return e.executeWebFetchResearch(ctx, params, true)
	}
	if e.web != nil && e.web.ResearchConfigured() {
		return e.executeWebFetchResearch(ctx, params, false)
	}

	page, err := FetchWebPage(ctx, params.URL, params.MaxLength)
	metadata := WebFetchResponseMetadata{
		URL:         page.URL,
		Title:       page.Title,
		StatusCode:  page.StatusCode,
		ContentType: page.ContentType,
		Truncated:   page.Truncated,
		CharCount:   len(page.Text),
	}
	if strings.TrimSpace(metadata.URL) == "" {
		metadata.URL = strings.TrimSpace(params.URL)
	}
	if err != nil {
		return tools.Result{
			Content:  fmt.Sprintf("web_fetch failed for %s: %v", firstNonEmptyWeb(metadata.URL, params.URL), err),
			Metadata: metadata,
			Status:   tools.ResultStatusError,
			IsError:  true,
		}, nil
	}

	return tools.Result{
		Content:  formatFetchedPageDiagnostics(metadata, "web research artifacts are unavailable because the research engine is not configured"),
		Metadata: metadata,
		Status:   tools.ResultStatusSuccess,
	}, nil
}

func (e *webFetchExecutor) executeWebFetchResearch(ctx context.Context, params WebFetchParams, taskMode bool) (tools.Result, error) {
	if e.web == nil || !e.web.ResearchConfigured() {
		metadata := WebFetchResponseMetadata{URL: params.URL}
		return tools.Result{
			Content:  "web_fetch task extraction requires the web research engine to be configured",
			Metadata: metadata,
			Status:   tools.ResultStatusError,
			IsError:  true,
		}, nil
	}
	task := params.Task
	if task == "" {
		task = "Fetch this URL and store raw content as artifacts without returning raw page text."
	}
	result, err := e.web.Research(ctx, webresearch.ResearchRequest{
		Task:       task,
		URLs:       []string{params.URL},
		MaxSources: 1,
		Browser:    "auto",
		Async:      "false",
	})
	metadata := metadataFromResearchFetch(params.URL, result)
	if err != nil {
		return tools.Result{
			Content:  fmt.Sprintf("web_fetch failed for %s: %v", firstNonEmptyWeb(metadata.URL, params.URL), err),
			Metadata: metadata,
			Status:   tools.ResultStatusError,
			IsError:  true,
		}, nil
	}
	status := tools.ResultStatusSuccess
	isError := false
	switch result.Status {
	case webresearch.StatusFailed:
		status = tools.ResultStatusError
		isError = true
	case webresearch.StatusPending, webresearch.StatusRunning:
		status = tools.ResultStatusNeutral
	}
	if taskMode {
		return tools.Result{Content: webresearch.FormatResult(result), Metadata: result, Status: status, IsError: isError}, nil
	}
	return tools.Result{
		Content:  formatFetchedPageDiagnostics(metadata, "raw page text and HTML were stored as artifacts; call web_fetch with task or use web_research_ask for extraction"),
		Metadata: metadata,
		Status:   status,
		IsError:  isError,
	}, nil
}

func metadataFromResearchFetch(inputURL string, result webresearch.ResearchResult) WebFetchResponseMetadata {
	metadata := WebFetchResponseMetadata{URL: strings.TrimSpace(inputURL), ResearchID: result.ResearchID}
	if len(result.Sources) == 0 {
		return metadata
	}
	source := result.Sources[0]
	metadata.URL = firstNonEmptyWeb(source.URL, metadata.URL)
	metadata.Title = source.Title
	metadata.StatusCode = source.StatusCode
	metadata.ContentType = source.ContentType
	metadata.ArtifactIDs = append([]string(nil), source.ArtifactIDs...)
	return metadata
}

func FetchWebPage(ctx context.Context, rawURL string, maxLength int) (WebFetchedPage, error) {
	rawURL = strings.TrimSpace(rawURL)
	if maxLength <= 0 {
		maxLength = defaultWebFetchMaxLength
	}
	if maxLength > maxWebFetchMaxLength {
		maxLength = maxWebFetchMaxLength
	}

	if err := validateFetchURL(rawURL); err != nil {
		return WebFetchedPage{URL: rawURL}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return WebFetchedPage{URL: rawURL}, fmt.Errorf("cannot build request: %w", err)
	}
	req.Header.Set("User-Agent", "matrixclaw/1.0 (+https://github.com/Suren878/matrixclaw)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,ru;q=0.8")

	resp, err := webFetchClient.Do(req)
	if err != nil {
		return WebFetchedPage{URL: rawURL}, fmt.Errorf("fetch failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	page := WebFetchedPage{
		URL:         rawURL,
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
	}
	if resp.Request != nil && resp.Request.URL != nil {
		page.URL = resp.Request.URL.String()
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return page, fmt.Errorf("server returned %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return page, fmt.Errorf("reading response: %w", err)
	}

	var text string
	if strings.Contains(page.ContentType, "text/html") || strings.Contains(page.ContentType, "application/xhtml") {
		page.Title, text = extractHTMLContent(body)
		page.HTML = string(body)
	} else {
		text = string(body)
	}

	if len(text) > maxLength {
		text = text[:maxLength]
		page.Truncated = true
	}
	page.Text = text
	return page, nil
}

func formatFetchedPageDiagnostics(metadata WebFetchResponseMetadata, note string) string {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "web_fetch: %s\n", strings.TrimSpace(metadata.URL))
	if metadata.ResearchID != "" {
		_, _ = fmt.Fprintf(&b, "research_id: %s\n", metadata.ResearchID)
	}
	if metadata.Title != "" {
		_, _ = fmt.Fprintf(&b, "title: %s\n", strings.Join(strings.Fields(metadata.Title), " "))
	}
	if metadata.StatusCode != 0 {
		_, _ = fmt.Fprintf(&b, "status: %d\n", metadata.StatusCode)
	}
	if metadata.ContentType != "" {
		_, _ = fmt.Fprintf(&b, "content_type: %s\n", metadata.ContentType)
	}
	if metadata.CharCount > 0 {
		_, _ = fmt.Fprintf(&b, "char_count: %d\n", metadata.CharCount)
	}
	if metadata.Truncated {
		b.WriteString("truncated: true\n")
	}
	if len(metadata.ArtifactIDs) > 0 {
		b.WriteString("artifact_ids:\n")
		for _, artifactID := range metadata.ArtifactIDs {
			b.WriteString("- ")
			b.WriteString(artifactID)
			b.WriteByte('\n')
		}
	}
	if note = strings.TrimSpace(note); note != "" {
		b.WriteString("\nnote: ")
		b.WriteString(note)
	}
	return strings.TrimSpace(b.String())
}

func boundWebToolText(value string, maxChars int) string {
	value = strings.TrimSpace(value)
	if value == "" || maxChars <= 0 || len(value) <= maxChars {
		return value
	}
	cut := strings.LastIndex(value[:maxChars], " ")
	if cut < maxChars/2 {
		cut = maxChars
	}
	return strings.TrimSpace(value[:cut]) + "..."
}

func firstNonEmptyWeb(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
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
