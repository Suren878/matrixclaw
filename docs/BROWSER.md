# Browser Module

The Browser module runs a local headless Chromium through
[Playwright MCP](https://github.com/microsoft/playwright-mcp) and gives the
assistant its tools: open pages, click, type, fill forms, wait, read the
rendered page and take screenshots. Use it for pages that need JavaScript or
interaction; `web_fetch` reads plain pages over HTTP and never drives a browser
(see [Web Tools](WEB_SEARCH.md)).

## Setup

Requirements: Node.js with `npm` on the daemon's `PATH`.

Open `/modules browser` (**Modules → Browser**) in the terminal or the Telegram
owner chat:

1. **Engine** installs the runtime: `npm install @playwright/mcp@latest` into
   the managed runtime directory, then the Chromium build that version needs.
   On an installed engine the same row deletes the runtime and its Chromium.
2. **Browser Provider** turns the module on with **Local Playwright** (the only
   provider) or off with **Disabled**.
3. **Runtime Mode** picks how the browser profile is kept (see below).

Changes apply at once, without a daemon restart. Only the owner can change
them.

The engine counts as installed only when the Chromium revision the runtime
requires has a real executable. A stale or mismatched revision shows as
**Repair Required**; running **Engine** again removes stale revisions and
installs the right one. The `bash` tool refuses to install or modify the
managed runtime itself, so repairs go through this screen.

Files live under the runtime directory (`~/.local/state/matrixclaw/runtime`
by default; `MATRIXCLAW_RUNTIME_DIR` or `MATRIXCLAW_LOCAL_DIR` move it):

| Path | Contents |
|---|---|
| `browser/playwright-mcp/` | the Playwright MCP package |
| `browser/ms-playwright/` | the managed Chromium |
| `browser/profile/` | the persistent profile (Always Running only) |

`setup.json`:

```json
{
  "modules": {
    "browser": {
      "enabled": true,
      "provider_id": "playwright",
      "provider_config": { "runtime_mode": "per_task" }
    }
  }
}
```

## Runtime modes

| Mode | Value | Profile |
|---|---|---|
| Run Per Task (default) | `per_task` | Isolated, in memory. Cookies and logins are not kept. |
| Always Running | `always_running` | Persistent at `browser/profile/`. Logins survive between tasks and restarts. |

In both modes the browser is headless with a 1280×720 viewport. When the daemon
runs as root on Linux, Chromium is started with `--no-sandbox`.

## Tools

When the module is on and installed, the daemon connects the runtime as a
managed MCP server with ID and tool prefix `browser`. Its tools are named
`mcp_browser_<remote tool>`. Playwright MCP's own tool names start with
`browser_`, so the IDs look like:

```text
mcp_browser_browser_navigate
mcp_browser_browser_click
mcp_browser_browser_type
mcp_browser_browser_snapshot
mcp_browser_browser_take_screenshot
```

The exact set depends on the installed Playwright MCP version; `GET /v1/tools`
lists them.

- **Approvals.** Only the tools that read the open page (`browser_snapshot`,
  `browser_console_messages`, `browser_network_requests`,
  `browser_network_request`) run without asking. Everything else, navigation
  included (it can reach private hosts), asks for approval unless a permission
  rule allows it, for example `mcp: browser__browser_navigate`.
- **Screenshots.** MCP image results reach the model only as a placeholder
  (`<image mime=... bytes=...>`); the assistant reads pages through
  `browser_snapshot`, not through pictures.
- **Crashes.** If the browser server stops, it restarts on the next call. A
  call that was running when it died is not repeated.
- Calls time out after 120 s.

The ID `browser` and the tool prefix `browser` are reserved for this module;
user-defined MCP servers cannot use them. See [MCP](MCP.md) for MCP servers in
general, including other browser servers.
