# Web Tools

The assistant has two web tools: `web_search` finds pages and `web_fetch` reads
one page. Both are always available and read-only, so they run without
approval unless a permission rule says otherwise. Research across many sources
is the assistant searching and fetching itself, or handing independent
questions to read-only `agent` children that run in parallel.

For pages that need a real browser (JavaScript rendering, logins, clicking,
forms, screenshots) use the [Browser module](BROWSER.md).

## `web_search`

```text
query  search query (required)
limit  number of results, 1-20, default 8
```

Returns a numbered list of titles, URLs and short descriptions from the
configured provider. A provider without its key or URL is skipped for
DuckDuckGo; a provider that fails also falls back to DuckDuckGo, and the result
starts with a note saying why.
Serper returns at most 10 results per query. Requests time out after 15 s.

## `web_fetch`

```text
url  http or https URL (required)
```

Returns the page as markdown, headed by its title and final URL (after
redirects).

- **HTML** goes through readability (the main article, without navigation,
  sidebars and footers) and is converted to markdown with absolute links. When
  readability finds less than 250 characters, the whole body is converted with
  page chrome removed.
- **Text** (`text/*`, JSON, XML, JavaScript, NDJSON) is returned as it is,
  decoded by its charset (undeclared UTF-8 is read as UTF-8). Other content
  types, such as PDFs and images, are refused.
- **Size.** At most 5 MB is downloaded and 400,000 characters returned; a cut
  page ends with a note. A result above about 8,000 tokens is saved to a file in
  the session, and the assistant gets its beginning and end plus the file path
  to read or grep the rest.
- **JavaScript-only pages.** A page with scripts and almost no visible text
  returns what text it has plus a note to open it with the browser tools.
  `web_fetch` never drives a browser itself.
- **Limits.** 15 s timeout, at most 5 redirects, only 2xx responses are read.
  The request goes out directly; proxy environment variables are not used.

### Safety

- Only `http` and `https` URLs are accepted.
- Private and internal addresses are refused before connecting, at every
  redirect and again when dialing (against DNS rebinding): loopback, RFC 1918,
  link-local, CGNAT, IPv6 unique-local and link-local, multicast and reserved
  ranges, and cloud metadata hosts (`169.254.169.254`,
  `metadata.google.internal`, `100.100.100.200`).
- Permission rules for `web_fetch` match the URL's host, for example
  `web_fetch: *.example.com`, and are checked again for every redirect target.

## Search providers

Choose the provider in `/modules web_search` (**Modules → Web Search**) in the
terminal or the Telegram owner chat. Changes apply at once, without a daemon
restart. Only the owner can change them.

| Provider | Cost | Needs | Results |
|---|---|---|---|
| DuckDuckGo (default) | Free | Nothing | DuckDuckGo HTML search |
| Tavily | 1,000 requests/month free | API key (`tvly-…`) from [app.tavily.com](https://app.tavily.com) | Search API built for agents |
| Serper | 2,500 requests/month free | API key from [serper.dev](https://serper.dev) | Google results |
| SearXNG | Free | Your own instance | Results of the engines it aggregates |

Picking a provider that still needs a key or URL opens that field first.
Entering a key or URL switches to its provider. Each provider keeps its own
value, so you can switch back and forth without re-entering keys. To clear a
value, enter `-`; clearing the active provider's value switches back to
DuckDuckGo.

Stored in `setup.json` under `modules.web_search`:

| Key | Value |
|---|---|
| `provider` | `ddg`, `tavily`, `serper` or `searxng` |
| `tavily_key` | Tavily API key |
| `serper_key` | Serper API key |
| `base_url` | SearXNG base URL, must start with `http://` or `https://` |

Clients see the keys only as masked previews, and non-owners only see whether a
key is set.

### SearXNG

matrixclaw calls `<base_url>/search?format=json`, so the instance must allow the
JSON format. With Docker:

```bash
docker run -d --name searxng -p 127.0.0.1:8888:8080 \
  -e SEARXNG_SECRET_KEY="$(openssl rand -hex 32)" \
  searxng/searxng
```

The official image serves only HTML by default. Mount a `settings.yml` that
enables JSON:

```yaml
search:
  formats:
    - html
    - json
```

Then set the base URL to `http://localhost:8888`.
