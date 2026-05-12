# Scripts

This directory holds operational scripts only.

Scripts:
- `install.sh`
  Install release binaries into `~/.local/bin` and start `matrixclaw setup`.
  After setup is saved, plain `matrixclaw` opens the terminal TUI and starts
  the daemon when needed. Use `--from-source` for local development installs.
- `uninstall.sh`
  Remove installed binaries and the user service. Keeps config and state unless
  `--purge` is explicitly passed.
- `build_release.sh`
  Build `matrixclaw` and `matrixclawd` with version, commit, and build date
  stamped through Go ldflags.
- `bootstrap/`
  Helper assets for remote or local installation.

Rule:
- scripts install files and print the next setup step
- scripts keep install/update/uninstall separate from runtime configuration
- scripts do not become a second configuration model
- scripts should build or invoke the canonical binaries `matrixclaw` and `matrixclawd`
