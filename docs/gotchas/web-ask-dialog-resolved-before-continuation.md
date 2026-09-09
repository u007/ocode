# Web ask dialogs: broadcast `*_resolved` before the continuation round

**Symptom:** the web/desktop `QuestionDialog` (or `PermissionDialog`) stays on
screen after the user answers, for as long as the model's follow-up round takes.
Looks like "dialog won't close".

**Cause:** the resolve handlers (`HandleAnswerQuestion`,
`HandleResolvePermission` in `internal/server`) inject the answer, then call
`agent.Step` synchronously inside the HTTP request. The browser dismisses the
dialog on either the `question_resolved` / `permission_resolved` SSE frame or
the POST completing — both of which only happen after `Step` returns if the
broadcast sits after the continuation.

**Rule:** broadcast the `*_resolved` frame immediately after the answer is
applied to the working transcript and BEFORE `agent.Step`. Guarded by
`TestHandleAnswerQuestionBroadcastsResolvedBeforeContinuation` and
`TestHandleResolvePermissionBroadcastsResolvedBeforeContinuation`, which gate
the fake model client so the test only passes if the frame arrives while the
model round is still blocked.

Fixed for permissions earlier; questions fixed 2026-09-08.
