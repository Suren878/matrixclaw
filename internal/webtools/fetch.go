package webtools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
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
	URL       string
	Content   pageContent
	Truncated bool
}

func (p fetchedPage) String() string {
	var b strings.Builder
	if title := strings.Join(strings.Fields(p.Content.Title), " "); title != "" {
		b.WriteString("# " + title + "\n")
	}
	b.WriteString(p.URL + "\n\n")
	b.WriteString(p.Content.Text)
	if p.Truncated {
		b.WriteString("\n\n[The page is longer; only its beginning was read.]")
	}
	if p.Content.ScriptJS {
		b.WriteString("\n\n[The page shows almost no text without JavaScript. If browser tools are available, open it with them to see the rendered content.]")
	}
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
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/markdown;q=0.9,text/plain;q=0.9,*/*;q=0.5")
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes+1))
	if err != nil {
		return page, fmt.Errorf("reading response: %w", err)
	}
	if len(body) > maxFetchBytes {
		body, page.Truncated = body[:maxFetchBytes], true
	}
	if page.Content, err = readContent(body, resp.Header.Get("Content-Type"), resp.Request.URL); err != nil {
		return page, err
	}
	var cut bool
	page.Content.Text, cut = capText(page.Content.Text)
	page.Truncated = page.Truncated || cut
	return page, nil
}
