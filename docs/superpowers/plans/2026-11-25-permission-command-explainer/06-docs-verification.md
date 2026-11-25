---
type: Plan
timestamp: 2026-11-25T07:19:25Z
---
# Part 06 — Documentation and full verification

**Goal:** Update the concept page through the OKF writer and prove the whole feature with focused, full-suite, and web checks plus the isolation invariants.

**Risk:** standard — docs plus verification; no new behavior.

**Spec:** `docs/superpowers/specs/2026-11-25-permission-command-explainer-design.md`.

**Global constraints this part must honor:**
- No implementation files.
- No new endpoint / request field / response field / config key / schema / migration.
- Full-suite verification runs once before claiming completion.
- Bundle changes go through the bundle lock, log, and regenerated index; never hand-edit `docs/index.md` or `docs/log.md`.

**Files:**
- Update (via `okf:context-writer` only): `docs/concepts/ask-dialog-tool-call-digest.md`
- Reference only: `docs/superpowers/plans/2026-11-25-permission-command-explainer/INDEX.md` and the spec

**Interfaces — consumed:** none new. The verification checks the behavior the implementation must produce; read it from the code, not from this page.

**Red-green steps:**
- [ ] **Step 1: Dispatch the OKF writer.** Brief it self-contained: replace the stale one-shot / no-tools / plain-prose / input-truncation / 409 / no-cancellation / no-cache sections with the as-built behavior (six-tool loop with the exact catalog, 8 KiB exact paging with no cumulative parameter cap and a context-window guard, process-wide ask-keyed coordinator with a 2min/128 cache and semaphore 3, cancellation, metadata-only hardened Git, TUI/web Markdown). Re-derive every `file.go:NNN` anchor by symbol, never by a line-number delta; keep unknown frontmatter keys; run the bundle lock, log, and index sequence. Do not edit `docs/` yourself.

- [ ] **Step 2: Verify the writer's diff.**
`git diff docs/`
Expected: only the concept page changes, plus the `docs/log.md` entry and the regenerated `docs/index.md` section; the page says what the brief said and cites only symbols that exist. If unrelated concurrent docs changed, leave them untouched.

- [ ] **Step 3: Focused Go tests.**
`go test ./internal/agent/ ./internal/server/ ./internal/tui/ ./internal/tool/ -count=1`
Expected: PASS.

- [ ] **Step 4: Full suite.**
`go test ./... -count=1`
Expected: PASS with `gofmt` and `go vet` clean.

- [ ] **Step 5: Web tests and typecheck.**
`cd web && pnpm test` then `pnpm typecheck` (it runs `bun check .`; do not call `bun check .` directly; there is no separate lint script). Use plain `pnpm`, not corepack-pinned. Expected: PASS.

- [ ] **Step 6: Confirm the isolation invariants.** Grep the new explainer path for `Agent.Step`, `scanContentGuard`, `OnMessage`, `OnDelta`, and permission-ask emission: none may be present. Confirm no migration, config, or schema file changed. Confirm `git status` shows no unrelated staged files, and commit path-limited only.
