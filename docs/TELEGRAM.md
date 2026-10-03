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
/permissions     approval mode (owner chat only) and permission rules
/todo            show or clear the session's todo list
/continue        continue the latest run with a fresh budget
/budget          show or override the session's run budget
/tasks           background tasks and scheduled AI tasks
/modules         manage modules
```

The owner chat is the private chat of `allowed_user_id`. Only it changes the
permission mode, keeps global rules, or starts external agent sessions. Other
chats cannot bind to or send into a session that runs tools without asking
(an external agent session, or one in `full_auto`).

Session selection is stored in the daemon binding for the Telegram external
key. Private chat runs deliver drafts, a run status message, approval
buttons, assistant messages, generated speech, and document deliveries back to
the same chat.

Selecting a configured provider switches the current session immediately. When
that provider exposes more than one model, Telegram opens the model picker next
so the default can be kept or another model can be selected.

## Streaming Replies

In private chats, generated text is shown with `sendMessageDraft`. The draft
does not create a persistent first-character message. Once an assistant segment
is complete, MatrixClaw sends the full formatted text with `sendMessage`.
Intermediate assistant segments and the run status message are sent silently;
the final answer's first chunk uses normal notifications. Telegram's silent
messages may still appear in the notification tray, depending on the client.
Approvals retain their normal notifications.

Draft updates are throttled to the configured stream flush interval (800 ms
by default), and an unchanged draft is refreshed every 15 seconds. A long draft
shows a bounded preview; final delivery splits the entire answer into messages.
Sending the final message dismisses the draft. An empty draft is not used as a
cleanup operation because current Telegram clients show it as “Thinking…”.

Groups and Bot API servers without draft support use an editable message.
The first preview waits for 40 characters, a sentence boundary after at least
12 characters, or 1.5 seconds. Subsequent updates edit that message. A short
finished answer bypasses the buffer. This fallback sends its notification with
the first buffered preview; message edits do not send a new notification.

Preview flood-control responses defer the next preview without sleeping in the
shared delivery loop. Preview failure does not suppress final delivery. Each
successfully delivered chunk is recorded before returning an error, so retrying
a later chunk does not resend the already confirmed prefix. These delivery IDs
are in memory; exactly-once delivery across a worker restart or an ambiguous
network failure is not guaranteed.

## Runs

- **Run status**: once a run calls a tool or waits, it gets one silent status
  message, edited in place instead of one message per tool call. It shows the
  state (working, waiting for approval, waiting for background work, done,
  stopped at the budget or on a loop, failed, canceled), `step n/limit` from
  `GET /v1/runs/{id}/progress`, the running tool and its subject ("Using bash:
  go test ./..."; parallel calls add "(+N more)"), the session's running
  background tasks and the todo list the run saved last. It is edited only
  when its text changes and at most every 2 seconds; the state the run ends
  in is always written. "Message is not modified" is ignored, a deleted or
  uneditable message is replaced by a new one, and a flood wait defers the
  chat's deliveries until `retry_after` passes. A plain answer without tools
  gets no status message. The final answer, approvals, engine notes, errors
  and generated speech remain separate messages; inline and guest targets
  show no status message.
- **Loading**: a run delivery keeps the run's messages and asks the daemon
  only for those above a `seq` cursor (`GET /v1/messages?after_seq=`), which
  sits below the first message that may still change (a call without a
  result, a streaming reply, the run's last message). The first load of a run,
  also after a worker restart, reads the latest 200 messages of the session.
- **Engine notes**: budget and loop warnings and similar notes arrive as silent
  `Note: …` messages. A run that stopped on its budget or on a loop ends with a
  notice and a **Continue** button (`/continue`).
- **Waiting**: a run parked by `await` shows "Waiting for background work" in
  its status message, without the typing indicator.
- **Messages during a run** steer it at its next step and wake a run that is
  waiting for background tasks.
- **Background work**: when background work finishes in an idle session, a
  wake run starts and its reply goes to the chat of the newest run (never a
  guest or inline target). After 20 wake runs in a row without a user message,
  the chat gets one notice per finished task instead.

## Approvals

An approval message offers **Allow**, **Always: session** and, in the owner
chat, **Always: global** when the daemon suggests a rule (for example
`bash: go test:*`), plus **Deny** and **Deny with reason**. Deny with reason
asks for the reason in the next message (`/cancel` aborts). The model receives
`User denied: <reason>` and continues. Guest and inline targets keep no rules.
A subagent's approval (blocking or background) is asked in its parent's chat,
headed with the subagent's name, while the parent's run keeps its own status.

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
