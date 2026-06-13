# Packaging

`matrixclaw` uses GitHub Releases as the source of truth for installable
artifacts.

Release channels:

- `scripts/install.sh` downloads a release archive and installs `matrixclaw`,
  `matrixclawd`, and `matrixclaw-telephony-gateway` into `~/.local/bin`.
- GitHub Releases provide manual `.tar.gz` downloads and `checksums.txt`.
- Homebrew can use the formula template in `homebrew/` from a tap repository.

Do not duplicate runtime configuration here. Packaging installs binaries;
`matrixclaw setup` and the `matrixclaw service` commands own provider, daemon,
Telegram, storage, and service-file configuration.
