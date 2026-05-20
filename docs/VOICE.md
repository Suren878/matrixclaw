# Local Voice

matrixclaw can use local speech runtimes without turning them into separate
apps. Clients ask the daemon for TTS or STT, the daemon reads the selected
module/provider/model from setup, and the result flows back through the same
session. Terminal, Telegram, and future clients all use this same daemon-owned
voice configuration.

## Providers

Current local providers:

- Text to Speech: Piper, Supertonic 3
- Speech to Text: Whisper.cpp

The runtime installer prepares binaries:

```bash
scripts/install_voice_runtime.sh
```

It installs Piper into `~/.local/state/matrixclaw/runtime/piper-venv`,
Supertonic into `~/.local/state/matrixclaw/runtime/supertonic-venv`, builds
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

Piper, Supertonic, and Whisper.cpp can also be installed from the TUI without
running the full voice runtime installer:

```text
/modules -> Text to Speech -> Setup Provider -> Piper -> Engine
/modules -> Text to Speech -> Setup Provider -> Supertonic 3 -> Engine
/modules -> Speech to Text -> Setup Provider -> Whisper.cpp -> Engine
```

The Piper runtime row installs or deletes the managed local `piper-tts`
runtime. It is separate from `Add voice`: Piper voices are model files, while
Piper runtime is the native executable that reads those voices.

The Supertonic runtime row installs the Python SDK with local server support and
runs the official `supertonic download` command. Supertonic voice styles are
built into the shared Supertonic model, so changing M1-M5/F1-F5 does not
download a separate voice file.
Supertonic can encode WAV, FLAC, and OGG/Vorbis natively. Matrixclaw requests
WAV from local TTS runtimes, converts it to MP3 through `ffmpeg`, and returns the
MP3 to clients. Temporary WAV files are removed immediately after the daemon
reads them.

The Whisper.cpp engine row builds local `whisper-cli` and `whisper-server`
under matrixclaw runtime state. If the engine is missing, selecting a Whisper
model can also run a combined flow: build the engine, download the selected
model, select it as active, and enable STT in Run Per Task mode.

## Run Modes

Local voice providers support two modes:

- **Run per task:** default mode. Piper or `whisper-cli` starts for one TTS/STT
  request and exits. Idle RAM stays near zero, which is the best default for a
  laptop, VPS, or single-user workstation. Supertonic also defaults to this
  mode.
- **Always running:** keeps a local process warm for lower latency. Piper uses a
  managed Piper process, Supertonic uses `supertonic serve` on loopback, and
  Whisper.cpp uses `whisper-server` with its local `/inference` endpoint.

Always-running mode is useful when voice is frequent and startup latency matters.
Run-per-task mode is better when RAM matters more than latency. Whisper.cpp
model size controls peak memory during transcription: `tiny` is lightest,
`base` is the balanced default, and `large-v3` is heavy.

The main module screens show the selected provider and run mode. Provider setup
screens handle engine installation, model/voice selection, language, threads,
and runtime mode. Status screens show installed storage and `Used RAM` for the
currently running managed process.

## Catalogs

Piper voices are loaded from the online Piper voice catalog when available, with
bundled English and Russian fallbacks. The TUI groups voices by language so you
can select a language first, then a voice.

Supertonic voice styles are loaded from the Supertonic 3 Hugging Face model tree
when available. The language setting can stay on `Auto`; pin a language code
when text needs explicit language handling. Supertonic 3 supports 31 language
codes plus its automatic fallback.

Whisper.cpp models are loaded from the upstream model catalog when available,
with bundled size tiers from `tiny` through `large-v3`. STT language can stay on
`Auto`, or you can pin one of Whisper's supported language codes.

Status screens show:

- selected provider and model/voice
- local installation state
- model file storage size
- current runtime RAM when a managed process is running

## Text To Speech

TTS has one active provider at a time. `Disabled` makes the assistant report
that local TTS is off; selecting Piper or Supertonic enables that provider after
its engine/model requirements are satisfied.

Piper is the light local TTS option. Install the engine, add a voice by
language, then choose the active voice. Multiple voices can be installed, but
only one Piper voice is active for generation.

Supertonic 3 is the higher-resource local TTS option. Install the engine once,
then choose a voice style. Its language setting can stay on `Auto` for normal
use; pin a language only when text needs explicit handling.

Matrixclaw converts local TTS output to MP3 before returning it to clients.
Telegram sends that MP3 as audio and stores a copy in local Storage under
`telegram/audio/`.

## Speech To Text

STT currently has one local provider: Whisper.cpp. Choose a model tier from the
catalog; Matrixclaw downloads only the selected model. `Auto` language lets
Whisper detect the spoken language, while the language picker can pin a specific
spoken language when detection is undesirable.

Run Per Task starts `whisper-cli` for the current upload and exits afterward.
Always Running starts `whisper-server` on loopback and uses its `/inference`
endpoint. Both modes use the same selected model and language setting.

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

Piper and Supertonic generate WAV audio internally, then Matrixclaw converts the
result to MP3 before sending it to clients or Telegram. Telegram rejects
generated audio over 25 MB, so very long TTS responses should be shortened or
split by the user.

## Privacy

Piper and Whisper.cpp run locally. The selected voice/model files stay on the
machine, and the audio is processed by local binaries in local mode. Audio can
still leave the machine if you select a non-local voice provider or forward the
result to an external LLM provider as part of a session prompt.
