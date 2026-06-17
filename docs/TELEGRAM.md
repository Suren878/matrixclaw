# Telegram

The Telegram client is a daemon-connected MatrixClaw client. It does not own
sessions; it binds a Telegram user or delivery target to daemon sessions and
uses the shared control-plane command surface.

## Private Chat Sessions

Normal Telegram usage is centered on the user's private bot chat.

```text
/new [title]     create a MatrixClaw session
/sessions        list, select, rename, or delete sessions
/provider        select provider and model
/permissions     change tool approval mode
/modules         manage modules
```

Session selection is stored in the daemon binding for the Telegram external
key. Private chat runs deliver drafts, tool updates, approval buttons,
assistant messages, generated speech, and document deliveries back to the same
chat.

## Inline Mode

Inline mode lets a user type the bot mention from another Telegram chat and
pick one placeholder article. MatrixClaw answers by editing that inline
message.

Flow:

1. Telegram sends an `inline_query`.
2. MatrixClaw returns one personal article result with a "Get answer" button.
3. Telegram sends `chosen_inline_result` when possible, or the callback button
   starts the run as a fallback.
4. The worker sends the request into the user's active MatrixClaw session.
5. Run delivery edits the inline message with progress and final text.

Inline requests use the private-chat binding when available. If there is no
binding, the worker falls back to the first visible non-external-agent session.
If neither exists, the inline message asks the user to select a session in the
private chat.

Inline location data is appended to the request when Telegram supplies it.
Inline TTS tool results are uploaded through the user's private chat when that
target is known; otherwise the inline message stays text-only.

## Guest Mode

Guest mode uses Telegram `guest_message` updates. The worker creates a run with
a `guest` delivery address and answers by `guest_query_id` when the run reaches
a terminal state.

Guest mode is text-only for generated speech. `/tts` in a guest target returns a
text message explaining that guest answers support text only.

## Files And Images

Telegram photos and image documents are downloaded by the Telegram client,
stored as temporary Storage files, and sent to the active session as image parts
that reference local storage paths.

Non-image documents are saved as temporary Storage files under
`telegram/files/`. Telegram replies with the temporary path and points the user
to:

```text
/modules storage
```

Temporary files can be promoted to durable storage by the user or by assistant
tools. See [Storage](STORAGE.md) for paths, limits, and cleanup rules.

## Geolocation

Location messages become text prompts built from the coordinates Telegram
provides. Inline queries can also include location; MatrixClaw appends that
location text to the inline request before starting the run.

## Voice And Audio

Telegram voice messages, audio files, and audio documents are downloaded and
sent to the daemon STT API. The transcription is sent back as:

```text
Transcribed: <text>
```

The transcribed text is then sent into the active session as the user message.

`/tts text` calls the daemon TTS API and sends the generated audio back to the
Telegram target when that target supports audio. Assistant `text_to_speech`
tool results are also delivered as Telegram voice/audio messages for chat
targets. Generated audio is archived in Storage under `telegram/audio/`.

See [Local Voice](VOICE.md) for providers, model paths, run modes, and audio
limits.
