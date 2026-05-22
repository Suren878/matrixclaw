# MCP Module

matrixclaw can participate in the Model Context Protocol in both directions:

- as an **MCP client**, connecting configured MCP servers and exposing their
  tools to the assistant as normal matrixclaw tools;
- as an **MCP server**, exposing matrixclaw daemon tools to external MCP hosts
  through a stdio server.

The MCP module follows the daemon-first architecture. The daemon owns sessions,
approvals, tool calls, and local state; MCP is another module plugged into the
same tool registry.

## MCP Client

Configure MCP servers in `setup.json` under `modules.mcp`. The first supported
transports are stdio command servers and streamable HTTP servers.

```json
{
  "modules": {
    "mcp": {
      "enabled": true,
      "servers": [
        {
          "id": "browser",
          "enabled": true,
          "transport": "stdio",
          "command": "npx",
          "args": ["-y", "@modelcontextprotocol/server-playwright"],
          "read_only": false
        },
        {
          "id": "internal_api",
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

When `matrixclawd` starts, it connects to enabled MCP servers, runs `tools/list`,
and registers each remote tool as a matrixclaw tool. Tool IDs are prefixed:

```text
mcp_<server>_<remote_tool>
```

For example, a remote `browser` server tool named `navigate` becomes:

```text
mcp_browser_navigate
```

The assistant sees these tools alongside built-in filesystem, shell, storage,
voice, automation, and plan tools.

## Configuration Fields

`modules.mcp.enabled` controls whether the MCP client module is active.

Each server accepts:

- `id`: stable server ID, used in tool prefixes.
- `name`: optional display name.
- `enabled`: whether this server should be connected.
- `transport`: `stdio` or `http`; defaults to `stdio`.
- `command`: stdio server executable.
- `args`: stdio server arguments.
- `env`: extra environment variables for the stdio server process.
- `endpoint`: streamable HTTP MCP endpoint.
- `tool_prefix`: optional prefix override for generated tool IDs.
- `read_only`: marks all tools from this server as read-only and safe.
- `require_approval`: retained for config clarity; non-read-only MCP tools
  require approval by default.
- `timeout_seconds`: connect and tool-call timeout override.

Invalid or incomplete server entries are ignored during config normalization.

## Safety Model

Remote MCP servers can wrap browsers, databases, APIs, local programs, and other
high-impact systems. matrixclaw therefore treats external MCP tools
conservatively:

- if `read_only` is false, tools are registered as mutation tools and require
  approval;
- if `read_only` is true, tools are registered as read-only and do not require
  approval;
- tool calls and tool results are persisted in the normal matrixclaw session
  history;
- stdio servers inherit the daemon environment plus `env` entries from config.

Only mark a server `read_only` when the server cannot mutate external state.
Database, browser automation, cloud API, email, ticketing, and shell-backed MCP
servers should stay approval-gated unless their own configuration enforces
read-only behavior.

## MCP Server

matrixclaw can also expose its daemon tool registry to any MCP host:

```bash
matrixclaw mcp serve --session SESSION_ID
```

The command runs a stdio MCP server. It ensures the daemon is running, reads the
daemon's `/v1/tools` registry, and proxies MCP `tools/call` requests to
`/v1/tools/execute`.

Use `--workdir` to set the working directory passed to tool calls:

```bash
matrixclaw mcp serve --session sess_123 --workdir /path/to/project
```

You can also set the session through the environment:

```bash
MATRIXCLAW_MCP_SESSION_ID=sess_123 matrixclaw mcp serve
```

External MCP hosts should configure this as a stdio MCP server command.

## Current Limits

The first MCP module layer focuses on tool interoperability:

- MCP client: tools over stdio and streamable HTTP.
- MCP server: matrixclaw tools over stdio.
- MCP prompts, resources, roots, elicitation, and sampling are not bridged yet.
- The setup TUI does not yet include an MCP configuration screen; edit
  `setup.json` directly for now.
- Remote MCP tool approval is coarse per server, not per remote tool.

This is enough to unlock existing MCP tool ecosystems while keeping the core
session, approvals, and tool history model intact.

