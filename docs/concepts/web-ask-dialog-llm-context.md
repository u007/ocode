---
type: Concept
title: Web Ask Dialog LLM Context Preview
description: Permission/question ask dialogs now show the LLM's last message and thinking for user context
tags:
  - web
  - permission
  - question
  - dialog
  - frontend
  - thinking
  - ask-context
timestamp: 2026-09-19T12:14:42Z
---

## Problem

When ocode's permission or question ask dialog appears, the user sees a
prompt and approve/deny buttons — but no context about *why* the agent
is asking. The SSE `permission` / `question` ask events carry only the
tool name and args; the transcript that produced the ask is not included.
In a multi-round turn this makes the ask opaque.

## Solution

The dialogs now display the LLM's most recent message — its prose and,
when present, its thinking — so the user understands the agent's
reasoning before deciding.

## Dialog surfaces

Two dialogs render the preview, both mounted globally in
`web/src/App.tsx` via `useChat(activeTabId)`:

| Dialog | File | Trigger |
|---|---|---|
| Permission ask | `web/src/components/Chat/PermissionDialog.tsx` | `pendingPermission` |
| Question prompt | `web/src/components/Chat/QuestionDialog.tsx` | `pendingQuestion` |

## AskContextPreview

Shared component `web/src/components/Chat/AskContextPreview.tsx`:

- Renders a `<section aria-label="Last model message">`.
- Assistant prose is always shown when present.
- Thinking block ("🧠 Thinking") renders in a collapsible `<pre>`
  capped at `max-h-40` with `overflow-y-auto`, expanded by default.
- Renders nothing when no context is available.

## Derivation: `extractAskContext`

`extractAskContext(messages, live)` in
`web/src/stores/chatStore.tsx` returns
`AskContext { thinking?: string; text?: string }`.

**Preference order:**
1. **Live buffer** — the in-progress `live` buffer (mid-turn pause) is
   preferred. Tool live-parts delimit assistant messages, so extraction
   skips trailing tool/status/notice parts and walks back over the
   contiguous thinking/text run, treating `status`
   ("Checking permission for …") and `notice` parts as transparent and
   stopping at a tool boundary. This isolates the assistant message behind
   the pending ask in a multi-round turn.
2. **Transcript fallback** — the last assistant message in the
   `permission` or `question` transcript, reading `reasoning_content`
   (thinking) and `content` (prose).

## Client-side rationale

The SSE `permission` / `question` ask events carry only the tool call and
its args — no transcript context. There is no server-side field for it.
Deriving the context client-side from the store is the only available
source.

## Performance

`useChat`'s `askContext` selector returns `null` unless a permission or
question is pending (O(1) common case). An `isEqual` comparator prevents
re-renders of `App` when streamed deltas elsewhere in the store change.

## Test coverage

| Suite | Covers |
|---|---|
| `web/src/stores/chatStore.test.tsx` (`extractAskContext` describe) | Transcript fallback, live preferred, multi-round isolation, transient status/notice transparency, null case |
| `PermissionDialog.test.tsx` | Model context display in permission dialog |
| `QuestionDialog.test.tsx` | Model context display in question dialog |

## Related: pending ask rehydration

Pending asks are rehydrated from the transcript sentinels
`PERMISSION_ASK:` / `QUESTION_PROMPT:` via
`extractPendingFromMessages` in `chatStore.tsx`.
