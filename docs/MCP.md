# MCP

matrixclaw speaks the Model Context Protocol in both directions:

- as an **MCP client**, the daemon connects MCP servers you configure and offers
  their tools to the assistant as ordinary matrixclaw tools;
- as an **MCP server**, `matrixclaw mcp serve` offers matrixclaw's tools to
  another MCP host over stdio.

Only tools are bridged. MCP prompts, resources, roots, elicitation and sampling
are not.

## MCP client

### Configure servers

Open `/modules mcp` (**Modules → MCP**) in the terminal or the Telegram owner
chat. Turn **External MCP** on, then **Add Server**: give it an ID and edit its
name, transport, command, args, endpoint, tool prefix, read-only flag and
timeout. New servers start disabled; enable them on the server's screen. Only
the owner can change MCP settings.

Or edit `setup.json` directly:

```json
{
  "modules": {
    "mcp": {
      "enabled": true,
      "servers": [
        {
          "id": "github",
          "enabled": true,
          "transport": "stdio",
          "command": "npx",
          "args": ["-y", "@modelcontextprotocol/server-github"],
          "env": { "GITHUB_PERSONAL_ACCESS_TOKEN": "..." }
        },
        {
          "id": "docs",
          "enabled": true,
          "transport": "http",
          "endpoint": "http://127.0.0.1:3333/mcp",
          "read_only": true
        }
      ]
    }
  }
}
```

| Field | Meaning |
|---|---|
| `modules.mcp.enabled` | Turns all user-defined servers on or off. |
| `id` | Server ID: lowercased, non-alphanumeric runs become `_`. Used in permission rules. |
| `name` | Display name (defaults to the ID). |
| `enabled` | Whether to connect this server. |
| `transport` | `stdio` (default) or `http` (streamable HTTP; `streamable_http` is accepted too). |
| `command`, `args` | Program and arguments of a stdio server. |
| `env` | Extra environment variables for a stdio server. It also inherits the daemon's environment. Not editable from `/modules`. |
| `endpoint` | URL of an http server. |
| `tool_prefix` | Prefix of the tool IDs; defaults to the ID. Must be unique. |
| `read_only` | Treat every tool of this server as read-only (no approval). |
| `timeout_seconds` | Connect timeout (default 15 s) and per-call timeout (default 2 min). Servers added from `/modules` get 30. |

A stdio server needs a `command` and an http server an `endpoint`; an
incomplete entry is refused when saved from `/modules` and skipped when read
from the file. The ID and the tool prefix `browser` are reserved for the
[Browser module](BROWSER.md).

### How servers run

Changes apply without a daemon restart: added or changed servers connect,
removed ones disconnect, and the others keep their connection. A server that
fails to connect is reported in the module status (`GET /v1/modules`,
**Modules**) while the rest keep working. A server that stops is restarted on
the next call to one of its tools; a call that was running when it died is not
repeated.

### Tool IDs

Each remote tool becomes `mcp_<prefix>_<remote tool>`, lowercased with other
characters turned into `_`. A tool `search_issues` on server `github` becomes
`mcp_github_search_issues`. The remote name is kept as it is, so a server whose
tools already start with its own name doubles it (`mcp_browser_browser_click`).
`GET /v1/tools` lists the registered IDs.

Text and structured results are passed to the model. Image and audio results
reach it only as a placeholder such as `<image mime=image/png bytes=48213>`.

### Approvals and permission rules

Remote servers can wrap browsers, databases, cloud APIs or shells, so their
tools ask for approval by default:

- tools of a server with `read_only: false` ask for approval, unless a
  permission rule or the session's permission mode allows them;
- tools of a server with `read_only: true` run without asking;
- on the managed browser server, only the tools that read the open page skip
  approval (see [Browser](BROWSER.md)).

Permission rules for MCP tools use the tool name `mcp` and match
`<server id>__<remote tool>` with `*` wildcards:

```text
/permissions add allow mcp github__search_*    every search tool of the github server
/permissions add deny mcp github__* global     every tool of the server, in all sessions
```

Mark a server `read_only` only when it cannot change anything; database,
browser, cloud, email, ticketing and shell servers should stay approval-gated
unless their own configuration enforces read-only access.

Tool calls and results are stored in the session history like any other tool.

## MCP server

`matrixclaw mcp serve` runs a stdio MCP server that offers the daemon's tool
registry (`GET /v1/tools`) to an MCP host and runs each call through
`POST /v1/tools/execute` in one matrixclaw session. It starts the daemon if it
is not running.

```bash
matrixclaw mcp serve --session SESSION_ID [--workdir DIR]
```

| Option | Meaning |
|---|---|
| `--session`, `-s` | Session the calls run in. Required, or set `MATRIXCLAW_MCP_SESSION_ID`. |
| `--workdir`, `-C` | Working directory passed to tool calls (defaults to the session's). |

Configure it in the host as a stdio server command, for example:

```json
{
  "mcpServers": {
    "matrixclaw": {
      "command": "matrixclaw",
      "args": ["mcp", "serve", "--session", "sess_123", "--workdir", "/path/to/project"]
    }
  }
}
```

Calls follow the session's permission rules and mode. A call that needs
approval is not run: the host gets an error result
`matrixclaw approval required: ...` and the approval is left pending in the
session (`GET /v1/approvals`). Approving it runs the call in the session, but
the result is not sent back to the host; allow the tool with a permission rule
if the host should call it directly.
