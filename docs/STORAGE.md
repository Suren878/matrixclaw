# Storage And Telegram Files

matrixclaw storage is a daemon module for files that should outlive a single
chat turn. It is local-first: files are written under the daemon state directory,
metadata is tracked locally, and clients work through the daemon API.

## Local Root

By default setup stores SQLite at:

```text
~/.local/state/matrixclaw/matrixclaw.db
```

The storage root is next to that database:

```text
~/.local/state/matrixclaw/storage/
```

If `MATRIXCLAW_DB_PATH` or setup points the daemon at a different database, the
storage root moves next to that database. Storage metadata is kept under
`.matrixclaw/` inside the storage root.

## Stored Files

Stored files are durable local files with metadata:

- relative storage path
- title
- MIME type
- tags
- size and timestamps

The daemon exposes storage through both client commands and assistant tools. In
the TUI or Telegram:

```text
/modules storage
```

The assistant can save generated notes or user documents with `storage_save`,
find them with `storage_list`, read text files with `storage_read`, update
metadata, and request approval before deleting files.

## Temporary Files

Temporary files are for uploads and attachments that may not be worth keeping.
They live under:

```text
~/.local/state/matrixclaw/storage/temporary/
```

Default cleanup settings:

- auto-cleanup enabled
- 7 day TTL
- 5 GB temporary storage cap

Temporary files can be promoted into durable storage from:

```text
/modules storage -> Temporary Files -> Save
```

The assistant can also promote a temporary attachment with `storage_save_temp`
when the user asks to keep an uploaded image or file. Promotion copies the file
into stored files and removes the temporary entry.

## Telegram Upload Flow

Telegram does not hand raw files directly to the model as anonymous blobs.
matrixclaw gives them a local storage path first.

Images:

- Telegram photos and image documents are downloaded by the Telegram client.
- The file is saved as a temporary storage file under `telegram/images/`.
- The active session receives an image message part that references that
  temporary storage path.

Documents:

- Non-image documents are saved as temporary files under `telegram/`.
- Telegram replies with the temporary path and points the user to
  `/modules storage -> Temporary Files`.
- The user or assistant can save the file permanently when it matters.

Voice and audio:

- Telegram voice messages, audio files, and audio documents go through the
  configured Speech to Text provider.
- The transcription is sent back to Telegram and also sent into the active
  session as the user message.

Generated speech:

- Telegram `/tts` and assistant TTS tool results are sent back as Telegram voice
  messages.
- Generated audio is archived in stored files under `telegram/audio/` with
  `telegram`, `generated`, `audio`, and `tts` tags.

## Size Limits

Storage writes default to a 25 MB content limit. Temporary files use the same
per-file limit and also obey the temporary storage cap. Telegram audio uploads
are capped at 25 MB before STT.

These limits keep a single-user daemon predictable on small machines. Large
project files should stay in the project workspace and be referenced by path
when a tool is allowed to read them.
