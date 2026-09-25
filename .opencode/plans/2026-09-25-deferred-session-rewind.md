# Plan: Deferred Durable Session Rewind

## Approved design

Implement `docs/superpowers/specs/2026-09-25-deferred-session-rewind-design.md`.

User decisions:
- “Restore to input” does not immediately remove history.
- The selected user message and every message after it remain visible until the next normal chat send.
- The next normal chat send transactionally removes that tail and appends the edited/sent message.
- Pending rewind and editable draft survive tab switches and full reloads.
- Show a composer banner with a Cancel action.
- Cancel restores the exact pre-Restore draft.
- The server owns a single-use, durable rewind resource with a 24-hour lifetime.
- Slash commands and `!shell` submissions do not consume the rewind.

## Constraints and working-tree safety

- The worktree contains substantial unrelated concurrent WIP. Preserve it; do not reset, clean, stash, or restore unrelated files.
- Several target files already have unrelated edits (`internal/server/agent_session.go`, `internal/server/handler.go`, `internal/server/server.go`, `web/src/api/client.ts`, `web/src/components/Chat/ChatPanel.tsx`, `web/src/components/Chat/ChatInput.tsx`, `web/src/stores/chatStore.tsx`, `web/src/App.tsx`, and TUI/session files may move while work proceeds). Re-read immediately before each edit and use narrow replacements.
- Follow TDD: every regression test must be written and observed failing for the intended reason before production implementation.
- No empty catches. Structured logging before catch-and-rethrow; inline `// intentionally not logged: <reason>` only for known-benign or caller-directed suppression.
- Use modern Go guidance before editing each Go file.
- Use the session store’s explicit replacement path for every shrink; never use an ordinary save to delete rows.
- Never use `INSERT ... ON CONFLICT ... DO UPDATE` for pending-rewind replacement. Inside the immediate transaction, read the existing row and issue a separate `UPDATE` or `INSERT`; pin this with a source/test guard.
- Keep tail deletion, insertion of the new user row, `history_gen` bump, and resource state transition in one SQLite transaction. The consumed/committed row remains keyed by the original token for response-loss lookup.
- Preserve queued-live-snapshot drop semantics through `history_gen` and preserve transcript-only revision semantics: preparing/cancelling a rewind must not change `meta.updated_at`, `history_gen`, revision, or session ordering.
- Keep the existing immediate `/api/sessions/{id}/truncate` endpoint for compatibility unless a focused test proves it can be safely retired; Web Restore must stop using it.
- Do not use Cloudflare Agents SDK or Durable Objects. This is project-local Go/SQLite state.

## User Expectation Checklist

- [ ] Restoring a user message only loads it into the composer; visible history is unchanged immediately.
- [ ] The confirmation copy and Pending rewind banner explain that deletion happens on the next normal chat send.
- [ ] A partially paginated/long chat targets the correct absolute message using `windowStartServerIndex`; the old `hasMore` silent no-op is gone.
- [ ] Pending token, current draft, previous draft, target preview, host/session binding, and expiry survive full reload and tab switching.
- [ ] Restoring another message supersedes the previous armed resource; the latest target wins.
- [ ] Cancel is server-confirmed and restores the exact pre-Restore draft; failed cancel retains the armed state and shows an error.
- [ ] The next normal chat send atomically deletes the selected message and tail, appends the new user row once, and remains deleted after reload/reopen.
- [ ] Resident agent transcript, pending queue, live persistence, and all connected clients reconcile to the shortened authoritative history.
- [ ] Web-controlled TUI (`/rc`) acknowledges only after the same durable transaction succeeds; failure never starts the turn.
- [ ] Remote SSH/WSL routes prepare/status/cancel/send through the owning host.
- [ ] Changed history becomes stale (409) and cannot later delete unseen messages; active turn refusal is 409 with no resource created.
- [ ] Expired/consumed resources return 410, preserve the draft, clear unusable armed UI, and explain that Restore must be performed again.
- [ ] Persistence/transport errors never masquerade as success; the resource remains retryable when rollback occurred.
- [ ] A committed resource is discoverable after a lost HTTP response, preventing duplicate sends.
- [ ] `/reset-id` moves the server resource and local pending record to the new session ID.
- [ ] Normal send, Retry, slash commands, `!shell`, streaming injection, search pagination, and native TUI message-picker behavior remain unchanged.
- [ ] Focused tests, affected-package tests, formatting, typecheck, Go build/vet, and Web production build pass or have explicit baseline-separated blockers.
- [ ] Documentation records the new lifecycle/API/schema and `CHANGES.md` is updated without overwriting concurrent edits.

## Implementation steps

### 1. Re-read seams and establish failing tests

