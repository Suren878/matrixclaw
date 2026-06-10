# Telegram Inline Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users invoke Matrixclaw from any Telegram chat via inline mode, choose a placeholder result, and have Matrixclaw update that inline message with the final answer.

**Architecture:** Add Telegram inline query and chosen inline result support to the existing Telegram worker. Inline queries return one lightweight article with an inline keyboard so Telegram provides `inline_message_id`; chosen results create a Matrixclaw run with an `inline` delivery target, and the existing delivery loop edits the inline message through `editMessageText`.

**Tech Stack:** Go, Telegram Bot API HTTP client, Matrixclaw daemon client, existing client delivery/run renderer.

---

### Task 1: Telegram Inline API Surface

**Files:**
- Modify: `clients/telegram/types.go`
- Modify: `clients/telegram/api.go`
- Test: `clients/telegram/api_test.go`

- [ ] Add `InlineQuery`, `ChosenInlineResult`, `AnswerInlineQueryRequest`, and `AnswerInlineQuery` API support.
- [ ] Test that `AnswerInlineQuery` posts to `/answerInlineQuery` with an article result and button markup.

### Task 2: Inline Update Routing

**Files:**
- Modify: `clients/telegram/worker.go`
- Create: `clients/telegram/inline.go`
- Test: `clients/telegram/worker_test.go`

- [ ] Include `inline_query` and `chosen_inline_result` in `allowed_updates`.
- [ ] Handle `inline_query` by returning a single article result for non-empty queries.
- [ ] Handle `chosen_inline_result` by creating a Matrixclaw run through `SendMessagePartsModeWithDelivery`.

### Task 3: Inline Delivery Target

**Files:**
- Modify: `clients/telegram/worker_types.go`
- Modify: `clients/telegram/helpers.go`
- Modify: `clients/telegram/delivery_address.go`
- Modify: `clients/telegram/message_transport.go`
- Modify: `clients/telegram/delivery.go`
- Test: `clients/telegram/delivery_test.go`

- [ ] Add `telegramTargetInline`.
- [ ] Encode/decode `DeliveryAddress{Kind:"inline", InlineMessageID:"..."}`.
- [ ] Route inline deliveries to `editMessageText(inline_message_id=...)`.
- [ ] Avoid chat-only features for inline targets, including typing actions and message drafts.

### Task 4: Verification

**Files:**
- Test command scope only.

- [ ] Run `go test -count=1 ./clients/telegram`.
- [ ] Run `go test -count=1 ./internal/controlplane`.
- [ ] Run `go test -count=1 ./...` if targeted tests pass.
- [ ] Run `git diff --check`.
