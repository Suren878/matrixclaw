# Todo List

The native agent keeps one todo list per session for work of three or more
steps. The model owns the list; clients show it.

## Tool

`todo_write{items: [{content, active_form, status}]}` replaces the whole list.

- `status` is `pending`, `in_progress` or `completed`; at most one item is
  `in_progress`.
- `content` names the step in the imperative ("Run the tests");
  `active_form` names it while it is in progress ("Running the tests").
- An empty `items` array clears the list. A list may hold 50 items; an
  item's `content` (required) and `active_form` up to 300 characters each, with runs of
  whitespace (newlines too) collapsed to one space.
- An invalid list is not saved; the model gets an error result that says what
  to correct ("2 items are in_progress; only one may be in progress at a
  time").

The tool never asks for approval. The list is stored per session (table
`session_todos`, together with the run chain that wrote it) and is deleted
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
again. A list left by another run chain is left out once every item is
completed.

## Completion check

When a run replies without calling tools while the list still has open items,
budget remains, and the list was written by this run or a run it continues
(`/continue`) and the model can call tools, the reply is kept and the engine
asks once:

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
  or hides it; a short panel scrolls to keep the item in progress in view.
  Too small a terminal shows the list in a dialog instead.
- **Telegram:** the run's status message shows the list the run saved last
  and is edited as the list changes. The list follows the run's
  `todo_write` calls only: a list emptied by `/todo clear`, `/clear` or the API
  while the run is going stays on it until the run writes the list again.
- **Commands:** `/todo` shows the list, `/todo clear` empties it after a
  confirmation (TUI and Telegram, Matrixclaw sessions only). `/clear` empties
  the list along with the context.
- **API:** `GET /v1/sessions/{id}/todo` returns `{"todo": {...}}`;
  `DELETE /v1/sessions/{id}/todo` empties the list and returns it; an empty
  list has `"items": []`. The
  `todo.updated` event carries the new list; the client snapshot has `todo`.

## Subagents

Child sessions get `todo_write` and keep their own list; the completion check
applies to their runs the same way.