- [ ] Re-read the approved spec and current target files immediately before editing.
- [ ] Run the Modern Go guidelines `list` command for every Go file to be edited.
- [ ] Add session-store regression tests first for:
  - table creation and resource persistence;
  - one-resource-per-session replacement;
  - 24-hour expiry and cancel;
  - legacy raw-preserving migration;
  - target resolution by `user_seq` and legacy index/content fallback;
  - full raw-transcript fingerprint change becoming stale;
  - commit deleting target/tail and inserting the new user row exactly once;
  - commit updating `history_gen` and status without prepare/cancel changing it;
  - committed status surviving close/reopen for response-loss recovery;
  - `/reset-id` moving/updating the resource.
- [ ] Add server regression tests first for:
  - prepare validation and active-turn 409;
  - prepare/cancel not moving transcript revision/history generation;
  - async send with a token committing before persistAck/202;
  - commit failure appending no user message;
  - resident `agentSession` prefix and stale pending queue reconciliation;
  - no duplicate append when a committed token is observed;
  - RC token propagation and ready/error behavior;
  - remote proxy host propagation.
- [ ] Add web tests first for:
  - absolute target calculation in a partially paginated session;
  - no optimistic history truncation on prepare;
  - localStorage-backed record persistence and quota failure rollback;
  - reload hydration and status validation;
  - banner rendering and in-flight duplicate suppression;
  - Cancel restoring the previous draft only after server success;
  - send including `rewindToken` with the correct host;
  - success clearing state and failure preserving draft/resource;
  - committed-status recovery after a transport error;
  - commands and shell leaving the resource armed;
  - rekey moving the local record.
- [ ] Add/adjust TUI RC tests first for durable commit-before-append and no-turn-on-failure.
- [ ] Run each new test command and record the intended pre-fix failure before changing production code.

### 2. Implement the durable session resource

Primary files:
- new `internal/session/pending_rewind.go`
- focused `internal/session/pending_rewind_test.go`
- minimal additive DDL support in `internal/session/sqlitestore.go` if required
- `internal/session/rekey_test.go` or focused rekey coverage

Implementation:
- [ ] Define typed pending-rewind state and typed errors (not found, expired, stale, active, invalid target).
- [ ] Add an on-demand `pending_rewinds` table in the per-session SQLite database. Keep resource state separate from `meta` and `messages` updates.
- [ ] Migrate legacy raw sessions through the existing verified migration before opening the table. This is required by the approved design: the resource lives in the per-session SQLite DB, while legacy `.json`/`.ojsonl` sessions have no SQLite DB; use `loadRawMessages` + the existing read-back-before-delete migration so filtered loader rows are never lost.
- [ ] Store a 256-bit token, target raw index, optional user sequence, full raw transcript fingerprint, state, timestamps, and committed user sequence.
- [ ] Use one immediate transaction and the existing per-session lock for create/cancel/commit/prune.
- [ ] Compute a stable SHA-256 fingerprint from the complete raw stored message sequence; metadata/title changes must not invalidate it.
- [ ] Implement prepare/status/cancel and the atomic rewind+append commit. Return the kept prefix needed to reconcile resident agents.
- [ ] On stale commit, roll back message deletion/new append, mark the resource stale transactionally, and never permit later success.
- [ ] Preserve the committed row until expiry so GET can recover a lost response.
- [ ] Move/update pending resource identity in the existing rekey transaction/copy flow.

### 3. Add server API and transactional send integration

Delivery order within this phase: (a) session store, (b) headless API/send transaction, (c) Web send path, then (d) independently tested RC/TUI, remote-host, and `/reset-id` slices. These are ordered later slices of the approved scope, not deferred requirements; do not report completion until each is implemented or the user explicitly approves deferral in `TODO.md`.

Primary files:
- new `internal/server/handler_rewind.go` plus focused tests
- `internal/server/server.go`
- `internal/server/handler.go`
- `internal/server/agent_session.go`
- `internal/server/session_manager.go` only if a narrow pending-queue replacement method is required
- `internal/server/rc_bridge.go`
- `internal/tui/model.go` and focused RC tests

Implementation:
- [ ] Add authenticated prepare/status/cancel routes beneath the session rewind resource path.
- [ ] Validate project/session ownership, target role/content/user sequence, active turn state, token length, and 24-hour expiry.
- [ ] Prepare/cancel/prune under `sessionTurnLock` before reading turn state, matching the turn job’s lock order; do not acquire the app map lock inside the session transaction.
- [ ] Extend send requests with optional `rewindToken`; leave token-free sends byte-for-byte behaviorally unchanged.
- [ ] For headless async sends, commit inside `executeTurnJob` while holding `sessionTurnLock`, before closing `persistAck`.
- [ ] After commit, set resident `as.messages` to the kept prefix under `as.mu`, replace stale pending entries with only the newly submitted content, and let existing bootstrap/runTurn append the durable user row once.
- [ ] Broadcast the authoritative committed transcript before terminal 202; preserve scroll-shrink detection on clients.
- [ ] For RC sends, add `RewindToken string` and `AckCh chan error` to `RCRequest`; wait for the TUI’s durable commit result before acknowledging the HTTP request.
- [ ] In TUI `rcRequestMsg`, run the shared transaction before appending/starting; on success replace display/bridge transcript with the kept prefix, append the new user message, and signal nil; on failure signal the classified error and do not call `askAgent`.
- [ ] Keep the native TUI message picker’s existing immediate-rewind behavior unchanged.

