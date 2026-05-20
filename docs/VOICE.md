# Local Voice

matrixclaw can use local speech runtimes without turning them into separate
apps. Clients ask the daemon for TTS or STT, the daemon reads the selected
module/provider/model from setup, and the result flows back through the same
session.

## Providers

Current local providers:

- Text to Speech: Piper
- Speech to Text: Whisper.cpp

The runtime installer prepares binaries:

```bash
scripts/install_voice_runtime.sh
```

It installs Piper into `~/.local/state/matrixclaw/runtime/piper-venv`, builds
Whisper.cpp under `~/.local/state/matrixclaw/runtime/whisper.cpp`, and installs
`ffmpeg` when the host package manager is supported.

On Ubuntu the installer uses `apt-get` for `git`, `cmake`, `g++`, `make`,
Python venv support, and `ffmpeg`. On macOS it uses Homebrew and requires Xcode
Command Line Tools; install them with `xcode-select --install` if they are not
already present.

The installer does not download every voice or STT model. Voices and models are
chosen later from:

```text
/modules tts
/modules stt
```

Local model files live under `~/.local/state/matrixclaw/local/voice/` by
default. `MATRIXCLAW_LOCAL_DIR` can override that local module root.

Piper can also be installed from the TUI without running the full voice runtime
installer:

```text
/modules -> Text to Speech -> Provider -> Piper -> Piper runtime
```

That row installs or deletes the managed local `piper-tts` runtime. It is
separate from `Add voice`: voices are model files, while Piper runtime is the
native executable that reads those voices.

## Run Modes

Local voice providers support two modes:

- **Run per task:** default mode. Piper or `whisper-cli` starts for one TTS/STT
  request and exits. Idle RAM stays near zero, which is the best default for a
  laptop, VPS, or single-user workstation.
- **Always running:** keeps a local process warm for lower latency. Piper uses a
  managed Piper process. Whisper.cpp uses `whisper-server` and its local
  `/inference` endpoint.

Always-running mode is useful when voice is frequent and startup latency matters.
Run-per-task mode is better when RAM matters more than latency. Whisper.cpp
model size controls peak memory during transcription: `tiny` is lightest,
`base` is the balanced default, and `large-v3` is heavy.

## Catalogs

Piper voices are loaded from the online Piper voice catalog when available, with
bundled English and Russian fallbacks. The TUI groups voices by language so you
can select a language first, then a voice.

Whisper.cpp models are loaded from the upstream model catalog when available,
with bundled size tiers from `tiny` through `large-v3`. STT language can stay on
`Auto`, or you can pin one of Whisper's supported language codes.

Status screens show:

- selected provider and model/voice
- local installation state
- model file storage size
- current runtime RAM when a managed process is running

## Telegram Flow

Telegram voice messages, audio files, and audio documents are downloaded by the
Telegram client and sent to the daemon STT API. The daemon transcribes with the
configured STT provider, then Telegram sends:

```text
Transcribed: <text>
```

The transcribed text is also sent into the active matrixclaw session as the user
message.

Telegram `/tts text to speak` calls the daemon TTS API and sends the generated
audio back as a Telegram voice message. If the assistant uses the
`text_to_speech` tool during a run, Telegram also sends that generated audio
back to the chat.

Generated Telegram TTS audio is saved into local storage under:

```text
telegram/audio/
```

## Limits

Telegram voice/audio uploads are capped at 25 MB before transcription. The local
STT API accepts JSON request bodies up to 36 MB, which accounts for base64
overhead around that raw audio size.

Piper returns WAV audio for local TTS. Telegram rejects generated audio over
25 MB, so very long TTS responses should be shortened or split by the user.

## Privacy

Piper and Whisper.cpp run locally. The selected voice/model files stay on the
machine, and the audio is processed by local binaries in local mode. Audio can
still leave the machine if you select a non-local voice provider or forward the
result to an external LLM provider as part of a session prompt.
