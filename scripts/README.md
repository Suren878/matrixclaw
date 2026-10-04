# Scripts

Install, update, uninstall and build scripts. They install binaries and point
to the next step; configuration belongs to `matrixclaw setup`.

| Script | What it does |
|---|---|
| `install.sh` | Installs the release binaries (`matrixclaw`, `matrixclawd`, `matrixclaw-telephony-gateway`) into `~/.local/bin`, then runs `matrixclaw setup`. Re-run it to update. |
| `install_voice_runtime.sh` | Installs optional local voice runtimes: Piper and Supertonic in Python venvs, a whisper.cpp CLI and server build, and `ffmpeg` through the system package manager. Idempotent. |
| `uninstall.sh` | Removes the binaries and the user service; keeps config and state unless `--purge`. |
| `build_release.sh` | Builds the three binaries with version, commit and date stamped through ldflags into `bin/` (`OUT_DIR` changes it). |

## install.sh

```text
install.sh [--version TAG] [--install-dir DIR] [--no-setup] [--from-source] [--voice-runtime] [--self-test]
```

- `--version TAG` installs a specific release (default: latest).
- `--from-source` builds from the local checkout instead of downloading.
- `--voice-runtime` also runs `install_voice_runtime.sh --all`.
- `--self-test` runs shell-level checks without downloading or installing.
- Environment: `MATRIXCLAW_REPO`, `MATRIXCLAW_VERSION`,
  `MATRIXCLAW_INSTALL_DIR`, `MATRIXCLAW_RUN_SETUP=0` (skip setup).

After setup, plain `matrixclaw` opens the TUI and starts the daemon when
needed.

## install_voice_runtime.sh

```text
install_voice_runtime.sh [--piper] [--whisper] [--supertonic] [--all] [--no-system-deps] [--self-test]
```

- With no target flag it installs all three.
- Runtimes go to `~/.local/state/matrixclaw/runtime`; `MATRIXCLAW_STATE_DIR`,
  `MATRIXCLAW_RUNTIME_DIR` and `MATRIXCLAW_WHISPER_CPP_REPO` redirect it.
- Voice models are not downloaded here: pick Piper voices and whisper.cpp
  models later in `/modules tts` and `/modules stt`.
- `--no-system-deps` skips the package manager (apt-get, dnf, pacman, or
  Homebrew on macOS). macOS needs Homebrew and the Xcode Command Line Tools
  (`xcode-select --install`).

## uninstall.sh

```text
uninstall.sh [--purge] [--yes] [--install-dir DIR]
```

`--purge` also removes `~/.config/matrixclaw` and `~/.local/state/matrixclaw`
(`MATRIXCLAW_CONFIG_DIR`, `MATRIXCLAW_STATE_DIR`); `--yes` skips its prompt.