### 4. Implement reload-safe Web/Desktop state and UI

Primary files:
- `web/src/lib/inputRestore.ts`
- new `web/src/lib/pendingRewindStore.ts` plus tests
- `web/src/stores/chatStore.tsx` and focused tests
- `web/src/api/types.ts`, `web/src/api/client.ts`
- `web/src/hooks/useChat.ts` and tests
- `web/src/components/Chat/MessageBubble.tsx`
- `web/src/components/Chat/ChatPanel.tsx`
- `web/src/components/Chat/ChatInput.tsx`
- focused UI tests
- `web/src/App.tsx` only for rekey wiring

Implementation:
- [ ] Change RestoreDetail to carry the selected message content, absolute target, and optional durable `user_seq`.
- [ ] Compute the absolute target from `windowStartServerIndex + entry.originalIndex`; refuse an unknown anchor instead of silently using a window-relative index.
- [ ] Add typed API methods for prepare/status/cancel and optional `rewindToken` on send, all host-aware.
- [ ] Add a versioned, validated persistence module patterned directly after `editorTabsPersistence` (version key, same-document CustomEvent + `storage` sync, safe parsing, bounded values, explicit test reset). Key records by host + session; do not add ad-hoc unvalidated localStorage access and do not use shared ocode config.
- [ ] Prepare the server resource first, persist the local record second; on local persistence failure DELETE the new resource and preserve the prior draft/history.
- [ ] Replace the old ChatPanel eager truncation listener. Restore must not dispatch `TRUNCATE_MESSAGES` or call `/truncate`.
- [ ] Hydrate pending state/draft on ChatInput mount, validate it with server GET, and keep an expired/stale local draft while clearing unusable armed state.
- [ ] Add the Pending rewind banner above the composer with preview, explanation, and Cancel; mark initial focus behavior consistently with the project dialog policy where relevant.
- [ ] Disable prepare/cancel/send submission while the corresponding request is in flight.
- [ ] Send the token only for a normal non-command, non-shell chat submission. On accepted commit, clear local pending state. On 409/5xx, keep it. On 410/stale, clear armed state but keep the draft. On ambiguous transport failure, GET status before deciding.
- [ ] Rekey the local record in `App.rekeySession` and handle session close cleanup without touching unrelated tab state.
- [ ] Preserve Composer focus/caret behavior, draft history, queueing, and existing command semantics.

### 5. Documentation and durable learning

- [ ] Ask the context agent to update the relevant project concept/gotcha documentation and `skills/ocode-web/SKILL.md`; the context agent is the sole automated writer of the OKF bundle.
- [ ] Update `CHANGES.md` narrowly with the user-visible behavior, resource TTL, failure semantics, and validation commands.
- [ ] Ensure docs state that prepare/cancel do not move transcript revision and that commit uses explicit replacement/history generation.
- [ ] Run the session-learnings-enforcer workflow after the implementation and tests are green; fold only durable, reusable lessons into the appropriate project docs.

### 6. Validation and acceptance

Focused commands (adjust paths only after tests exist):
- [ ] `go test ./internal/session -run 'PendingRewind|Rewind|Rekey' -count=1`
- [ ] `go test ./internal/server -run 'Rewind|RC.*Rewind' -count=1`
- [ ] `go test ./internal/tui -run 'RC.*Rewind' -count=1`
- [ ] `cd web && npm test -- --run src/lib/pendingRewindStore.test.ts src/components/Chat/ChatInput.restore.test.tsx src/hooks/useChat.rewind.test.tsx`
- [ ] `cd web && npm run typecheck`
- [ ] `cd web && npm run build`
- [ ] `gofmt -l` on changed Go files (must be empty)
- [ ] `go test ./internal/session ./internal/server ./internal/tui -count=1`
- [ ] `go vet ./internal/session ./internal/server ./internal/tui`
- [ ] `go build ./...`
- [ ] Full affected Web suite; compare any failures against a pristine worktree if concurrent WIP is responsible.
- [ ] Manual acceptance with a 500+ message session: restore a middle message, verify history remains, reload, verify draft/banner survive, send edited text, verify tail disappears immediately and stays gone after reopen.
- [ ] Manual Cancel acceptance and remote SSH project acceptance.
- [ ] Review the final diff for unrelated changes, logging compliance, pagination invariants, lock ordering, and checklist completion.
