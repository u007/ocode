# Part 2 — TUI Migrations & UX Polish (Tasks 7–14)

Self-contained part. Spec: `docs/superpowers/specs/2026-06-11-ui-overhaul-design.md`. Requires the component layer from Part 1 (Scrollbar, Button, overlay compositor `compositeOverlay`/`dimLines`, Dialog, ListBox, ModalStack) to be merged. All work in `internal/tui/`. High-level plan: files and behaviors, no code.

Rules for every task here:
- One surface per commit. After each task, the FULL suite passes: `go test ./internal/tui/...` (includes the 5k-line property suite in `model_test.go`).
- Manual smoke check after each migration: `go build ./... && ./bin/ocode` (or `go run .`), open the migrated surface, verify render + keys + mouse. The TUI runs alt-screen — any stray stdout/stderr write corrupts the frame (CLAUDE.md); never add prints, use `agent.emitDebug`/`log`.
- Visual parity is the default; the only intended visual change per migration is centered-overlay placement + dimmed backdrop.

### Task 7: Migrate permission dialog → Dialog + ModalStack, centered overlay, scroll-through

The headline task. **Files:**
- Modify: `internal/tui/model.go` — `renderPermissionDialog` (~line 8762), `permBtnStyle`/hover (~8621), `permViewport`, `permHoverChoice`, `showPermDialog` routing in `Update`, `shouldForwardToTranscriptViewport` (~line 4005), mouse wheel guards, `modalOpen()` (~line 10434)
- Test: extend `internal/tui/model_test.go` (or a new `permission_overlay_test.go`)

- [ ] **Step 1:** Write failing tests for the new routing, driving the model with synthetic `tea.KeyMsg`/`tea.MouseMsg` while a permission request is showing:
  - wheel over transcript area scrolls transcript; wheel over sidebar scrolls sidebar; wheel over dialog bounds scrolls dialog body
  - PgUp/PgDn and ctrl+u/ctrl+d scroll the transcript
  - up/down scroll the dialog body; left/right move button focus; y/n/a/esc/enter resolve the permission (assert the decision callback fires with the right verdict)
  - dialog renders centered over a dimmed backdrop (assert dialog content present mid-screen and backdrop lines still present around it)
- [ ] **Step 2:** Run new tests — expect FAIL.
- [ ] **Step 3:** Build the permission dialog as a `Modal` wrapping a `Dialog` (body = existing permission body content, buttons = existing choices incl. always/deny variants). Push/pop it on a `ModalStack` field on the model where `showPermDialog` is set/cleared today; keep `showPermDialog` as a derived accessor during migration so untouched call sites keep working.
- [ ] **Step 4:** In `View()`, render the permission overlay via `compositeOverlay` over the dimmed frame instead of inline bottom-chrome placement. Wire the dim cache invalidation to transcript content/scroll version.
- [ ] **Step 5:** Relax the input guards: where `modalOpen()` currently blocks transcript/sidebar scroll, let unconsumed mouse/scroll messages from `ModalStack` fall through to the existing pane handlers.
- [ ] **Step 6:** Run new tests — expect PASS. Run full `go test ./internal/tui/...` — expect PASS.
- [ ] **Step 7:** Manual smoke: trigger a permission request, verify scroll-through (wheel each pane, PgUp/PgDn), hover on buttons, y/n/a/esc, resize terminal while open.
- [ ] **Step 8:** Commit: `feat(tui): permission dialog as centered overlay with scroll-through`.

### Task 8: Migrate picker → ListBox + Dialog + ModalStack

**Files:**
- Modify: `internal/tui/picker.go` (`renderPicker` ~line 636, delete-confirm dialog), `internal/tui/model.go` (`showPicker` routing)
- Test: extend existing picker tests / `internal/tui/picker_test.go`

- [ ] **Step 1:** Write failing tests: picker renders as centered overlay (dimmed backdrop present); filtering, selection, wheel, scrollbar drag behave as before (port assertions from existing picker tests); session-delete confirm renders as a nested modal on the stack (esc pops only the confirm, not the picker).
- [ ] **Step 2:** Run — expect FAIL.
- [ ] **Step 3:** Rebuild picker rendering on `ListBox`; delete-confirm becomes a small `Dialog` pushed on the `ModalStack` above the picker.
- [ ] **Step 4:** Run picker tests + full suite — expect PASS.
- [ ] **Step 5:** Manual smoke: model picker, session picker (incl. delete confirm), theme picker entry point.
- [ ] **Step 6:** Commit: `refactor(tui): picker on ListBox/ModalStack with overlay rendering`.

### Task 9: Migrate slash popup → ListBox

**Files:**
- Modify: `internal/tui/slash_popup.go` (~line 501), `internal/tui/model.go` (`showPalette` routing)
- Test: extend slash popup tests

