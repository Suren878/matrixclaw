# Packaging

GitHub Releases are the source of installable artifacts. Pushing a `v*` tag
runs `.github/workflows/release.yml`: it verifies the tree (gofmt, test, build,
vet), builds `linux` and `darwin` archives for `amd64` and `arm64`, and
publishes them with `checksums.txt`. Each archive holds the three binaries,
`README.md`, `LICENSE`, `docs/` and `scripts/install_voice_runtime.sh`.

Install channels:

- `scripts/install.sh` downloads a release archive into `~/.local/bin`
  (see [scripts/README.md](../scripts/README.md)).
- Manual download of a `.tar.gz` from the release page.
- Homebrew: `homebrew/matrixclaw.rb.template` is a formula for a tap
  repository; fill in `VERSION` (without the leading `v`) and the four
  `*_SHA256` values from
  `checksums.txt`.

Packaging installs binaries only; `matrixclaw setup` and `matrixclaw service`
own configuration and service files.
