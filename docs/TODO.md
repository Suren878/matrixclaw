# Todo List

The native agent keeps one todo list per session for work of three or more
steps. The model owns the list; clients show it.

## Tool

`todo_write{items: [{content, active_form, status}]}` replaces the whole list.

- `status` is `pending`, `in_progress` or `completed`; at most one item is
  `in_progress`.
- `content` names the step in the imperative ("Run the tests");
  `active_form` names it while it is in progress ("Running the tests").
- An empty `items` array clears the list. A list may hold 50 items.
- An invalid list is not saved; the model gets an error result that says what
  to correct ("2 items are in_progress; only one may be in progress at a
  time").

The tool never asks for approval and a replay after a daemon restart is
harmless, since each call replaces the list.

## Storage

`session_todos` holds one row per session: the items as JSON,
`chain_run_id` (the first run of the `/continue` chain that wrote the list),
`updated_run_id` (the run that wrote it last) and `updated_at`. The row goes
with its session.

## What the model sees

The list is not part of the system prompt. Whenever it changes, the engine's
context note (a hidden `origin: engine_model` message) carries it:

```text
Todo list (keep it current with todo_write):
1. [completed] Read the parser
2. [in_progress] Fix the bug
3. [pending] Run the tests
```

Summaries do not rewrite the list; the context note after a summary carries it
again.

## Completion check

When a run replies without calling tools while the list still has open items,
budget remains, and the list was written by this run or a run it continues
(`/continue`), the reply is kept and the engine asks once:

```text
Your todo list has 2 items open:
1. [in_progress] Fix the bug
2. [pending] Run the tests
Continue with them, or update the list with todo_write and explain why you are stopping.
```

The next reply without tools completes the run. A list left by an unrelated
earlier run never holds a run back; neither does a list that cannot be read.

## Clients

- **TUI:** a side panel shows the list while it has open items; `ctrl+n` shows
  or hides it. Too small a terminal shows the list in a dialog instead.
- **Telegram:** one silent message per run shows the list the run saved last
  and is edited as the list changes.
- **Commands:** `/todo` shows the list, `/todo clear` empties it after a
  confirmation (TUI and Telegram, Matrixclaw sessions only).
- **API:** `GET /v1/sessions/{id}/todo` returns `{"todo": {...}}`;
  `DELETE /v1/sessions/{id}/todo` empties the list and returns it. The
  `todo.updated` event carries the new list; the client snapshot has `todo`.

## Subagents

Child sessions get `todo_write` and keep their own list; the completion check
applies to their runs the same way.
