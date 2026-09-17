---
type: Gotcha
title: 'Question-answer transcript echo: client must mirror server payload byte-for-byte'
description: 'Gotcha: optimistic client rewrite of QUESTION_PROMPT sentinel must match applyQuestionAnswer payload shape exactly; sentinel is never rendered.'
tags:
  - web
  - desktop
  - question
  - transcript
  - sentinel
  - client-echo
timestamp: 2026-09-17T12:33:27Z
---
# Question-answer transcript echo: client must mirror server payload byte-for-byte

## What changed

When a user answers the `question` tool popup in the web/desktop SPA, the chat
transcript now shows the questions and selected answers immediately. Previously
the answered result was only visible as a generic tool block with raw JSON (and
only after the continuation turn's `messages` snapshot landed).

## How it works

1. **Client dispatches `QUESTION_ANSWERED`** (`web/src/hooks/useChat.ts`
   `submitQuestionAnswers`) immediately after a successful
   `api.answerQuestion(...)`, before `QUESTION_RESOLVED`.

2. **Optimistic local rewrite** (`web/src/stores/chatStore.tsx` reducer
   `QUESTION_ANSWERED`): rewrites the `QUESTION_PROMPT:` sentinel tool result
   **in place** with `JSON.stringify(answers)`, mirroring the server's
   `applyQuestionAnswer` (`internal/server/handler_questions.go`) which replaces
   the sentinel with the payload the model receives as the tool result.

3. **Turn-end snapshot replaces with equivalent content** — the local rewrite
   is idempotent; it is a no-op when the sentinel is not in the loaded
   transcript page (deep history / hydrated ask).

4. **Rendering** (`web/src/components/Chat/TurnParts.tsx`):
   - `parseQuestionAnswers(output)` parses the `[{header, question, answers}]`
     array.
   - `QuestionAnswerBlock` renders each header/question with `→ <label>` and
     `→ <label>: "<text>"` for custom rows.
   - `ToolBlock` early-returns the card when `tool === "question"` and the
     result parses as an answer array (branch placed after all hooks).
   - The raw `QUESTION_PROMPT:` sentinel is suppressed from rendering
     everywhere, including the live stream.

## Constraint: payload shape must match byte-for-byte

The optimistic local rewrite deliberately duplicates
`applyQuestionAnswer`'s in-place replacement. Any change to the answer
payload shape (`QuestionAnswerPayload` in `web/src/api/types.ts` —
`{header?, question, answers: [{label, text?, custom?}]}`) must change
**both** the client echo and the server's `questionAnswerPayload` struct
(`internal/server/handler_questions.go`). A mismatch produces a silent
divergence: the model sees one payload shape while the transcript renders
another.

## Constraint: sentinel never rendered

`ToolBlock` suppresses any output starting with `QUESTION_PROMPT:` when
`tool === "question"` (TurnParts.tsx `suppressSentinel`). This applies to
the grouped transcript path (which already filters the sentinel tool message)
and the live stream (which can carry it as tool output until the snapshot
lands). Never bypass this suppression — the sentinel is an internal protocol
marker, not user-facing content.

## Related docs

- `gotchas/web-ask-dialog-resolved-before-continuation.md` — broadcast
  timing (`*_resolved` must fire before `agent.Step`).
- `gotchas/pending-ask-recovery-live-session-state.md` — recovering pending
  asks when the sentinel is absent from the persisted transcript.