# Security Policy

## Reporting a vulnerability

Report it privately through GitHub Security Advisories ("Report a
vulnerability" on the repository's Security tab), or contact the maintainer
privately. Do not open a public issue with exploit details.

## Security model

matrixclaw is a single-user tool that runs as your user and can run commands
and change files on your machine. It is experimental; do not expose the daemon
API to untrusted networks.

### Daemon API

- `matrixclawd` binds to loopback. A non-loopback address is refused unless
  `MATRIXCLAW_ALLOW_REMOTE_HTTP=1` is set, and even then only with an API
  token. Put remote access behind your own trusted transport.
- Every request except `GET /v1/health` needs `Authorization: Bearer <token>`.
  Setup generates the token (`daemon.api_token` in setup.json);
  `MATRIXCLAW_API_TOKEN` overrides it. Anyone with the token has full access.
- Clients assert a role in `X-Matrixclaw-Role` (`owner`, `member`, `guest`;
  absent means owner). The daemon trusts it as it trusts the token. Only the
  owner can change settings, providers, MCP servers, skills, external agents,
  permission modes and global rules, or restart/stop the daemon. Members and
  guests cannot use sessions that run unattended (`full_auto` or external
  agent), guests keep no permission rules, and non-owners never see key
  previews or MCP secrets.

### Telegram

The bot answers only the configured numeric user id. Your private chat with it
acts as the owner; inline and guest-mode requests act as a guest. The role
comes from the chat Telegram reports, never from message text.

### Tool approvals

- Every tool call goes through the same permission check, whether it comes
  from a run, the tools API, voice or `matrixclaw mcp serve`. Rules (`allow`,
  `ask`, `deny`; per session or global) match bash commands, paths, domains
  and MCP tools; deny wins over ask over allow.
- Tools that change things ask by default: `write`, `edit`, `multiedit`,
  `bash`, `task_kill`, `send_file`, `storage_delete`, `skill_manage`, memory
  changes, scheduled AI tasks, `telephony_call`, and every MCP tool that is not
  read-only (browser tools included). Approvals are stored and survive
  restarts; an approved call runs once.
- Permission modes: `default`; `accept_edits` also allows `write` and `edit`
  inside the working directory; `full_auto` allows every call no rule denies or
  asks for.
- Read-only tools (`read`, `grep`, `glob`, `ls`, `web_search`, `web_fetch`) run
  without asking and can reach any file your user can. Add `deny` rules to
  fence paths you want kept from the model.
- After a crash, a mutating call that may have started is never re-run without
  a fresh approval.
- Read-only subagents cannot run mutating tools.
- External agent sessions (Codex, Claude Code) run with the agent's own
  sandbox: `full_auto` gives them full access without approvals.

### Protected paths and network

- `read` and `grep` refuse credential files: setup.json and `daemon.env`
  (and their backups), the ChatGPT login store
  (`~/.local/state/matrixclaw/auth/`), Codex `auth.json` and Claude Code
  `.credentials.json`. `bash` is not covered by this list; it asks by default.
- `web_fetch` reaches public addresses only: private, loopback, link-local and
  cloud-metadata targets are refused, and every redirect is checked again.
- Inbound phone calls are rejected unless
  `MATRIXCLAW_TELEPHONY_INBOUND_ALLOWED_CALLERS` lists the caller.

### Secrets

Provider keys and the Telegram bot token are stored in plaintext in
`~/.config/matrixclaw/setup.json` and `daemon.env`, both mode 0600. Never
commit files from `~/.config/matrixclaw` or `~/.local/state/matrixclaw`.
Committed examples use empty strings or environment variable names such as
`$OPENAI_API_KEY`.
