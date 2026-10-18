# Skills

A skill is a directory with a `SKILL.md` (frontmatter `name`, `description`,
optional tags, then Markdown instructions) and optional `scripts/`,
`references/` and `assets/`. The library lives under `$DATA/skills`
(`installed/`, `quarantine/`, `drafts/`) with its rows in the store's
database (`internal/skills`).

- Runs see only **trusted, enabled, active** skills: their list in the system
  prompt, and the full instructions of the ones the session activated
  (`skill_use`) or the user names (`$name` or `/name`).
- Installed skills land **quarantined** (disabled) unless
  `modules.skills.trust_policy` is `trusted`. Drafts are always quarantined,
  whoever writes them: the user's screen, the model's `skill_manage create`
  (which asks first), or a session review.
- The owner trusts, enables, archives or removes skills on
  `/modules skills` (the review queue is `/modules skills section review`).

## Learning from sessions

After a session has done real work, matrixclaw reviews it once in the
background and proposes what it learned: skills to add or improve and facts to
remember. Nothing it proposes is used until the owner approves it.

**When.** When a run ends and the session is left idle (no run, no queued
message, no goal run started), a review starts if all of these hold:

- the session is a native top-level session: not a subagent, not an external
  agent, and none of its runs since the last review answered a reply-once
  address (a Telegram guest or inline query);
- at least `min_tool_steps` model steps that called tools (default 10) were
  recorded since the last review, or the session goal completed since then;
- the last review of the session is at least `interval_hours` old (default 6).
  The time of a review is its `run_steps` row, so the limit holds across
  restarts; a failed model call waits the same interval before another try.

**What it reads.** One model call: the compact model when one is configured
(`daemon.compact_model`), else the session's model. It gets the conversation
since the last review, rendered as summaries render history and cut to the
newest 24k tokens (a third of a smaller window), the skills the session
loaded with their instructions, and the memory entries runs already see. It
looks for corrections of style or workflow by the user, non-trivial techniques
that worked, and loaded skills that proved wrong or outdated, and answers with
JSON:

```json
{"skills": [{"name": "...", "description": "Use when ...", "body": "...", "updates": ""}],
 "memories": [{"scope": "user", "content": "..."}]}
```

At most 3 skills and 5 memories are kept. A skill needs a name (≤ 64
characters), a description that starts with "Use when" (≤ 60 characters) and
a body (≤ 8000 characters) that records lessons, not a log of the session;
`updates` names a loaded skill it rewrites, and an unknown id makes it a new
skill. A memory is one sentence of at most 300 characters; `project` scope
binds it to the session's working directory. Items breaking a rule are
dropped; a reply without a JSON object is ignored. Memories that runs already
see or that are already suggested are not filed again.

**Where results go.**

- Skills become quarantined, disabled drafts tagged `learned` (and
  `update-of-<id>` for a rewrite of a skill), in the review queue
  `/modules skills section review`. Trusting a rewrite leaves the old skill
  as it was; archive or remove it there.
- Memories become pending entries: `/memory`, the `memory` tool and the
  system prompt leave them out. The owner approves or rejects each on
  `/memory pending` (`POST /v1/memory/{id}/approve`, `DELETE
  /v1/memory/{id}`).
- When anything was filed, one notice goes to the session and to where its
  runs are delivered (the owner's Telegram chat), naming both screens. A
  review that finds nothing says nothing.

**What it does not touch.** The review creates no run, holds no session gate
and uses no run or goal budget. It takes a model slot like any generation
(`daemon.model_concurrency`) and stops with the daemon. Its tokens are a
`run_steps` row with stop reason `review` on the session's latest run, so they
show in `/usage` with that run, as `/compact` does.

**Settings** (`setup.json`):

```json
{"modules": {"skills": {"review": {"enabled": true, "min_tool_steps": 10, "interval_hours": 6}}}}
```

Reviews follow the skills module: on while skills are on, unless
`review.enabled` is `false`. They are on by default because nothing they
produce is trusted without the owner and the cost is one call per busy
session at most every few hours. Negative numbers are refused when
setup.json is loaded; 0 keeps the default.
