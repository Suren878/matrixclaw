# Telegram

The Telegram bot is a MatrixClaw client that runs inside the daemon
(`matrixclawd`). It binds your Telegram chat to daemon sessions, so a session
started in the terminal can be continued from Telegram and back.

## Setup

1. Create a bot with [@BotFather](https://t.me/BotFather) and copy its token.
2. Find your numeric Telegram user ID (for example with @userinfobot).
3. Run `matrixclaw setup`, open **Channels → Telegram** and set:

| Field | `setup.json` key | Notes |
|---|---|---|
| Enabled | `clients.telegram.enabled` | |
| Bot token | `clients.telegram.bot_token` | Checked with Telegram's `getMe` when you save. |
| Allowed user id | `clients.telegram.allowed_user_id` | Required, numeric. The bot answers only this user. |
| Provider setup | `clients.telegram.allow_provider_setup` | Off by default. When on, provider API keys can be entered from Telegram. |

The daemon starts the bot when the config is saved; there is no separate
process. It registers its command menu with Telegram on start. The worker keeps
two small state files next to the database: `telegram-inline-cache.json` and
`telegram-render-state.json`.

## Who can use it

- Only the allowed user is served, and only in the private chat with the bot.
  Group messages are ignored.
- The private chat is the **owner** chat. Only the owner can change settings
  (`/modules` screens, provider, permission mode, global rules), start external
  agent sessions, and restart or stop the daemon.
- Inline and guest requests run with the restricted **guest** role: they keep no
  permission rules and cannot use a session that runs tools without asking (an
  external agent session or one in `full_auto`).

## Commands

The bot menu lists `/sessions`, `/provider`, `/permissions`, `/context`,
`/todo`, `/memory`, `/skills`, `/modules`, `/tasks`, `/server`, `/help` and
`/cancel`. These also work when typed:

```text
/new [title]   create a session            /usage     token usage
/continue      continue a stopped run      /budget    show or override the run budget
/search        search history              /remind    add a reminder
/status        daemon status               /restart   restart the daemon (owner)
/stop          stop the daemon (owner)     /tts text  speak text with the TTS module
```

The selected session is stored in the daemon as the chat's binding. If none is
selected when you write, the bot shows the session list (or creates a session
when there are none) and asks you to send the message again.

Selecting a provider in `/provider` switches the current session at once; when
the provider has more than one model, the model picker opens next.

## Runs

- **Streaming.** In the private chat a reply streams as a Telegram draft and is
  sent as a normal message when the segment is complete. Where drafts are not
  supported, a message is sent once there is some text and then edited.
  Intermediate segments are silent; the final answer notifies.
- **Status message.** Once a run calls a tool or waits, it gets one silent
  status message that is edited in place (at most every 2 s): the state
  (working, waiting for approval, waiting for background work, done, stopped,
  failed, canceled), `step n/limit`, the running tool ("Using bash: go test
  ./..."; parallel calls add "(+N more)"), running background tasks and the
  todo list. A plain answer without tools gets no status message.
- **Cancel.** The status message has a **⛔ Cancel** button while the run is
  active. `/cancel` cancels the running task of the chat's session and replies
  "Nothing is running." when there is none. While the bot waits for a typed
  answer (such as a denial reason), `/cancel` closes that prompt instead.
- **Messages during a run** steer it ("Sent to the running task.") and wake a
  run that is waiting for background work.
- **Engine notes** (budget or loop warnings) arrive as silent `Note: …`
  messages. A run that stopped on its budget or on a loop ends with a
  **Continue** button, the same as `/continue`.
- **Background work.** When background work finishes in an idle session, a wake
  run starts and its reply goes to the chat that wrote last. After 20 wake runs
  in a row without a message from you, the chat only gets a notice.
- **Restarts.** The bot remembers which messages it sent for a run, so after a
  daemon restart it edits them instead of sending the output again.

## Approvals

An approval message offers **Allow**, **Deny** and **Deny with reason**. When the
daemon suggests a rule (for example `bash: go test:*`) it also offers
**Always: session**, and in the owner chat **Always: global**. Deny with reason
takes the next message as the reason (`/cancel` aborts); the model receives
`User denied: <reason>` and continues. An open prompt expires after 10 minutes,
after which your next message is an ordinary one.

A subagent's approval is asked in its parent's chat, headed with the
subagent's name.

## Inline mode

Type the bot's username in any chat, then your request, and tap the result.
The bot answers by editing that message.

- The request runs in your private chat's session, or the first visible session
  that does not run tools unattended. With neither, it asks you to pick a
  session in the private chat.
- Approvals are asked in the private chat; the inline message says so.
- If a task is already running, the inline message says so and is answered when
  it ends.
- Location shared with the inline query is added to the request. Speech from
  `text_to_speech` is attached when the private chat is known; otherwise the
  answer is text only.

## Guest mode

Telegram guest messages (`guest_message` updates) start a run that answers
once, when it finishes. Guest answers are text only; `/tts` replies that guest
mode supports text answers only.

## Files, images and location

| You send | What happens |
|---|---|
| Photo or image (JPEG, PNG, GIF, WebP; up to 8 MB) | Saved to temporary Storage and sent to the session with your caption ("Describe this image." without one). |
| Other file (up to 25 MB) | Saved to temporary Storage under `telegram/files/`; the bot replies with the path. Keep or delete it from `/modules → Storage → Temporary Files`. |
| Voice message or audio (up to 25 MB) | Transcribed by the STT module; the bot replies `Transcribed: <text>` and sends the text to the session. |
| Location | Sent as a prompt with the coordinates and the street address from OpenStreetMap. |

A message that asks for something "nearby" or "near me" (also in Russian) gets
your location and a list of nearby places from OpenStreetMap added. Without a
location shared in the last 30 minutes, the bot asks you to share one first.

The assistant can send storage files back as documents with the `send_file`
tool (up to 50 MB). Speech from `/tts` or the `text_to_speech` tool is sent as
audio and a copy is saved to Storage under `telegram/audio/`.

See [Storage](STORAGE.md) for storage paths and cleanup, and
[Voice](VOICE.md) for TTS and STT setup.
