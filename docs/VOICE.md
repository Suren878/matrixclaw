# Voice

matrixclaw has three voice modules and an optional phone gateway:

| Module | `/modules` ID | What it does |
|---|---|---|
| Text to Speech | `tts` | Local speech synthesis with Piper or Supertonic 3. |
| Speech to Text | `stt` | Local transcription with Whisper.cpp. |
| Realtime Voice | `realtime_voice` | Live speech-to-speech through Gemini Live, Grok Voice or OpenAI Realtime. |
| Telephony | `telephony` | Phone calls through Asterisk and `matrixclaw-telephony-gateway`. |

The daemon owns all voice settings; the terminal, Telegram and the iOS app use
the same ones. Settings are changed in `/modules <id>` (terminal or Telegram
owner chat) or with `GET`/`POST /v1/settings/{module}`; only the owner can
change them, and changes apply without a restart.

## Local TTS and STT

### Install the engines

Engines can be installed from the module screens (the **Engine** row of each
provider) or all at once:

```bash
scripts/install_voice_runtime.sh            # all: Piper, Whisper.cpp, Supertonic
scripts/install_voice_runtime.sh --piper    # or --whisper, --supertonic
scripts/install_voice_runtime.sh --no-system-deps
```

The script installs system packages with `apt-get`, `dnf`, `pacman` or
Homebrew (`git`, `cmake`, a C++ compiler, Python with venv, `ffmpeg`); on macOS
it needs the Xcode Command Line Tools (`xcode-select --install`). It then
installs into the runtime directory, `~/.local/state/matrixclaw/runtime` by
default (`MATRIXCLAW_RUNTIME_DIR` overrides it):

| Engine | Path |
|---|---|
| Piper (`piper-tts`) | `piper-venv/` |
| Supertonic 3 (`supertonic[serve]`) | `supertonic-venv/` |
| Whisper.cpp (`whisper-cli`, `whisper-server`, built from source) | `whisper.cpp/` |

