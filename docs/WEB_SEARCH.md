# Web Search

matrixclaw gives the assistant two web tools: `web_search` finds pages and
`web_fetch` reads one page. Research across many sources is the assistant
searching and fetching itself, or handing independent questions to read-only
`agent` children, which run in parallel and report back.

DuckDuckGo works out of the box with no API key. Tavily, Serper, and SearXNG can
be configured from Modules -> Web Search.

## Tools

### `web_search`

```
query  - search query
limit  - 1-20, default 8
```

Returns titles, URLs, and short descriptions from the configured provider,
falling back to DuckDuckGo when the provider fails.

### `web_fetch`

```
url  - http or https URL
```

Returns the page's main content as markdown, headed by its title and final URL
(after redirects):

- HTML goes through readability (the main article or document body, without
  navigation, sidebars and footers) and is converted to markdown with absolute
  links; when readability finds too little, the whole body is converted with
  page chrome removed. Plain text, markdown, JSON and XML are returned as they
  are. Bodies are decoded by their charset. Other content types are refused.
- At most 5 MB is downloaded and 400,000 characters returned. A result above
  about 8,000 tokens is kept in a file of the session, and the assistant gets
  its beginning and end with the file's path to read or grep the rest.
- A page that shows almost no text without JavaScript (scripts, tiny visible
  text, and a `<noscript>` notice or several scripts) returns whatever text it
  has plus a note to open it with browser tools. web_fetch never drives a
  browser itself.
- Private and internal addresses (localhost, RFC 1918 ranges, link-local, cloud
  metadata endpoints) are refused before connecting, at every redirect, and
  at dial time. `web_fetch` permission rules apply to the URL's host and to
  every redirect.

For interactive work (logging in, clicking through a flow, filling forms,
screenshots) configure an MCP browser server and use its `mcp_browser_*` tools;
see [Browser Module](BROWSER.md). Their actions follow the normal approvals.

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

The module settings (`/v1/settings/web_search`) show the keys only as masked
previews. To remove a stored key or URL, enter `-` in its prompt.
