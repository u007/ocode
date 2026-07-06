# Part 05 — `/docs` subcommands + documentation + end-to-end verification

Spec: `docs/superpowers/specs/2026-07-06-okf-context-agent-design.md` (sections: Command surface, Coexistence). Read it before starting. Depends on Parts 01–04.

Global constraints (self-contained copy): `/docs init` is non-destructive (annotate + index + report; never deletes; idempotent — re-run re-audits without clobbering existing frontmatter); `/docs cleanup` is the ONLY deletion path and confirms per file; init/cleanup writes go through `knowledge.WithBundleLock`; no new config flag — master switch stays `Config.Ocode.DocPromptEnabled`; cleanup listing sorted; caught errors logged; `go build ./...` + full `go test ./internal/...`; commit per task with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

Verified code anchors: `/docs` spec entry `internal/tui/commands.go:144` (aliases `/doc-mode`), thin wrapper `runDocsCmd` `commands.go:1537`, handler `model.handleDocsCmd` `internal/tui/model.go:6980` (parses on|off|status), persistence `config.SaveDocPromptEnabled` `internal/config/ocodeconfig.go:1453`. Pattern for dispatching a prompt into the current session: `sendCustomCommandPrompt` as used by `/doc-sync` (`internal/tui/doc_sync.go:124`). `/mem update` handler (`runMemCmd`) is the analog for `update`.

---

## Task 10: `/docs init | update | cleanup` + extended `status`

**Files:**
- Modify: `internal/tui/commands.go` — `/docs` usage/help text
- Modify: `internal/tui/model.go` — extend `handleDocsCmd` (:6980) with the three subcommands and richer `status`
- Create: `internal/tui/docs_knowledge.go` — the subcommand implementations (keep `model.go` growth minimal; follow the `doc_sync.go` file pattern)
- Test: `internal/tui/docs_knowledge_test.go` (pure parts: prompt builders, cleanup listing/confirmation state), `internal/knowledge` gains any init helper tests

**Interfaces consumed:** `knowledge.DetectBundle`, `NewStore`, `GenerateIndex`, `AppendLog`, `WithBundleLock`, `Doc`/`ParseDoc` (Part 01); `context` subagent dispatch (Part 03); `Agent.QueueDocMaintenance(DocMaintenanceRequest{Forced: true, Focus: ...})` (Part 04).

**Behavior:**
- `init`: if marker already present → re-run audit (idempotent path). Otherwise dispatch the `context` subagent into the current session (via the `sendCustomCommandPrompt` pattern, like `/doc-sync`) with an init instruction: scan `docs/` (create the directory if absent), add OKF frontmatter to classifiable existing files preserving all content and any existing frontmatter keys, create root `index.md` with `okf_version: "0.1"` and `log.md`, and finish with a staleness report (docs contradicting code, duplicates, orphans) marking candidates via `doc_deprecate` — never deleting. Doc tools are available to the context agent per Part 03; bootstrap ordering note: the doc tools require the marker via `DetectBundle`, so `init` first writes the marker `index.md` + empty `log.md` itself under `WithBundleLock` (a small `knowledge.InitBundle(workDir) error` helper — add to Part 01's package with a test: creates marker index + log, errors if marker already present), then dispatches the agent to annotate and fill.
- `update [focus]`: require active knowledge system (else print hint to run init); call `QueueDocMaintenance` with `Forced=true`, optional focus, and the current transcript tail; print confirmation that a maintenance pass was queued.
- `cleanup`: list deprecated docs (path, reason, deprecated date), sorted by path; per-file confirmation through the TUI's existing confirmation flow (grep for the permission/confirm prompt pattern used by destructive actions); on confirm: delete file, `AppendLog(Deletion, ...)`, `GenerateIndex`, all under `WithBundleLock`; on decline: skip. No-op message when nothing is deprecated.
- `status`: existing output plus: bundle present/absent (marker check), conforming/unclassified/deprecated doc counts, last `log.md` entry date, whether the knowledge system is active.

**Steps:**
- [ ] Write failing tests: `knowledge.InitBundle` creates marker index + log and refuses double-init; init prompt builder contains the non-destructive + deprecate-don't-delete instructions; cleanup list sorted and empty-case message; status line assembly for active/inactive/uninitialized states.
- [ ] Run tests → fail.
- [ ] Implement `InitBundle` in `internal/knowledge`, then the TUI subcommands.
- [ ] `go test ./internal/... -v -run 'Init|DocsCmd|Cleanup'` → PASS; `go build ./...`.
- [ ] Manual smoke: in a scratch repo — `/docs on`, `/docs status` (hints init), `/docs init`, verify `docs/index.md` marker + `log.md`, `/docs status` (active, counts), ask a knowledge question (exercises `knowledge_lookup`), `/docs update focus`, `/docs cleanup` with one deprecated fixture doc.
- [ ] Commit: `feat(tui): /docs init, update, cleanup subcommands and knowledge status`.

---

## Task 11: Documentation + end-to-end verification

**Files:**
- Modify: `AGENTS.md` — document the knowledge system: activation (flag + marker), the `context` subagent vs `explore` division, `knowledge_lookup`, `task_cancel`, the sole-automated-writer invariant, `/docs` subcommands
- Modify: `README.md` — one short feature bullet/section for `/docs` knowledge bundle (match existing feature-listing style)
- Modify: `TODO.md` — only if anything was deferred in Parts 01–05 (project rule: incomplete work must be recorded and reported)

**Steps:**
- [ ] Re-read the spec top to bottom; verify each requirement landed (checklist against spec sections: bundle rules, command surface, context agent, tools, read path, write-back, coexistence, error handling). Fix or record gaps in `TODO.md`.
- [ ] Update `AGENTS.md` and `README.md` as above.
- [ ] Full sweep: `go build ./...` and `go test ./internal/...` → all PASS.
- [ ] Commit: `docs: document OKF knowledge bundle and context agent`.
