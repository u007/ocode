# Web/Desktop UI Parity — Current Status

Last updated: 2026-08-22. Desktop (`cmd/ocode-desktop`, Wails) embeds the same
`web/` React app, so "web" and "desktop" are the same UI surface — gaps
below apply to both.

## Achieved (parity complete)

### 1. Cron / scheduling ✅

- Backend: `GET/POST /api/cron/*`, outbox, targets (`internal/server/scheduler.go`).
- Web: `CronPanel`, `CronJobDialog`, `CronOutboxPanel`, `CronTargetsPanel` (`web/src/components/Cron/`).
- TUI: `/cron` command (`internal/tui/command_cron.go`).
- Docs: `docs/scheduled-jobs.md`.

### 2. Per-session Changes tab ✅

- Backend: `GET /api/changes`, `GET /api/changes/diff`, `POST /api/changes/undo-file`, `POST /api/changes/undo-block` (`internal/server/handler_changes.go`).
- Web: `ChangesPanel`, `ChangesFileList`, `ChangesDiffView` (`web/src/components/Changes/`).
- TUI: `internal/tui/changes_model.go`, `internal/tui/changes.go`.
- Design spec: `docs/superpowers/specs/2026-07-22-changes-tab-design.md`.

### 3. Image generation config ✅

- Backend: `GET/PUT /api/config/ocode/imagegen` (`internal/server/handler_config.go`).
- Web: `ImageGenForm` in Settings (`web/src/components/Settings/ImageGenForm.tsx`).
- TUI: `/image` command (`internal/tui/imagegen_cmd.go`).

### 4. GitHub integration ✅

- Backend: `GET /api/github/pr/{owner}/{repo}/{number}`, `GET /api/github/issues/{owner}/{repo}` (`internal/server/handler.go`).
- Web: `/github` slash command falls through to LLM (handled via `commands.ts`).
- TUI: `/github` command (`internal/tui/github_tui.go`).

### 5. Secret redaction (`/mask`) ✅

- Backend: `GET/PUT /api/config/mask/*` (`internal/server/handler_config.go`).
- Web: `SecurityForm` in Settings (`web/src/components/Settings/SecurityForm.tsx`).
- TUI: `/mask` command (`internal/tui/redaction.go`).

### 6. In-chat search (`/search`) ✅

- Web: `ChatSearchBar.tsx` with `Ctrl/Cmd+F` already implements in-chat find.
- TUI: in-chat find bar via `/search` (`internal/tui/chat_search.go`).

### 7. Other achieved items

- **Terminal tabs:** WebSocket + xterm.js (`web/src/components/Terminal/`); shells survive reload via detach/reattach (`docs/architecture/terminal-detach-reattach.md`).
- **Settings:** 20-section overlay (`web/src/components/Settings/`).
- **Git panel:** `GitPanel` (`web/src/components/Git/`).
- **Logs panel:** `LogPanel` + `LogsForm` (`web/src/components/Logs/`).
- **Agents panel:** `AgentsPanel` (`web/src/components/Agents/`).
- **Assets/Uploads:** `AssetsPanel` (`web/src/components/Assets/`).
- **Multi-project:** `ProjectSidebar` (`web/src/components/Layout/`).
- **Profiles:** `ProfilesManager`, `ProfileSwitcher`.
- **Theme sync:** `ThemeForm` + CSS variable mapping.
- **Plugin settings:** `OcodePluginsForm` in Settings.
- **Discovery settings:** `DiscoveryForm` in Settings.
- **OCR settings:** `OcrForm` in Settings.
- **Local model config:** `local-models` section in Settings.

## Remaining gaps (TUI has it, web does not)

### 8. Memory management (`/mem`) — web gap

- **TUI:** Full implementation — `internal/tui/memory.go` with `/mem [on|off|status|update [user|project|global] [focus]]`.
- **Web:** Settings-only toggle (`MemoryEnabled` in `FeaturesForm`). No panel for browsing or editing memory files.
- **API:** No dedicated `/api/memory` endpoint. Toggle only via `GET/PUT /api/config/ocode/features`.
- **Gap:** No web UI for inspecting or editing user/project/global memory files.

### 9. Learn / skill authoring (`/learn`) — TUI only

- **TUI:** `internal/tui/learn.go` + `internal/skill/learn.go` — list project-root skills, guide skill creation/update.
- **Web:** Skill listing via `GET /api/skills` (surfaced in slash autocomplete). No creation/update UI.
- **API:** No HTTP endpoint for skill authoring.

### 10. Goal orchestration (`/goal`) — TUI only

- **TUI:** `internal/tui/goal.go` — run multi-agent orchestration pipeline on a coding goal.
- **Web:** No equivalent.
- **API:** `POST /api/init` for project analysis, but no `/goal` equivalent.

### 11. Doc sync (`/doc-sync`) — TUI only

- **TUI:** `internal/tui/doc_sync.go` — update AGENTS.md/rules/skills to reflect current changes.
- **Web:** No equivalent.

### 12. Knowledge bundle (`/docs`) — TUI only

- **TUI:** `internal/tui/docs_knowledge.go` — init/update/cleanup/status for OKF knowledge bundle.
- **Web:** No equivalent.

### 13. Kaizen skill digest — no web needed

- TUI: automatic prompt-injection of per-model Kaizen tuning directives (`internal/tui/model.go`).
- Not an interactive feature — no dedicated web UI is the right shape.

### 14. Claude export (`/export-claude`) — TUI only

- **TUI:** `internal/tui/export.go` — export in Claude Code JSONL format.
- **Web:** JSON export exists (`GET /api/sessions/{id}/export`). No Claude format endpoint.

## Not started (neither TUI nor web)

None — all TUI features have at least partial web coverage or are non-interactive (Kaizen).
