# Web Search

matrixclaw exposes a unified web research toolset to the assistant. The primary
tool is `web_research`; `web_research_ask` handles follow-up questions by
reusing the saved research session before fetching again. Legacy `web_search`
and `web_fetch` remain available for compatibility, but their outputs are
compact and bounded.

DuckDuckGo works out of the box with no API key. Tavily, Serper, and SearXNG can
be configured from Modules -> Web Search.

## Tools

### `web_research`

Runs a bounded research workflow: search, fetch selected sources, optionally use
browser fallback, store raw artifacts, and return only compact results.

```
task         - research task or question
query        - optional search query; defaults to task
urls         - optional URLs to read directly
max_sources  - 1-12, default 5
depth        - quick, standard, or deep
browser      - auto, always, or never
freshness    - auto, refresh, or cache
async        - auto, true, or false
```

Result format is compact text plus structured metadata:

```text
research_id: wr_...
status: completed

answer:
...bounded answer...

facts:
1. Fact text [source_id]

sources:
1. Page title
   https://example.com

warnings:
- Browser fallback setup hint, blocked fetch, or other caveat.
```


### Browser fallback and browser tools

`web_research` can use a configured browser fallback for pages where direct
fetching is blocked, empty, or too dynamic. That fallback reads the rendered
page, stores text/DOM/screenshot artifacts, and still returns only compact facts
and sources to the main assistant context.

MatrixClaw also supports full interactive browser automation through MCP browser
servers. Those tools are separate from `web_research` and are registered as
normal MatrixClaw tools with `mcp_<server>_<tool>` IDs. For a server configured
with `id: "browser"`, browser actions may appear as tools such as:

```text
mcp_browser_navigate
mcp_browser_click
mcp_browser_type
mcp_browser_screenshot
mcp_browser_wait
```

Exact names depend on the MCP server. If the remote tool is named
`browser_click`, the MatrixClaw tool ID keeps that name after the server prefix.
Non-read-only browser tools require normal MatrixClaw approval.

Raw page text, HTML, DOM snapshots, and screenshots are saved as runtime
artifacts under the web research artifact directory and are not pasted into the
main provider context. Default retention is 30 days.

### `web_research_ask`

Answers a follow-up against a saved `research_id`. It first searches stored
facts/artifacts. If the answer is missing or the freshness policy requires an
update, it performs a follow-up search/fetch/browser pass in the same research
session.

```
research_id  - id returned by web_research
question     - follow-up question
freshness    - auto, refresh, or cache
browser      - auto, always, or never
```

### `web_research_status`

Checks a research session by `research_id`, including background jobs started
with `async=true` or long `async=auto` runs.

### Compatibility tools

`web_search` runs only search and returns compact titles, URLs, and snippets.

`web_fetch` remains active for older prompts, but it is now artifact-first:
without `task`, it fetches one URL, stores raw text/HTML as artifacts when the
web research engine is available, and returns only diagnostic metadata plus
artifact/research references. With `task`, it routes through the same extraction
path as `web_research` and returns compact facts/results for that URL.

For interactive browser tasks such as opening a page, clicking through a flow,
typing into forms, waiting for dynamic content, or taking screenshots, configure
an MCP browser server and use its `mcp_browser_*` tools.

Both compatibility tools stay compact and bounded. Runtime guidance prefers
`web_research` for source-backed answers, current information, ratings,
reviews, comparisons, and follow-up research.

Private/internal addresses (localhost, RFC 1918 ranges, link-local, cloud
metadata endpoints) are blocked before any direct fetch request is made.

## Providers

Configure from **Modules → Web Search** in the terminal TUI or Telegram.
The active provider and its credentials are stored in
`~/.config/matrixclaw/setup.json` and take effect immediately — no daemon
restart needed.

### DuckDuckGo

- **Free, no account or API key required.**
- Scrapes the DuckDuckGo HTML endpoint.
- Used automatically when no other provider is configured.
- Good for general queries; rate limits may apply under heavy use.

### Tavily

- **Free tier: 1 000 requests/month.**
- Designed for AI agents — returns clean excerpts rather than raw snippets.
- Sign up at [app.tavily.com](https://app.tavily.com) to get an API key.
- Keys start with `tvly-`.
- Configure: **Modules → Web Search → Tavily → API Key**.

### Serper

- **Free tier: 2 500 requests/month.**
- Proxies Google Search results.
- Sign up at [serper.dev](https://serper.dev) to get an API key.
- Configure: **Modules → Web Search → Serper → API Key**.

### SearXNG

- **Free, unlimited — self-hosted only.**
- Privacy-preserving meta-search engine that aggregates results from many
  sources simultaneously.
- You run the instance; matrixclaw points at it.
- Configure: **Modules → Web Search → SearXNG → Base URL**.

#### Running SearXNG with Docker

```bash
docker run -d \
  --name searxng \
  -p 8888:8080 \
  -e SEARXNG_SECRET_KEY=$(openssl rand -hex 32) \
  searxng/searxng
```

Then set Base URL to `http://localhost:8888`.

> **Note:** The official SearXNG Docker image disables JSON output by default.
> If searches return errors, mount a custom `settings.yml` that enables it:
>
> ```yaml
> search:
>   formats:
>     - html
>     - json
> ```

## Provider comparison

| Provider   | Cost          | Requires       | Results source          |
|------------|---------------|----------------|-------------------------|
| DuckDuckGo | Free          | Nothing        | DuckDuckGo              |
| Tavily     | Free 1k/mo    | API key        | AI-optimized web index  |
| Serper     | Free 2.5k/mo  | API key        | Google Search           |
| SearXNG    | Free          | Self-hosted    | 70+ configurable sources|

## Credential storage

Each provider stores its credentials independently:

- Tavily key → `modules.web_search.tavily_key`
- Serper key → `modules.web_search.serper_key`
- SearXNG URL → `modules.web_search.base_url`

Switching providers does not clear the other provider's key. You can store
both a Tavily and a Serper key and switch between them instantly.
