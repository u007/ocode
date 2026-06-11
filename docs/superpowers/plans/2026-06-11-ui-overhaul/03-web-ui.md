# Part 3 — Web UI (Tasks 15–19)

Self-contained part. Spec: `docs/superpowers/specs/2026-06-11-ui-overhaul-design.md`. Server work in `internal/server/` (Go), web work in `web/src/` (React 18 + Vite + Tailwind + Radix/shadcn). Independent of Part 2; Task 18 reuses `ThemeColors` from `internal/tui/theme.go`.

Rules:
- Go: TDD with httptest against the server handlers; `go test ./internal/server/...` green per task.
- Web: `bun run typecheck` (runs tsgo — never plain tsc, per global rules) green per task; build via the project's existing web build script.
- All new endpoints are read-only GETs behind the existing `authMiddleware` (see route table in `internal/server/server.go` ~lines 99–160).
- Lists rendered in web panels must be sorted and paginated (global listing rule) unless a code comment documents why not.

### Task 15: `GET /api/git/diff` endpoint

**Files:**
- Modify: `internal/server/server.go` (route registration, near the existing `GET /api/git/status` at ~line 99), plus the handler file containing `handleGitStatus`
- Test: extend `internal/server/server_test.go` or sibling handler test file

- [ ] **Step 1:** Failing httptest cases: returns unified diff JSON (per-file entries: path, status, hunks/patch text) for the working tree; supports `?path=` filter for a single file; 401 without auth; clean tree → empty list, 200.
- [ ] **Step 2:** Run `go test ./internal/server/ -run TestGitDiff -v` — FAIL.
- [ ] **Step 3:** Implement handler. Reuse however `handleGitStatus` and the TUI git view (`internal/tui/git_model.go`) obtain git data — call the same underlying git helper; if both shell out separately today, route through the shared spawn path (global rule: no new raw `exec.Command` sites if a supervisor/helper exists — check how existing handlers spawn git first).
- [ ] **Step 4:** Tests PASS. **Step 5:** Commit: `feat(server): add read-only GET /api/git/diff endpoint`.

### Task 16: Git panel — real diff rendering

**Files:**
- Modify: `web/src/components/Git/GitPanel.tsx` (currently a 49-line stub), `web/src/api/client.ts` (add `getGitDiff`), `web/src/api/types.ts`
- Test: typecheck + manual

- [ ] **Step 1:** Add the typed API client method for `/api/git/diff`.
- [ ] **Step 2:** Build the panel: file list (sorted by path; paginate or virtualize if large) with status badges; selecting a file shows its diff with added/removed line coloring via Tailwind tokens already in the design system; loading/error/empty states (errors surfaced visibly and logged with `console.error` — no silent catch, per global rules).
- [ ] **Step 3:** Use existing shadcn primitives for structure (no hand-rolled modal/list chrome); match ChatPanel spacing/typography.
- [ ] **Step 4:** `bun run typecheck` PASS; manual: run `ocode serve`, dirty a file, verify diff renders and updates on refresh.
- [ ] **Step 5:** Commit: `feat(web): Git panel with real diff rendering`.

### Task 17: Logs panel — wire to existing endpoints

**Files:**
- Modify: `web/src/components/Logs/LogPanel.tsx` (174 lines, partially built), `web/src/api/client.ts`
- Test: typecheck + manual

- [ ] **Step 1:** Audit what LogPanel already does vs `GET /api/logs` + `GET /api/logs/stream` (SSE) — wire fetch-then-stream: initial page from `/api/logs` (sorted newest-last, paginated), then append from the EventSource, following the `streamMessages` EventSource pattern already in `client.ts` (~line 108).
- [ ] **Step 2:** Auto-scroll pinned to bottom unless the user has scrolled up; level-based row coloring; clear button calling `DELETE /api/logs` with confirm.
- [ ] **Step 3:** Visible + logged error states for fetch/stream failures; reconnect on SSE drop (log each attempt).
- [ ] **Step 4:** `bun run typecheck` PASS; manual: generate activity, watch live append, test clear + reconnect (restart server while panel open).
- [ ] **Step 5:** Commit: `feat(web): Logs panel wired to logs API with live streaming`.

### Task 18: Theme sync endpoint + web mapping

**Files:**
- Modify: `internal/server/server.go` + handler file (new `GET /api/theme`), `web/src/hooks/useTheme.ts`, `web/src/index.css`/Tailwind variable layer
- Test: httptest for endpoint; typecheck for web

- [ ] **Step 1:** Failing httptest: `GET /api/theme` returns the configured theme's color values (resolve theme name from config → `ThemeColors` lookup in `internal/tui/theme_generated.go`); works with no TUI running (server may be headless); unknown/unset theme returns the default theme rather than erroring — document this as deliberate (it's config resolution, not a silent fallback). If importing `internal/tui` from `internal/server` creates an import cycle or drags in Bubble Tea, move `ThemeColors` + the generated theme table to a small new package (e.g. `internal/theme`) with `internal/tui` re-exporting; otherwise import directly. Decide by checking the import graph first.
- [ ] **Step 2:** Run — FAIL. **Step 3:** Implement endpoint.
- [ ] **Step 4:** Web: on load, `useTheme` fetches `/api/theme` and writes the colors onto the existing CSS custom properties (`--background`, `--foreground`, `--primary`, `--accent`, `--border`, `--destructive` mapped from ThemeColors' Background/Text/Accent/Border/Error etc.); fetch failure keeps the built-in defaults and logs a warning.
- [ ] **Step 5:** Go tests + typecheck PASS; manual: switch theme in config, reload web, colors follow.
- [ ] **Step 6:** Commit: `feat: theme sync endpoint; web UI follows TUI theme`.

### Task 19: Web consistency pass

**Files:**
- Modify: `web/src/components/Chat/PermissionDialog.tsx`, `web/src/components/Chat/SlashCommandMenu.tsx`, `web/src/components/Layout/ModelDialog.tsx`, `web/src/components/common/CommandPalette.tsx`, others flagged by audit
- Test: typecheck + manual

- [ ] **Step 1:** Audit every dialog/menu/list for hand-rolled chrome; rebuild any non-Radix modal on the existing `components/ui/dialog` and `components/ui/command` primitives.
- [ ] **Step 2:** Unify hover/focus: every interactive element gets the same hover token + visible focus ring (keyboard a11y); spacing/typography audit against ChatPanel as the reference surface.
- [ ] **Step 3:** `bun run typecheck` PASS; manual click-through of all dialogs and menus, keyboard-only pass (Tab/Enter/Esc).
- [ ] **Step 4:** Commit: `style(web): unify dialogs on shadcn primitives, consistent hover/focus`.
