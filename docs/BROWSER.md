# Browser Module

The Browser module manages local Playwright browser automation for MatrixClaw.
It is separate from web search: web research returns compact facts and sources,
while browser tools can interact with rendered pages when a real browser is
needed.

Open it from the terminal UI:

```text
/modules browser
```

## Provider

The current provider is Local Playwright:

```json
{
  "modules": {
    "browser": {
      "enabled": true,
      "provider_id": "playwright",
      "provider_config": {
        "runtime_mode": "per_task"
      }
    }
  }
}
```

The module reports runtime installation state, managed Chromium installation
state, runtime path, browser executable path, browser cache path, runtime mode,
and runtime process state when a managed process is running.

## Install And Repair

Use `/modules browser -> Install/Repair` to install the managed Playwright MCP
runtime and its managed Chromium browser cache.

MatrixClaw treats the browser as installed only when the required Playwright
Chromium revision contains a real executable. A stale or mismatched browser
revision is shown as `Repair Required`, and running Install/Repair removes stale
Chromium revisions before reinstalling the required one.

## Run Modes

Local Playwright supports two runtime modes:

- `per_task`: start an isolated browser runtime for the current job and stop it
  afterward. This is the default for lower idle memory.
- `always_running`: keep a managed browser profile warm for lower startup
  latency.

The TUI exposes this as:

```text
/modules browser -> Runtime Mode
```

## MCP Relationship

Interactive browser actions are exposed to the assistant as MCP tools. When the
Browser module is enabled and installed, the daemon injects a managed MCP server
with ID `browser`. The MCP module reserves that ID so a user-defined MCP server
does not conflict with the managed browser server.

Remote tools are registered with prefixed IDs such as:

```text
mcp_browser_navigate
mcp_browser_click
mcp_browser_type
mcp_browser_screenshot
mcp_browser_wait
```

Exact names depend on the MCP server's tool names. Non-read-only browser tools
use the normal MatrixClaw approval flow.

## Web Research Relationship

`web_research` can use a browser fallback for pages that direct HTTP fetching
cannot read. That fallback stores raw page artifacts and returns compact facts
and source references to the assistant.

For interactive browser work such as clicking through flows, filling forms,
waiting for dynamic content, or taking screenshots, use the MCP browser tools
provided by this module or by another configured MCP browser server.
