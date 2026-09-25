# Reopen Hidden Question Dialog — Implementation Plan

**Design:** `docs/superpowers/specs/2026-09-25-reopen-hidden-question-dialog-design.md`

## User Expectation Checklist

- [✓] X and Escape hide a pending question without calling the server cancellation endpoint.
- [✓] The explicit footer action is labeled `Don't answer` and retains final server-side dismissal semantics.
- [✓] The transcript's existing `Open question` button reopens the same locally hidden request.
- [✓] Same-request recovery/reconciliation does not defeat the local hide; a genuinely new request still opens.
- [✓] Session scoping, stale request guards, answer/dismiss/resolve/reset/rekey cleanup, and remote/local behavior remain correct.
- [✓] Regression tests are written first and prove the old behavior fails.
- [✓] Inline documentation and the web skill guide are aligned; no backend/API/schema change.
- [✓] Targeted tests, typecheck, and relevant broader suites are run, with unrelated concurrent failures reported separately.

## Tasks

1. **Store TDD and state model**
   - Add failing `chatStore` tests for request-ID-scoped hidden state.
   - Add `hiddenQuestionRequestId` to `SessionSlice` and initial/reset/rekey behavior.
   - Add `QUESTION_HIDE` and `QUESTION_SHOW` actions with current-request/stale-ID guards.
   - Make `QUESTION_REQUEST` preserve a hide for the same request ID while opening a new request.
   - Clear hidden state on matching answer/dismiss/resolve and on broad session reset/rekey.

2. **Dialog TDD and UI split**
   - Add failing `QuestionDialog` tests proving X/Escape call a local `onHide`, never `onCancel`.
   - Add explicit `onHide`; keep async `onCancel` for `Don't answer`.
   - Keep overlay interaction prevented and in-flight suppression unchanged.

3. **Wiring and reopen path**
   - Expose the hidden selector/hide action through `useChat` as needed.
   - Gate App's `QuestionDialog` mount on `pendingQuestion.request_id !== hiddenQuestionRequestId`.
   - Change ChatPanel's existing `Open question` action to explicitly show the same request.
   - Ensure all recovery dispatches remain safe through reducer semantics.

4. **Regression and documentation validation**
   - Add/adjust App scope and ChatPanel tests for hidden-pending behavior.
   - Update stale comments that claim X/Escape performs TUI-equivalent final cancellation.
   - Update `skills/ocode-web/SKILL.md` with the local-hide/reopen contract and recovery caveat.
   - Run focused tests first, then `npm run typecheck`, relevant Chat/App suites, and build if practical.
   - Inspect `git diff --check` and the exact changed-file set; do not stage, reset, restore, or alter concurrent WIP.
