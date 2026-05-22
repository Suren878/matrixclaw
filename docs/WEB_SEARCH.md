# Web Search

matrixclaw exposes two tools to the assistant: `web_search` and `web_fetch`.
Both are active whenever the assistant runs a coding session. No extra setup is
required — DuckDuckGo works out of the box with no API key.

## Tools

### `web_search`

Runs a search query and returns titles, URLs, and descriptions.

```
query  – the search string (required)
limit  – max results to return, 1–20, default 8
```

Result format:
```xml
<web_search query="..." provider="tavily" count=5>

[1] Page title
    URL: https://example.com
    Short description or excerpt.

...
</web_search>
```

### `web_fetch`

Fetches a URL and returns the page content as readable text (HTML stripped,
converted to plain markdown-like text).

```
url         – the URL to fetch (required)
max_length  – character limit, 1 000–100 000, default 20 000
```

Result format:
```xml
<web_page url="https://example.com" title="Page Title">
...extracted text...
</web_page>
```

Private/internal addresses (localhost, RFC 1918 ranges, link-local, cloud
metadata endpoints) are blocked before any network request is made.

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
