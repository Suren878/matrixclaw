# Agent loop and Telegram delivery reliability

This change builds on the existing uncommitted execution/checkpoint work. Its
scope is native model turns, tool feedback, and Telegram reply delivery. It does
not replace the external-agent runtimes or migrate the production database.

## Agent turns

- Generation/retry and tool dispatch now have separate modules. Model retries
  happen before any output is published and before tool execution. Empty replies
  and transient transport failures allow at most two retries with cancellable
  backoff. A partial published answer is preserved as failed instead of replayed.
- OpenAI-compatible HTTP requests retry 408, 429, and 5xx responses, respecting
  numeric/date `Retry-After`. A server delay above 30 seconds ends the request
  with an error rather than retrying earlier than requested. Authentication and
  invalid-request errors do not use this retry path.
- Chat-completion SSE and Anthropic SSE require a terminal event. Chat streams
  also surface error events, reject token-limit/content-filter termination and
  malformed tool arguments, and retain tool calls with sparse stream indices.
  The shared SSE reader can stop at a terminal event without waiting for EOF.
- Assistant commentary is finalized before tool dispatch, including nonstreaming
  model responses. Tool results remain paired with calls that also contain text.
- A repeated tool call ID within a native run reuses its recorded result. Changing
  its name/arguments or reusing an ID from another run is rejected. This does not
  claim exactly-once execution after a crash during an external side effect;
  the existing checkpoint recovery rules still govern that case.
- Gemini's synthesized tool IDs now identify a response as well as an output
  slot. Calling the same function on a later turn no longer overwrites a previous
  call/result or incorrectly activates duplicate-call protection.
- Unknown/invalid tool requests become paired error results for the model to
  correct. The tools themselves are not executed by this recovery path.
- Tool-turn usage is stored in message finish metadata and included in the
  existing per-run usage aggregate. No schema change is needed. Recovery can
  rebuild the total without incrementing it twice.
- The system guidance asks the agent to finish and verify authorized work,
  inspect failures, and avoid claiming success from an intermediate step.

## Telegram

See [Streaming replies](../TELEGRAM.md#streaming-replies) for the user-visible
behavior, fallback, and delivery limits. The renderer persists successful chunk
IDs even when a later chunk fails, replaces an uneditable/deleted preview, and
removes stale overflow if a final response shrinks. Failed/canceled runs show a
terminal status even when a partial answer was already delivered.

## Follow-up audit

- Native Responses decoding now uses the ordered `response.completed.output`
  for final text, function calls, and usage. Text/refusal deltas are previews.
  EOF, `[DONE]` without completion, failed/incomplete/cancelled responses, nested
  and flat error events, and malformed calls cannot become successful output.
  The old partial-tool map and serialization fallback were removed. Explicit
  JSON replies are accepted when a gateway ignores the streaming request.
- Chat-completion JSON and SSE use the same tool validation. A nameless call
  accompanying commentary is no longer silently discarded. JSON responses to
  streaming requests are decoded as JSON instead of retried as empty streams.
- Tool identity comparisons preserve JSON numbers without float rounding, so
  distinct large integer IDs cannot reuse an earlier result. Approved tool
  failures follow the ordinary tool-error path: the model receives the stored
  error result and can explain or correct the failure without an automatic
  replay of the approved side effect.
- Run and document delivery share queue processing and retry scheduling.
  Telegram `retry_after` suspends that destination; other destinations in the
  fetched batch can proceed. Temporary daemon/storage errors remain pending,
  and cancellation does not mark deliveries failed. Inline/guest completion
  now uses the latest assistant response instead of the first commentary.
- Confirmed sends retain a local receipt when daemon acknowledgement fails.
  The next attempt only acknowledges, avoiding duplicate guest answers or file
  uploads. Receipts survive for the worker lifetime (at most 24 hours since the
  last ack attempt), not process restarts; ambiguous Telegram send timeouts
  still cannot guarantee exactly-once delivery. The existing 20-item polling
  batch limit remains.
- Telegram API retry wrappers use one common implementation. Removing stale
  overflow is idempotent when the user has already deleted the old message.

## References reviewed

- [Hermes conversation loop](https://github.com/NousResearch/hermes-agent/blob/8c8003f80b528377d4387b96faa2c00283168d68/agent/conversation_loop.py)
  and [per-turn retry state](https://github.com/NousResearch/hermes-agent/blob/8c8003f80b528377d4387b96faa2c00283168d68/agent/turn_retry_state.py):
  bounded recovery state around model calls, separate tool rounds and finalization.
- [Hermes stream consumer](https://github.com/NousResearch/hermes-agent/blob/8c8003f80b528377d4387b96faa2c00283168d68/gateway/stream_consumer.py):
  throttled previews, tool boundaries, and retaining final delivery after preview failure.
- [OpenClaw Telegram draft stream](https://github.com/openclaw/openclaw/blob/33e8e3c30f49f5490b938b12f65f5d304572c399/extensions/telegram/src/draft-stream.ts):
  initial buffering, flood-control suspension, and final delivery state.
- [Telegram Bot API: sendMessageDraft](https://core.telegram.org/bots/api#sendmessagedraft):
  private-chat ephemeral drafts, stable draft IDs, and persistent final messages.
- [OpenAI Responses streaming events](https://developers.openai.com/api/reference/resources/responses/streaming-events):
  terminal response state, complete ordered output, refusals, and error payloads.

The implementation is local to MatrixClaw; no external runtime dependency was added.

## Verification

Regression tests cover native tool rounds in SQLite, streaming/nonstreaming
commentary, usage aggregation, bounded retries, incomplete output, duplicated
tool IDs, invalid-tool correction, Telegram draft/fallback flows, throttling,
flood waits, short/long answers, partial delivery failure, and deleted previews.
The follow-up adds terminal Responses events, authoritative final text/tool
ordering, large integer arguments, failed approved tools, JSON gateway fallback,
cross-chat flood waits, transient daemon/storage errors, acknowledgement-only
retries for chat/inline/guest/documents, and cancellation during delivery.
Tests use fake providers, local HTTP servers, and fake Telegram APIs. They send
no messages to real Telegram users and make no paid model requests.