- [ ] **Step 1:** Failing tests: filtering, navigation, Enter executes, Esc closes, hover row follows mouse. Note: slash popup stays anchored above the input (it is a completion popup, not a centered dialog) — overlay compositing applies but positioned above the input, not centered; no backdrop dim for this one (it must not obscure typing context). Record this exception in the consistency pass (Task 14).
- [ ] **Step 2:** Run — expect FAIL.
- [ ] **Step 3:** Rebuild on `ListBox`, keep anchored placement.
- [ ] **Step 4:** Full suite — expect PASS. Manual smoke: type `/`, filter, arrow, click, Enter.
- [ ] **Step 5:** Commit: `refactor(tui): slash popup on ListBox`.

### Task 10: Migrate question prompt → Dialog

**Files:**
- Modify: `internal/tui/question_prompt.go` (~415 lines), `internal/tui/model.go` (`showQuestionDialog` routing)
- Test: extend question prompt tests

- [ ] **Step 1:** Failing tests: centered overlay render, option buttons via `Button` with hover + click, keyboard selection, Esc cancels, multi-question flows unchanged.
- [ ] **Step 2:** Run — FAIL. **Step 3:** Rebuild on `Dialog`/`Button`, push on `ModalStack`. **Step 4:** Full suite PASS; manual smoke via a flow that triggers AskUserQuestion. **Step 5:** Commit: `refactor(tui): question prompt on Dialog component`.

### Task 11: Migrate connect modal → Dialog + ListBox

**Files:**
- Modify: `internal/tui/connect.go` (`renderConnect` ~line 255), `internal/tui/model.go` (`showConnect` routing)
- Test: extend connect tests

- [ ] **Step 1:** Failing tests: each stage (provider list → method → key/code inputs → oauth wait) renders inside one `Dialog`; provider list uses `ListBox`; tab cycles text inputs; Esc backs out one stage; centered overlay.
- [ ] **Step 2:** Run — FAIL. **Step 3:** Rebuild: `Dialog` body swaps per stage; keep the existing `textinput.Model` fields (no input wrapper component — YAGNI per spec). **Step 4:** Full suite PASS; manual smoke: `/connect` flow for one provider with a fake key. **Step 5:** Commit: `refactor(tui): connect modal on Dialog/ListBox`.

### Task 12: Migrate theme picker + retry dialog

**Files:**
- Modify: `internal/tui/model.go` (theme picker overlay ~line 10686, `showRetryDialog` routing)
- Test: extend existing tests

- [ ] **Step 1:** Failing tests: theme picker renders via the shared overlay path (live theme preview while highlighting must keep working); retry dialog renders as a small centered `Dialog` with `Button`s.
- [ ] **Step 2:** Run — FAIL. **Step 3:** Migrate both onto `ModalStack`. After this task, `modalOpen()` should be derived entirely from the stack (plus detail-view state, which stays separate); delete the now-dead per-modal render branches. **Step 4:** Full suite PASS; manual smoke both. **Step 5:** Commit: `refactor(tui): theme picker and retry dialog on ModalStack; modal booleans removed`.

### Task 13: Hover everywhere

**Files:**
- Modify: `internal/tui/model.go` (`handleMouseMotion` ~line 4698), `internal/tui/picker.go`, `internal/tui/slash_popup.go`, tabs rendering in model.go
- Test: extend mouse tests

- [ ] **Step 1:** Failing tests: synthetic `MouseNone` motion over a picker row / slash row / tab sets the hovered visual state and returns a redraw only when the hovered target changes (assert no-redraw on motion within the same target — AllMotion cheapness, CLAUDE.md).
- [ ] **Step 2:** Run — FAIL.
- [ ] **Step 3:** Implement: during render, each interactive surface records its hit-test map (rects → target id) into cached per-frame geometry (pattern already used by `pathLinkProbeCache` / sidebar hover); motion handler reads caches only.
- [ ] **Step 4:** Full suite PASS. Manual smoke: move mouse (no button) across tabs, picker rows, slash rows, buttons, sidebar files — hover follows, no flicker, no lag while streaming.
- [ ] **Step 5:** Commit: `feat(tui): consistent hover states across all interactive surfaces`.

### Task 14: Consistency pass

**Files:**
- Modify: `internal/tui/theme.go`, call sites flagged below
- Test: full suite only (visual task)

- [ ] **Step 1:** Audit and unify, in one commit: single border style for all overlays; uniform padding inside Dialog/ListBox; uniform hint-bar format (key labels styled the same; same ordering: navigation → action → dismiss) across permission/picker/connect/question/theme/retry; one scrollbar glyph set everywhere (delete the Task-1 compatibility wrappers `renderScrollbar`/`renderListScrollbar` and call `Scrollbar` directly at remaining sites: `files_model.go`, `git_model.go`, `detail_view.go`); document the slash-popup anchored-no-dim exception in a comment.
- [ ] **Step 2:** Full suite PASS; `go vet ./...` clean.
- [ ] **Step 3:** Manual smoke: open every modal in sequence in one session; verify identical chrome.
- [ ] **Step 4:** Commit: `style(tui): unify borders, padding, hint bars, scrollbars across modals`.
