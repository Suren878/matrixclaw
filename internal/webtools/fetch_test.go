package webtools

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/tools"
	"golang.org/x/text/encoding/charmap"
)

// publicHost is a public address the tests' transport routes to a local server.
const publicHost = "http://93.184.215.14"

// serveAsPublic sends every web_fetch connection to handler; dialed counts them.
func serveAsPublic(t *testing.T, handler http.HandlerFunc) *int {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	dialed := 0
	previous := webFetchTransport
	webFetchTransport = &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		dialed++
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	t.Cleanup(func() { webFetchTransport = previous })
	return &dialed
}

func fetch(t *testing.T, rawURL string) tools.Result {
	t.Helper()
	args, _ := json.Marshal(WebFetchParams{URL: rawURL})
	result, err := NewFetchTool().Execute(context.Background(), tools.Call{Args: args})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestWebFetchReturnsThePageAsMarkdown(t *testing.T) {
	page, err := charmap.Windows1251.NewEncoder().String(`<html><head><title>Новости</title></head><body>
<nav><a href="/">Главная</a></nav>
<main><h1>Заголовок</h1><p>Первый абзац статьи с <a href="/more">ссылкой</a>.</p><p>` + strings.Repeat("Текст статьи. ", 30) + `</p></main>
<footer>Все права защищены</footer></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	serveAsPublic(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, publicHost+"/news/1", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=windows-1251")
		_, _ = w.Write([]byte(page))
	})
	result := fetch(t, publicHost+"/start")
	if result.IsError() || !strings.HasPrefix(result.Content, "# Новости\n"+publicHost+"/news/1\n\n") {
		t.Fatalf("result = %+v", result)
	}
	for _, want := range []string{"Первый абзац статьи с [ссылкой](" + publicHost + "/more)", "Текст статьи."} {
		if !strings.Contains(result.Content, want) {
			t.Errorf("content lacks %q:\n%s", want, result.Content)
		}
	}
	for _, chrome := range []string{"Главная", "Все права защищены", "JavaScript"} {
		if strings.Contains(result.Content, chrome) {
			t.Errorf("content keeps %q:\n%s", chrome, result.Content)
		}
	}
}

func TestWebFetchReturnsPlainTextAsIs(t *testing.T) {
	serveAsPublic(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true}`))
	})
	if result := fetch(t, publicHost+"/api"); result.IsError() || result.Content != publicHost+"/api\n\n{\"ok\": true}" {
		t.Fatalf("result = %+v", result)
	}
}

func TestWebFetchCapsTheText(t *testing.T) {
	serveAsPublic(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(strings.Repeat("я", maxPageChars)))
	})
	result := fetch(t, publicHost+"/big")
	if result.IsError() || !strings.HasSuffix(result.Content, "[The page is longer; only its beginning was read.]") || len(result.Content) > maxPageChars+200 {
		t.Fatalf("result is %d bytes, ends %q", len(result.Content), result.Content[len(result.Content)-80:])
	}
}

func TestWebFetchFailures(t *testing.T) {
	serveAsPublic(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/image":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("\x89PNG\r\n"))
		case "/inside":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	})
	for path, want := range map[string]string{
		"/image":  "web_fetch reads text pages; this one is image/png",
		"/inside": "unsafe redirect",
		"/gone":   "server returned 404 Not Found",
	} {
		if result := fetch(t, publicHost+path); !result.IsError() || !strings.Contains(result.Content, want) {
			t.Errorf("%s: result = %+v, want error with %q", path, result, want)
		}
	}
}

func TestWebFetchNeverConnectsToPrivateHosts(t *testing.T) {
	dialed := serveAsPublic(t, func(http.ResponseWriter, *http.Request) {})
	for _, target := range []string{"http://127.0.0.1:8080/admin", "http://169.254.169.254/latest/meta-data/", "http://[::1]/", "file:///etc/passwd"} {
		if result := fetch(t, target); !result.IsError() {
			t.Errorf("%s: result = %+v", target, result)
		}
	}
	if *dialed != 0 {
		t.Fatalf("dialed %d times", *dialed)
	}
}

