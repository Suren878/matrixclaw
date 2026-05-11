# Security Policy

matrixclaw is currently a local single-user tool. Treat it as experimental and do
not expose the daemon API to untrusted networks.

## Local Daemon API

By default `matrixclawd` refuses to bind the HTTP API to non-loopback addresses.
Remote binding requires `MATRIXCLAW_ALLOW_REMOTE_HTTP=1` and should only be used
behind your own trusted transport controls.

## Secrets

Setup files are written with user-only permissions, but provider keys and
Telegram tokens are still stored as local plaintext configuration in the current
release line. Do not commit files from `~/.config/matrixclaw` or
`~/.local/state/matrixclaw`.

Keep committed examples placeholder-only: use empty strings or environment
variable names such as `$OPENAI_API_KEY`, never real provider keys, bot tokens,
local databases, generated binaries, or personal setup files.

## Reporting Issues

Please report security issues privately through GitHub Security Advisories when
available, or contact the project maintainer privately. Do not open a public
issue with exploit details.