`ffmpeg` is required: local TTS output is converted to MP3, and STT input that
is not WAV (such as Telegram's OGG voice messages) is converted to 16 kHz mono
WAV first.

Voices and models are downloaded later, one at a time, from the module screens.
They live under `~/.local/state/matrixclaw/local/voice/` (`MATRIXCLAW_LOCAL_DIR`
moves the `local` directory).

### Text to Speech (`/modules tts`)

One provider is active at a time; **Disabled** turns TTS off. Choosing a
provider whose engine is missing offers to install it.

- **Piper** (about 130 MB RAM): install the engine, then **Voice → Add Voice**,
  pick a language and a voice, and make it active. Several voices can be
  installed; one is used. Voices come from the online Piper catalog, with
  bundled English and Russian fallbacks.
- **Supertonic 3** (about 550 MB RAM): the engine install also downloads the
  shared model, so voice styles (M1–M5, F1–F5) need no extra download.
  Language stays on `Auto` or is pinned to one of 31 languages; threads are
  `Auto`, 2, 4 or 8.

Output is always MP3. While TTS is on, the assistant has the
`text_to_speech` tool (no approval); its audio goes to the client, and Telegram
also saves a copy under `telegram/audio/` in Storage. Telegram's `/tts text`
speaks text directly.

### Speech to Text (`/modules stt`)

Whisper.cpp is the only provider. **Model → Add Model** downloads one size
from the upstream catalog (bundled tiers `tiny` to `large-v3`; `base` is the
default); if the engine is missing, the same step builds it first and turns
STT on. Language stays on `Auto` or is pinned to one Whisper language. The
model size sets the RAM a transcription needs.

Telegram voice messages and audio files are transcribed with it and sent to
the session (see [Telegram](TELEGRAM.md)).

### Run modes

Each local provider has a **Run Mode**:

- **Run Per Task** (default): the engine starts for one request and exits.
  Idle RAM stays near zero; each request pays the start-up (about 1.4 s for
  Piper, 1.2 s for Supertonic).
- **Always Running**: the engine stays up for lower latency. Piper runs as a
  managed process, Supertonic as `supertonic serve` on loopback, Whisper.cpp as
  `whisper-server` on loopback.

The daemon starts the selected always-running engine, stops engines that are
no longer selected, and stops all of them when it exits. Status screens show
the engine state, the run mode and the RAM the running engine uses.

### API and limits

```text
POST /v1/modules/voice/tts    text to MP3
POST /v1/modules/voice/stt    audio (base64 JSON) to text
```

- The STT request body is limited to 36 MB, which is about 25 MB of audio
  after base64 overhead. Telegram refuses voice and audio uploads over 25 MB.
- Telegram refuses generated audio over 25 MB; shorten very long texts.

## Realtime Voice

Realtime voice is live speech-to-speech over a WebSocket, separate from batch
TTS/STT. A client creates a session, streams microphone audio, and receives
assistant audio, transcripts, tool calls and approval updates on the same
connection. The protocol is the same for every provider, so the iOS app and the
telephony gateway do not depend on a provider's wire format.

### Providers and settings (`/modules realtime_voice`)

| Provider | ID | Defaults | Key from |
|---|---|---|---|
| Gemini Live | `gemini_live` | voice `Puck` | `GEMINI_API_KEY`, `GOOGLE_API_KEY` |
| Grok Voice | `grok_voice` | model `grok-voice-latest`, voice `eve` | `XAI_API_KEY`, `GROK_API_KEY` |
| OpenAI Realtime | `openai_realtime` | model `gpt-realtime-2.1`, voice `marin` | `OPENAI_API_KEY` |

Each provider page has API Key, Model, Voice, Language and, under Advanced,
API Key Env and Endpoint. `setup.json` keeps only values that differ from the
defaults:

```json
{
  "modules": {
    "realtime_voice": {
      "enabled": true,
      "provider_id": "grok_voice",
      "providers": {
        "grok_voice": { "api_key_env": "MY_XAI_KEY", "voice_id": "ara", "language": "ru" }
      }
    }
  }
}
```

The API key is taken from the first of: `api_key`; the variable named by
`api_key_env`; a configured LLM provider of the same vendor (`gemini`, `xai`,
`openai`); the vendor's usual variables in the table above.

### Audio and protocol

- Input: raw PCM S16LE, 16 kHz, mono. Output: raw PCM S16LE, 24 kHz, mono.
  Both travel base64-encoded in JSON frames.
- OpenAI Realtime takes 24 kHz input; its adapter resamples the 16 kHz input
  and turns on input transcription with `gpt-live-transcribe`.
- Only final user and assistant transcripts are stored in the session history,
  one turn at a time; audio is never stored. A session created with
  `"persist_mode": "none"` stores nothing.
- At most 8 realtime sessions run at once.
- The model can call the daemon's tools; calls follow the normal permission
  rules and approvals. Non-owner clients cannot open sessions that run tools
  unattended.

```text
GET    /v1/modules/voice/realtime_voice          module, providers, formats
POST   /v1/realtime-voice/sessions               create a session
GET    /v1/realtime-voice/sessions/{id}          session state
GET    /v1/realtime-voice/sessions/{id}/stream   WebSocket
DELETE /v1/realtime-voice/sessions/{id}          close
```

The create request takes optional `session_id`, `client` and `external_key`
(use that client's bound session), `working_dir`, `provider_id`, `model_id`,
`voice_id`, `language`, `system_instruction`, `input_audio`, `output_audio`
(only the formats above are accepted) and `persist_mode`. Without a session
or binding, a new "Voice conversation" session is created. A session that is
created but not streamed is dropped after a minute.

Frames are `{"v":1,"type":...,"seq":...,"voice_session_id":...,"payload":{...}}`.
The client sends `input_audio.append` (`{"audio_base64": ...}`),
`input_audio.end`, `input_text.append`, `response.cancel` and `session.close`.
The daemon sends `session.ready`, `input_transcript.delta`/`.final`,
`assistant_transcript.delta`/`.final`, `assistant_audio.delta`,
`interrupted`, `turn.final`, `tool.call`, `tool.result`,
`approval.requested`, `approval.resolved`, `backpressure`, `error` and
`session.closed`.

## Telephony

Phone calls go through a separate, optional process,
`matrixclaw-telephony-gateway`, so SIP and RTP stay out of the daemon:

- Asterisk handles SIP/PJSIP trunks, registration and routing. The gateway
  drives calls through Asterisk ARI and bridges their audio with
  `externalMedia` RTP into a realtime voice session.
- The daemon keeps the provider choice, prompts, approvals and transcripts.
  Realtime Voice must be set up for calls to work.
- With the Telephony module on and a gateway URL set, the assistant has
  `telephony_call` (always asks for approval): `to`, `objective`, optional `system_instruction`,
  `initial_message` and `profile`. During a call the phone model has only
  `telephony_end_call`.
- After an outbound call, a report with the transcript is sent to the chat that
  asked for it.
- Inbound calls are answered only for numbers on the allowed list; with no list
  every inbound call is rejected.
- Recordings are saved locally and, by default, to temporary Storage under
  `call-records/`.

Update `matrixclawd` and `matrixclaw-telephony-gateway` together.

### Daemon side (`/modules telephony`)

`setup.json` under `modules.telephony`:

| Key | Meaning |
|---|---|
| `enabled` | Offer the telephony tools. |
| `gateway_url` | Gateway address, for example `http://127.0.0.1:8090`. |
| `gateway_token` | Must match the gateway's `MATRIXCLAW_TELEPHONY_TOKEN`. |
| `default_profile` | Gateway profile used when a call names none. |
| `phone_prompt` | Instructions added to every phone call. |

Every call's instructions are built by the daemon from the assistant's name,
the phone prompt and your custom instructions.

### Gateway environment

The gateway is configured only through environment variables. It needs
`MATRIXCLAW_TELEPHONY_ARI_PASSWORD` and `MATRIXCLAW_API_TOKEN` (the daemon's
`daemon.api_token`); without them `GET /v1/health` reports `"ready": false`.

| Variable | Default | Meaning |
|---|---|---|
| `MATRIXCLAW_TELEPHONY_ADDR` | `127.0.0.1:8090` | Gateway HTTP address. |
| `MATRIXCLAW_TELEPHONY_TOKEN` | (none) | Bearer token required by the gateway API when set. |
| `MATRIXCLAW_API_URL` | `http://127.0.0.1:8080` | Daemon URL; set it to your `daemon.http_addr`. |
| `MATRIXCLAW_API_TOKEN` | (none) | Daemon API token. Required. |
| `MATRIXCLAW_TELEPHONY_ARI_URL` | `http://127.0.0.1:18088/ari` | Asterisk ARI URL. |
| `MATRIXCLAW_TELEPHONY_ARI_USER` | `matrixclaw` | ARI user. |
| `MATRIXCLAW_TELEPHONY_ARI_PASSWORD` | (none) | ARI password. Required. |
| `MATRIXCLAW_TELEPHONY_ARI_APP` | `matrixclaw` | ARI (Stasis) application name. |
| `MATRIXCLAW_TELEPHONY_SIP_PROFILE` | `main` | Default SIP profile. |
| `MATRIXCLAW_TELEPHONY_CALLER_ID` | (none) | Caller ID for outbound calls. |
| `MATRIXCLAW_TELEPHONY_RTP_BIND` | `0.0.0.0:40000` | Local RTP address. |
| `MATRIXCLAW_TELEPHONY_RTP_EXTERNAL_HOST` | first non-loopback IPv4 | RTP address announced to Asterisk (`host` or `host:port`). |
| `MATRIXCLAW_TELEPHONY_CALL_TIMEOUT` | `45s` | How long an outbound call may ring. |
| `MATRIXCLAW_TELEPHONY_MAX_CALL_DURATION` | `10m` | Hard limit per call. |
| `MATRIXCLAW_TELEPHONY_INBOUND_ENABLED` | off | Answer inbound calls. |
| `MATRIXCLAW_TELEPHONY_INBOUND_ALLOWED_CALLERS` | (none) | Allowed caller numbers. |
| `MATRIXCLAW_TELEPHONY_INBOUND_GREETING` | `Здравствуйте.` | First phrase on inbound calls. |
| `MATRIXCLAW_TELEPHONY_INBOUND_PROMPT` | (none) | Extra instructions for inbound calls. |
| `MATRIXCLAW_TELEPHONY_RECORD_CALLS` | on | Record calls. |
| `MATRIXCLAW_TELEPHONY_RECORDING_FORMAT` | `mp3` | `mp3`, `wav`, `gsm`, `ulaw`, `alaw` or `sln` (MP3 is made with `ffmpeg`). |
| `MATRIXCLAW_TELEPHONY_RECORDING_DIR` | `~/.local/state/matrixclaw/storage/temporary/call-records` | Local recording directory. |
| `MATRIXCLAW_TELEPHONY_RECORDING_PREFIX` | `call-records` | Storage folder for recordings. |
| `MATRIXCLAW_TELEPHONY_RECORDING_TEMP_STORAGE` | on | Also upload recordings to temporary Storage. |
| `MATRIXCLAW_TELEPHONY_DEBUG_AUDIO` | off | Write WAV captures of call audio for debugging. |
| `MATRIXCLAW_TELEPHONY_DEBUG_AUDIO_DIR` | `~/.local/state/matrixclaw/telephony-debug` | Where debug captures go. |
| `MATRIXCLAW_TELEPHONY_DEBUG_AUDIO_SECONDS` | `20s` | Length of each debug capture. |

Booleans accept `1/0`, `true/false`, `yes/no`, `on/off`. Durations accept Go
durations (`90s`, `2m`) or whole seconds.

`MATRIXCLAW_TELEPHONY_INBOUND_ALLOWED_CALLERS` takes numbers separated by
commas, semicolons, spaces or newlines. Numbers are compared by their digits
only (a leading `+` and punctuation are dropped, and an 11-digit number
starting with `8` is read as `7…`).

## Privacy

Piper, Supertonic and Whisper.cpp run on your machine; local TTS and STT audio
does not leave it. Realtime voice and phone calls stream audio to the selected
cloud provider. Transcripts stored in a session are sent to the session's LLM
provider like any other message.
