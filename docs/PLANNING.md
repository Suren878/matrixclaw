# Planning Mode

Planning Mode is matrixclaw's durable workflow layer for multi-step work inside
a session.

## Model

A session plan contains:

- a goal
- top-level tasks
- optional subtasks
- item statuses: `pending`, `active`, `done`, `skipped`

Tasks with subtasks are treated as sections. The runner executes leaf tasks and
auto-closes parent sections when all children are `done` or `skipped`.

## Runner

The daemon owns plan execution state. It stores a single checkpoint row per
session in `plan_runs`:

- `status`
- `current_item_id`
- `last_run_id`
- `last_error`
- `step_no`
- `attempt`

The TUI asks the daemon to start or resume the plan. The daemon selects the next
executable item, marks it active, and records the checkpoint. The TUI then sends
that one item to the model. After the run completes, core closes the checkpointed
item and advances the runner.

This avoids relying on the model to remember every plan status update.

## Status Flow

Normal flow:

```text
pending item -> active item -> model run -> done item -> next item
```

Blocked flow:

```text
active item -> model reports blocked -> plan_run blocked -> item remains active
```

Completion flow:

```text
all executable items terminal -> parent sections auto-close -> plan_run completed
```

## Rules

- Parent items with open children are not executable.
- `done` and `skipped` items are terminal.
- A successful model run closes only the checkpointed item.
- A blocked model run does not mark the item done.
- Old internal plan prompts are filtered out of future provider context.
- The current plan-run prompt remains visible to the provider for the active run.

## UI

The Terminal TUI renders the plan as a side panel:

- `Run` starts/resumes execution
- `Pause` pauses auto-run in the client
- `Cancel Plan` clears the plan after confirmation
- tasks/subtasks can be created and edited from the panel

The TUI is not the source of truth. It only renders plan state and asks the
daemon to advance the runner.

## Tests

Important cases covered by tests:

- current plan-run prompt reaches the provider, old prompts do not
- blocked output leaves the checkpointed item open
- successful output closes the checkpointed item
- parent sections close when children are terminal
- failed provider runs keep the plan panel open
- tree rendering avoids misleading top-level connector lines