func TestWebFetchRedirectPolicyRejectsPrivateTarget(t *testing.T) {
	t.Parallel()

	redirectURL, err := url.Parse("http://127.0.0.1/private")
	if err != nil {
		t.Fatal(err)
	}
	req := &http.Request{URL: redirectURL}
	err = fetchClient(nil).CheckRedirect(req, []*http.Request{{}})
	if err == nil || !strings.Contains(err.Error(), "unsafe redirect") {
		t.Fatalf("CheckRedirect error = %v, want unsafe redirect error", err)
	}
}

func readFixture(t *testing.T, name string, rawURL string) fetchedPage {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	pageURL, _ := url.Parse(rawURL)
	content, err := readContent(body, "text/html; charset=utf-8", pageURL)
	if err != nil {
		t.Fatal(err)
	}
	return fetchedPage{URL: rawURL, Content: content}
}

func TestReadContentKeepsTheMainContentOfRealPages(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		fixture, url, title string
		want, chrome        []string
	}{
		{
			fixture: "wikinews-article.html",
			url:     "https://en.wikinews.org/wiki/Pope_Leo_XIV_visits_four_nations_in_Africa",
			title:   "Pope Leo XIV visits four nations in Africa",
			want:    []string{"His first stop in Algeria marked the first papal visit to the country in history.", "## Sources"},
			chrome:  []string{"Random article", "Recent changes", "Write an article"},
		},
		{
			fixture: "go-docs-tutorial.html",
			url:     "https://go.dev/doc/tutorial/getting-started",
			title:   "Tutorial: Get started with Go",
			want:    []string{"## Install Go", "```\n   package main", "[`go mod init`](https://go.dev/ref/mod#go-mod-init)"},
			chrome:  []string{"Why Go", "Terms of Service"},
		},
	} {
		page := readFixture(t, tc.fixture, tc.url).String()
		if !strings.HasPrefix(page, "# "+tc.title) {
			t.Errorf("%s: starts %q", tc.fixture, page[:min(len(page), 80)])
		}
		for _, want := range tc.want {
			if !strings.Contains(page, want) {
				t.Errorf("%s: lacks %q", tc.fixture, want)
			}
		}
		for _, chrome := range append(tc.chrome, "JavaScript") {
			if strings.Contains(page, chrome) {
				t.Errorf("%s: keeps %q", tc.fixture, chrome)
			}
		}
	}
}

func TestReadContentSaysWhenAPageNeedsJavaScript(t *testing.T) {
	t.Parallel()
	shell := readFixture(t, "excalidraw-shell.html", "https://excalidraw.com/").String()
	if !strings.HasPrefix(shell, "# Excalidraw Whiteboard\n") || !strings.HasSuffix(shell, "open it with them to see the rendered content.]") {
		t.Fatalf("shell page = %q", shell)
	}
	text := strings.Repeat("Readable words. ", 20)
	for page, want := range map[string]bool{
		`<script src="a.js"></script><noscript>Enable JavaScript.</noscript><div id="root">Loading…</div>`: true,
		`<script src="a.js"></script><script src="b.js"></script><div id="app"></div>`:                     true,
		`<script>track()</script><p>Short note.</p>`:                                                       false,
		`<script src="a.js"></script><noscript>Enable JavaScript.</noscript><p>` + text + `</p>`:           false,
		`<p>Tiny.</p>`: false,
	} {
		content, err := readContent([]byte("<html><body>"+page+"</body></html>"), "text/html", nil)
		if err != nil || content.ScriptJS != want {
			t.Errorf("%s: needs JavaScript = %v (err %v), want %v", page, content.ScriptJS, err, want)
		}
	}
	partial, _ := readContent([]byte(`<html><body><script src="a.js"></script><noscript>x</noscript><p>Some text is here.</p></body></html>`), "text/html", nil)
	if !partial.ScriptJS || !strings.Contains(partial.Text, "Some text is here.") {
		t.Fatalf("partial page = %+v", partial)
	}
}
