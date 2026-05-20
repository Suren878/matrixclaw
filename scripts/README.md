# Scripts

This directory holds operational scripts only.

Scripts:
- `install.sh`
  Install release binaries into `~/.local/bin` and start `matrixclaw setup`.
  Re-run it to replace the installed binaries with a newer release, or pass
  `--version TAG` for a specific release. Use `--self-test` for shell-level
  installer checks that do not download or install anything.
  After setup is saved, plain `matrixclaw` opens the terminal TUI and starts
  the daemon when needed. Use `--from-source` for local development installs.
  Use `--voice-runtime` when the user explicitly wants local Piper and
  Whisper.cpp dependencies installed during setup.
- `install_voice_runtime.sh`
  Install optional local voice runtime dependencies. It prepares Piper in a
  local Python venv, builds Whisper.cpp CLI under matrixclaw state, and installs
  `ffmpeg` through the host package manager when available. It is idempotent and
  can be rerun after updates. Use `--piper`, `--whisper`, or `--all` to choose
  runtime targets, and `--no-system-deps` when system packages are managed
  outside the script.
- `uninstall.sh`
  Remove installed binaries and the user service. Keeps config and state unless
  `--purge` is explicitly passed.
- `build_release.sh`
  Build `matrixclaw` and `matrixclawd` with version, commit, and build date
  stamped through Go ldflags. By default it writes local builds to `bin/`;
  GitHub release packaging writes archives to `dist/`. Both directories are
  ignored and should not be committed.

Rule:
- scripts install files and print the next setup step
- scripts keep install/update/uninstall separate from runtime configuration
- scripts do not become a second configuration model
- scripts should build or invoke the canonical binaries `matrixclaw` and `matrixclawd`

Voice runtime notes:
- `install.sh --voice-runtime` calls `install_voice_runtime.sh` after installing
  the release binaries.
- voice runtime files default to `~/.local/state/matrixclaw/runtime`
- voice model files are not downloaded by the scripts; choose Piper voices and
  Whisper.cpp models later from `/modules tts` and `/modules stt`
- macOS voice runtime install requires Homebrew and Xcode Command Line Tools
  (`xcode-select --install`)
- `MATRIXCLAW_STATE_DIR`, `MATRIXCLAW_RUNTIME_DIR`, and
  `MATRIXCLAW_WHISPER_CPP_REPO` can redirect the voice runtime install
