# Changelog

## [Unreleased]

### Added
- **Fast Viewport Component** — New `internal/tui/fastviewport` package with O(1) `SetContentLines` replaces chat transcript's bubbles viewport, reducing render time from ~30ms→0.73ms at 1000 message pairs (~41× faster).
- **Permission Read File Enhancement** — `read_file` tool now supports directory listing when pointed at directory paths, providing better context for list/glob/grep/repo_overview operations.
- **Transcript Render Optimization** — Coalesced streaming render cadence from 50ms→90ms while auto-scrolling, halving in-flight CPU with no perceptible animation loss (~11fps vs 20fps on thinking streams).
- **GLM Model Compatibility** — New `isGLMModel()` helper in `internal/agent/client.go` detects Zhipu GLM models across providers (OpenRouter, Z.AI, etc.). `convertToOpenAIMessages` now omits empty-string `content` when an assistant message carries `tool_calls` (GLM error 1214), skips `reasoning_content` in request fields (GLM emits but rejects it), and appends a synthetic `{"role":"user","content":"continue"}` when the message sequence ends on an assistant turn.

### Changed
- **Permission Read File Tool** — Enhanced `read_file` tool description to clarify it can read files or list directories, providing better context for permission decisions.
- **TODO.md Documentation** — Updated with detailed performance analysis and optimization results for transcript rendering, including root cause analysis and implementation details.
- **Makefile Build Targets** — `install` target now builds binary to `bin/` before `go install`; `clean` target now removes the `bin/` directory.
- **Config Default Model & Prefixes** — Updated `last_model` to `opencode-go/deepseek-v4-flash`; reordered `auto_allow_prefixes` alphabetically; added `"feat:"` to bash prefix allow list.

### Fixed
- **Transcript Performance Bottleneck** — Replaced O(N) viewport line scanning with O(1) pointer assignment, eliminating 27-35ms per render from redundant `\r\n` scans and `maxLineWidth` calculations that were never used due to `SoftWrap=false`.
- **Directory Permission Handling** — Permission system now properly handles directory targets for read_file tool, returning usable directory listings instead of opaque errors for list/glob/grep operations.
- **Instant Slash Commands** — Synchronous local UI/config slash commands (`/model`, `/help`, `/sidebar`, `/theme`, `/lsp`, `/mcp`, etc.) now bypass the command queue and execute immediately even while the agent is streaming.
- **Git Permission Subcommand Granularity** — Bash prefix permission rules for `git` are now offered at two-word subcommand granularity (e.g. "git push") so "always allow" persists without blanket-allowing every git subcommand. Harmful git operations (force-push, revert, etc.) always require explicit approval.
- **Shell Redirect fd-dup Parsing** — Shell parser now correctly handles fd-duplication redirects (`2>&1`, `>&2`, `&>`, `&>>`) as single tokens, fixing bogus `bash prefix "1"` permission prompts.
- **LSP Server Warm-Up** — New `Manager.WarmUp()` eagerly starts language servers at app init based on extensions found in the project root (scans depth-limited, skipping `vendor`/`node_modules`/`.git`). Emits a `Phase:"starting"` event before the blocking initialize handshake begins, so the sidebar can show a spinner immediately.
- **LSP Lifecycle Phase Events** — `ServerStartedEvent` now carries a `Phase` field (`"starting"` | `"ready"`), enabling the TUI to distinguish pre-handshake and post-handshake state.
- **IDE Sidebar Toggle** — Clicking the IDE status line in the sidebar now toggles between `IDEModeClaude` and `IDEModeOff`. Includes `ideToggleTopIdx`/`ideToggleRows` render-data tracking and a new `sidebarIDEToggleForClick` hit-test method.
- **Slash Command Queuing** — Slash commands entered while the agent is streaming or compacting are now queued in `queuedCommands []string` and executed one-at-a-time after the current work ends (behind `queuedInputs`), instead of running immediately. Only `/exit`, `/quit`, `/q` bypass the queue. The queue is drained in the `agentStreamDoneMsg` and `compactFinishedMsg` handlers after queued inputs are processed. The status bar queue counter now includes both queued inputs and queued commands.
- **Manual /compact Re-Compaction** — When manual `/compact` finds no new content after the previous summary, it now re-compacts the summary itself instead of skipping, ensuring the command always produces a result.
- **Few-Turn Session Compaction** — Sessions with few user turns that exhaust the `KeepRecentTurns` budget now retry with `KeepRecentTurns=1`, leaving no "nothing to compact" gap.
- **Resumed Session Compaction Fix** — `findPrefixEnd` now stops before compaction summary markers, preventing resumed sessions with multiple base prompts from absorbing the summary into the prefix.
- **LSP Server Status API** — New `ServerStartedEvent`, `ActiveServers()`, and `SetEventChan()` in `internal/lsp/manager.go` for TUI sidebar LSP state wiring. New `lsp.ServerForExt` lookup.
- **Debug Kind LSP** — New `debuglog.KindLSP` entry kind for language server diagnostic and lifecycle events.
- **TUI LSP Event Listener** — TUI model now receives `lspServerStartedMsg` and `lspIndexingDoneMsg`, tracks per-server indexing state, and logs diagnostic summaries (errors/warnings per file) to the debug log. Sidebar render cache includes `lspStateSeq` to refresh on LSP state changes.
- **LSP Manager Tests** — New `internal/lsp/manager_test.go` with test coverage.
- **Advisor Config Persistence** — New `config.SaveAdvisorEnabled()` function to persist the advisor toggle to config.
- **Theme Picker Live Preview** — Theme picker now previews the selected theme as the user types in the filter field.
- **LSP Status Sidebar Plan & Spec** — New `docs/superpowers/plans/2026-06-07-lsp-status-sidebar.md` and `docs/superpowers/specs/2026-06-07-lsp-status-sidebar-design.md`.
- **README Slash Commands** — Comprehensive slash command reference added to `README.md` with palette rendering, command table, headless examples, and `/recap` status.
- **Slash Command Usage Skill** — Section 9 of `skills/ocode-usage/SKILL.md` updated with the full slash command documentation.

### Changed
- **Permission Approval Path** — "Always allow" permission choices now execute via `executeApprovedTool` (no re-check) instead of `executeToolWithRules`, preventing the permission dialog from looping when the persisted rule doesn't fully cover the request.
- **Slash Command Queuing Docs** — Updated `AGENTS.md` to document that synchronous local UI/config commands may bypass the queue.
- **LSP Sidebar Warming Server Display** — `renderLSPSection` now includes servers whose handshake is in progress (present in `lspServerStartTimes` but not yet in `ActiveServers()`), so the sidebar shows warming servers alongside ready ones.
- **LSP Debug Message Wording** — Event log messages for handshake completion now say "ready" instead of "started", matching the new two-phase lifecycle (`starting` → `ready`).
- **Makefile Parallel Cross-Compilation** — `build-darwin`, `build-linux`, `build-all`, and `release` targets now run per-OS/per-arch builds concurrently with `&` + `wait`, cutting total build time per target. `install` now depends on `web-build` (not `build`) and includes `$(LDFLAGS)` for versioned binaries.
- **Files Tab Keyboard Shortcuts** — Migrated from single-letter keys to Ctrl+letter combos across the file tree and preview panels to avoid terminal input conflicts. `j/k` → `up/down`, `e` → `ctrl+e`, `E` → `ctrl+v`, `n` → `ctrl+n`, `N` → `ctrl+b`, `r` → `ctrl+r`, `D` → `ctrl+d`, `i` → `ctrl+l`, `y` → `ctrl+y`, `o` → `ctrl+o`, `R` → `ctrl+t`, `/` → `ctrl+g` (fuzzy), `ctrl+f`/`/f` → `ctrl+f` (content search). In-file search `n`/`p` → `ctrl+n`/`ctrl+p`.
- **Git Tab Keyboard Shortcuts** — Same Ctrl+letter migration across changes, log, stash, and branches sections. `r` → `ctrl+r`, `/` → `ctrl+f`, `s` → `ctrl+s`, `u` → `ctrl+u`, `d` → `ctrl+d`, `c` → `ctrl+\\`, `a` → `ctrl+a`, `i` → `ctrl+l`, `I` → `ctrl+_`, `f` → `ctrl+g`, `p` → `ctrl+p`, `P` → `ctrl+o`, `n` → `ctrl+n`, `x` → `ctrl+x`, `S` → `ctrl+z`, `E` → `ctrl+e`.
- **Root Model Add-to-Context Key** — `a` key binding changed to `ctrl+a` for consistency.
- **Files Tab Preview Layout Safety** — Hint, header, status, and prompt lines now clamped with `.Width(w).MaxHeight(1)` to prevent text wrapping from pushing bottom chrome off-screen.
- **Git Tab Preview Layout Safety** — Same MaxHeight(1) clamping applied to hint lines.
- **Files Tab Content Search Layout Safety** — Query, extension, ignore-toggle, and hint lines clamped with MaxHeight(1).
- **LSP Manager Lock Contention** — `ClientForExt` now releases the mutex before long-running operations (PATH lookup, client initialize), preventing TUI stalls during LSP startup.
- **Editor Process Group Removal** — Removed `Setpgid`/`OwnsProcessGroup` for external editor subprocesses, simplifying lifecycle management in containers and CI.
- **Slash Commands Recorded in Transcript** — Slash commands (e.g. `/sidebar`, `/theme`, `/editor-mode`) are now recorded as transcript messages with `skipLLM=true`, so they appear in session history but are not sent to the LLM or used for title generation.
- **Recap Result Integration** — Recap result is now added to the message list as an assistant message instead of being held as ephemeral `recapText`.
- **Compact Result Scroll** — Manual `/compact` now scrolls the transcript to the compaction banner; auto-compact scrolls to bottom.
- **Compact Banner Scroll** — Compaction result viewport scrolling updated to target the compaction banner specifically.
- **Version** — Bumped from `0.3.3` to `0.3.4`.

### Fixed
- **Commit Message Generation Model Fallback** — Git tab's `Ctrl+G` commit message generator now uses `ResolveSmallModel` fallback instead of hardcoded `openai/gpt-5.4-mini`, fixing "no LLM configured" errors when OpenAI API key is not set. Configurable via `commit_msg_model` in `ocodeconfig.json`.
- **Permission Dialog Loop** — Broad single-word deny rules (e.g. "git" => deny) now correctly win over granular allow rules, and harmful commands always require explicit approval regardless of prefix rules.
- **LSP Manager Lost During Model Init** — The LSP manager and event channels are now transferred from the temporary builder model to the real model, fixing a nil `lspMgr` that prevented the sidebar LSP section from rendering.
- **VS Code IDE Client Keepalive** — `notifications/initialized` now omits empty
  params, matching the Claude Code VS Code extension's expected payload shape and
  preventing the immediate socket churn seen during IDE connect.
- **Command File Parser** — Fixed 50-line cap in `parseCommandFile` that silently truncated command prompt bodies longer than 50 lines after frontmatter (e.g., `/git-commit-push.md` dropped its "Step 4: Stage" and "Step 5: Commit and push" sections). Now reads the full file content.
- **Multi-Session Permission Clobbering** — `SaveOcodePermissions` no longer overwrites the on-disk `auto.permissions.model` (owned exclusively by `SavePermissionModel`). Also preserves the entire disk auto block when the calling session has no auto block.
- **Transcript Auto-Scroll** — Changed to sticky-bottom behavior: only follows when pinned to the bottom; one wheel-up stops auto-scroll and stays put while the LLM continues streaming.
- **Model Picker Favorite Toggle** — Changed favorite toggle keybinding from `f` (which conflicted with type-to-filter) to `ctrl+f`.
- **Git Tab Mouse Click Mapping** — Corrected section panel and file-list click coordinate mapping (border offset, staged/unstaged header row offset); clamped negative content lines in diff click.
- **Files Tab Content Search Cancellation** — Pressing Esc now properly cancels an in-flight content search.
- **Git Tab Diff Gutter & Soft-Wrap** — Diff viewport now soft-wraps long lines (no horizontal truncation) and shows line numbers in a left gutter (`4d │ ` style), so wrapped diffs stay readable.
- **TUI Clickable File Paths** — File-path tokens (e.g. `internal/server/handler.go`, `handler.go:42`) in the chat transcript and the agent-detail drill-in are now clickable. Lazy stat-based detection (only existing files become links). Opens in `$EDITOR`/`$VISUAL` with system-opener fallback. Known limitations: TUI click ignores the `:line` suffix; paths split across a visual-line wrap boundary linkify only the first segment.
- **Web Clickable File Paths** — Same lazy-stat path detection rendered via a custom `rehype` plugin (`rehypeFileLinks`) + `linkifyPlainText` for user messages, producing a `filelink` custom element. Web `POST /api/files/open` opens the resolved file via the system opener or `--goto`-capable GUI editor.
- **OpenAI Responses Empty-Response Retry** — Empty Responses-API replies (no text, no tool calls) are now classified as a retryable error (`ErrNoResponseFromOpenAIResponses`) and retried with the normal backoff loop instead of failing the turn.
- **Anthropic OAuth Token POST** — `AnthropicExchange` and `AnthropicRefresh` now send `application/x-www-form-urlencoded` (per Anthropic's spec) instead of JSON, fixing token acquisition that was previously failing with a 415.
- **Detail Viewport Scroll on Resize** — `refreshTopDetailView` now preserves the scroll position when the viewport is resized: if the user was scrolled to the bottom before the resize, the viewport stays at the bottom after content is updated. Prevents jarring jumps to the top on terminal resize or panel split.

### Added
- **VS Code `/ide` Integration** — Added lock discovery, WebSocket + MCP client,
  selection/open-editor/mention streaming, IDE status chip, `/ide status`,
  auto-attach into selection context, and `ide_mode` config support for Claude
  Code in VS Code.
- **Web UI Layout Restructure** — New tab-based navigation with `TopTabs` (chat/files/git/logs), collapsible `SessionSidebar`, `CoworkSidebar`, and `ModelDialog` components. Session history, model selection, and agent tabs separated into dedicated panels.
- **Makefile `dev` Target** — One-command hot-reload development environment launching Go backend (`:4096`) and Vite frontend (`:5173`) in parallel with port cleanup.
- **Makefile `production` Target** — Build web UI assets then compile and serve the Go binary on `:4096`.
- **shadcn/ui Component Library** — Added shadcn/ui primitive components (`Button`, `Select`, `CommandDialog`, `Command`) as the new UI component foundation.
- **Cowork Panel Toggle** — Status bar button to show/hide the right-side agent cowork sidebar for parallel agent management.
- **Model Dialog Command** — New `/model` slash command opens a model selection dialog.
- **Git Action Logging** — All terminal-state git user actions (push, pull, fetch, commit, stage/unstage, stash, branch create/delete/checkout, hunk apply) are now logged to the DebugLog with a new `GIT` kind, filterable via `5` key in the log tab.
- **Files Tab Content Search Streaming** — Content search results are now streamed incrementally in batches (batch size 10, flush interval 100ms) via a background goroutine, with Esc cancellation support.
- **Binary File Detection in Open** — Both Files tab and Git tab now detect binary files and route them to the system file opener instead of Vim/editor.
- **Double-Click Folder Opens Explorer** — Double-clicking a directory in the Files tab tree opens it in the operating system's file explorer.
- **Claude Settings Deny Rules** — Added built-in deny rules for `git push --force` and `rm -rf` destructive commands.
- **Files Tab Hidden Files Toggle** — Added `Ctrl+H` to toggle showing hidden files/directories in the file tree; hidden entries are visually dimmed when visible.
- **Files Tab Fuzzy Search Popup** — Replaced inline fuzzy filter with a full popup overlay supporting keyboard navigation (`↑`/`↓` or `j`/`k`), live preview of the highlighted result, and result count display.
- **Files Tab Multi-Select Delete** — Space now selects directories too (not just files); `D` with multi-selected items deletes all in depth-sorted order (children before parents).
- **Files Tab Rename Overwrite Confirmation** — Rename requires pressing enter twice when the target path already exists, preventing accidental overwrites.
- **Files Tab Tree Hint Bar** — Added a keybinding hints bar at the top of the file tree panel showing available actions.
- **Server Error Logging** — Added `log.Printf` error logging to serve handlers for agent step errors, surfacing failures in the debug panel.
- **Test Coverage** — New tests for multi-session permission model preservation, double-click folder explorer behavior, binary file opener, empty search path, stale search message filtering, and 50-line cap removal in command parser.
- **`/rc` (Remote Control) TUI Command** — New TUI slash command (aliases: `/remote-control`) starts an embedded web server bound to the current TUI session and opens the browser to `http://localhost:4096/session/<id>`. Web messages stream to the TUI agent via an `RCBridge`; the TUI remains the source of truth. New `tui.RunOptions.WebFS` field threads the embedded `dist/` assets to the TUI command.
- **Advisor Runtime Toggle** — The `advisor` tool is now always registered, but its exposure to the model is gated by a runtime `atomic.Bool` (`Agent.SetAdvisorEnabled` / `AdvisorEnabled`). Default seeded from `cfg.Ocode.Advisor.Enabled`. New `GET/PUT /api/config/advisor-enabled` endpoint lets the web sidebar flip the toggle for the session's lifetime (not persisted to config).
- **Web Agent-Runs API & Preview** — `GET /api/agents/runs` and `GET /api/agents/runs/stream` (SSE) expose the in-memory run tree (id, name, status, model, token usage, full transcript, nested sub-agent runs) for the new `AgentPreview` web component. Used by the cowork panel to show live sub-agent activity.
- **Web Logs API & Panel** — `GET /api/logs`, `GET /api/logs/stream`, `DELETE /api/logs` expose the in-process debug log. New `Logs/LogPanel` web component renders the log with live SSE updates.
- **Web Session Routing** — React Router added; `/session/:id` now resolves to a dedicated `SessionPage` that resumes a session, replays its full message history (via the new `SessionDetail` type), and supports in-place streaming.
- **Web Permissions API & Dialog** — `POST /api/permissions` accepts `{ request_id, approved }` to resolve a `permission_required` SSE event. New `Chat/PermissionDialog.tsx` replaces the old `common/PermissionDialog.tsx` and lives inside the chat surface.
- **Web Slash-Command Autocomplete** — Typing `/` in the chat input opens a `SlashCommandMenu` popup with keyboard navigation (↑/↓, Enter, Esc). Supports `/clear`, `/model`, `/compact`, `/recap`, `/export`, `/share`, `/help`.
- **Web Model Dialog Tabs** — `ModelDialog` now has three tabs (main / small / advisor) and is the single place to switch any of the three models. Each tab is wired to its own `get/set` config endpoint.
- **Web Advisor Status in StatusBar** — Status bar reflects the live advisor on/off state; the toggle persists across `RESET` (the reducer carries `advisorEnabled` through new sessions).
- **Web Markdown Rendering** — Assistant messages now render through `react-markdown` + `remark-gfm` with `highlight.js` syntax highlighting, `@tailwindcss/typography` (`prose`) styles, and shadcn-styled code/blockquote/heading/list overrides.
- **Web File Open** — `POST /api/files/open` opens a file by absolute path (or relative to the server's `os.Getwd()`), opening GUI editors with `--goto <line>` when the path includes a `:line` suffix.
- **TUI Files In-File Search** — New `filesModeInFileSearch` mode searches within the current preview file. Matches are highlighted via `viewport.Highlights`; `n` / `N` jumps through matches. Triggered by `/` while focused on a previewed file.
- **TUI Session Pagination** — `session.ListRefsPaginated(limit, offset)` returns a page of refs plus the total count. The session picker now loads 20 refs at a time with progressive "load more" via the new `loadSessionRefsCmd(seq, limit, offset)`.
- **TUI Session Delete** — `session.Delete(id)` removes a session file and updates the on-disk index. New `renderSessionDeleteConfirmDialog` in the TUI for the confirmation step.
- **`ForceRefreshRegistry`** — New `agent.ForceRefreshRegistry()` synchronously fetches the models.dev registry (bypassing the 5-minute TTL), writes through to the on-disk cache, and returns the new data. Hooked up to the TUI picker via `refreshModelsCacheCmd` + `modelsRefreshedMsg` so users can manually refresh the model list.
- **TUI Branch-Only Refresh** — Lightweight `gitModel.cmdBranchRefresh` (no diff reload) keeps the sidebar's current-branch / ahead-behind display fresh without disturbing the active tab's scroll/selection.
- **Debuglog Package** — `internal/debuglog` extracted from `tui/debuglog.go` so non-TUI consumers (server, agent, etc.) can write to the shared log without importing the TUI. `tui.DebugEntry`, `tui.DebugLog`, and friends remain as backward-compat aliases.
- **CLI Help Flags** — `ocode`, `ocode acp`, `ocode mcp`, `ocode models`, and `ocode run` all accept `-h` / `--help` and print a usage block (previously the top-level help was missing and the subcommands printed usage only on bad input).
- **Makefile `kill-ports` / `close` Targets** — Convenience target to free `:4096` (backend) and `:5173` (Vite) before `make dev`/`make production`.
- **Test Coverage (continued)** — New tests: `pathlink_test.go` (path detection + `:line` suffix), `files_click_offset_test.go` (preview click coordinates), `detail_view_test.go` (agent drill-in rendering), `model_test.go` extensions (slash popup, model picker refresh, session delete dialog), `handler_open_test.go` (server-side open validation), `handler_runs_test.go` (run-tree serialization), `anthropic_test.go` (form-encoded token POST).

### Changed
- **Module Path Rename** — Renamed the Go module from `github.com/jamesmercstudio/ocode` to `github.com/u007/ocode` repo-wide (go.mod + all imports + docs), aligning the import path with the canonical repository.
- **VS Code `/ide` Status Location** — Moved IDE status out of the bottom status bar and into the chat sidebar so the enabled/disabled state and connection state stay visible even in narrow terminals.
- **Session API Response** — `GET /api/sessions/:id` now returns `SessionDetail` (includes full message history) instead of just session metadata, enabling session resume/import in the web UI.
- **Web API Client** — Updated `listAgents` endpoint from `/api/agents` to `/api/config/agents` for consistency with the config-based API layout. Updated `getSession` return type.
- **Makefile `install` Target** — `install` now depends on `build`, ensuring web assets are compiled before installing the binary.
- **shadcn/ui Component Migration** — Replaced raw `<button>` elements with shadcn/ui `<Button>` across ChatInput, FileTree, AgentTabs, SessionList, ErrorBoundary, and StatusBar. Replaced raw `<select>` with shadcn/ui `<Select>` in ModelSelector. Rewrote CommandPalette using shadcn/ui `<CommandDialog>` for consistent styling and accessibility.
- **Tab Shortcut Simplification** — Removed the numbered `1`–`4` tab shortcuts; use `alt+[` / `alt+]` (or `ctrl+shift+[` / `ctrl+shift+]`) to switch tabs. Tab bar labels updated from `1:chat`/`2:files`/`3:git`/`4:log` to plain `chat`/`files`/`git`/`log`.
- **Advisor Tool Simplification** — Removed per-call `providerID`/`modelID` overrides from the advisor tool; model is now preset via the `/advisor` command or config, reducing complexity and preventing accidental model switching.
- **Files Tab Selection Help** — Updated the Files tab help text to call out multi-select flow (`space` select, `shift+↑↓` extend, `D` delete).
- **Files Tab Keybinding** — Status bar keybinding updated from `i edit` to `o open` to reflect the new binary-aware open behavior.
- **Files Tab Status Bar** — Updated status bar to show `space select`, `^h hidden`; removed `^S save`.
- **Files Tab Prompt Input** — Prompt input now auto-focuses when starting create file/directory or rename.
- **Model Picker Score Threshold** — Raised fuzzy matching minimum score to 100,000 to reduce false positives from subsequence matching across 5,000+ provider models.
- **Model Picker Navigation** — Fixed picker navigation to skip filtered-empty items when model picker is actively filtered.
- **Log Tab Auto-Scroll** — Log viewport auto-scrolls to the bottom when switching to the log tab.
- **Transcript Trailing Padding** — Added 20 lines of trailing vertical padding so agent/permission boxes at the bottom of the transcript are not obscured by the viewport.
- **Gitignore** — Added `.claude/settings.local.json` to `.gitignore`.
- **Version** — Bumped from `0.3.0` to `0.3.1`.
- **Version** — Bumped from `0.3.1` to `0.3.2` for the current unreleased set of changes (`/rc`, advisor runtime toggle, web markdown rendering, debuglog package extraction, etc.).
- **Server Chat Locking** — `Handler.HandleChat` / `HandleSendMessage` / `HandleChatStream` now hold the per-agent `agentSession.mu` (not the global `Handler.mu`) while stepping the agent, so concurrent requests to *different* sessions no longer serialize behind each other. The handler-wide lock is released as soon as the session is selected.
- **SSE Chat Model Override** — `GET /api/chat/stream` now accepts an optional `?model=…` query param; falls back to the configured model when absent.
- **Model Picker Refresh** — `refreshModelPickerItems` rebuilds the model-family picker in place after a force-refresh (or any other in-place mutation) without losing the user's filter text or selection. Page size for the session picker reduced 50 → 20 to smooth first-paint of large session histories.
- **TUI Selection Helper** — `insertHighlight` is now a thin wrapper around a new `insertSGRSpan(rendered, raw, rawStart, rawEnd, openSeq, closeSeq)` helper, making arbitrary SGR spans (e.g. the file-link underline) reusable from the same single-pass escape scanner.
- **Anthropic Token HTTP Client** — Token exchange now uses a package-level `anthropicHTTPClient` (30s timeout) instead of constructing a new client per request. Token URL is also a `var` (was `const`) so tests can override it.
- **OAuth Token Payload Encoding** — `anthropicTokenRequest` switched from JSON to `url.Values` (form-encoded) as required by Anthropic's OAuth endpoint.
- **`@path` Help Text** — Updated the `/` help line from "Add file context or attach an image" to "Reference a file (attach an image, or pass the path to the model)" to match the new path-linkifying behavior.

### Removed
- **PermissionDialog** — Removed obsolete `PermissionDialog` component; permissions flow now uses API-driven inline approval in the web UI.
- **Legacy Sidebar Components** — Removed `web/src/components/Sidebar/{AgentTabs,ModelSelector,SessionList}.tsx` as part of the layout restructure. Replaced by the new `Layout/TopTabs`, `Layout/SessionSidebar`, `Layout/CoworkSidebar`, and `Layout/ModelDialog` components.
- **`common/PermissionDialog.tsx`** — Removed in favor of the new `Chat/PermissionDialog.tsx` colocated with the chat surface.

## [0.3.1] — 2026-06-04

### Added
- **CONTRIBUTING.md** — Development setup guide, code conventions, and PR guidelines for contributors.
- **TEAM_ONBOARDING.md** — Comprehensive team onboarding documentation covering architecture, build/test/run commands, and development workflow.
- **Team Onboarding Skill** — New bundled skill (`skills/team-onboarding/`) for generating onboarding docs from codebase analysis.

### Changed
- **README.md** — Added "Why ocode?" section highlighting lightweight design, auto-permissions, and extensibility. Added link to CONTRIBUTING.md in Quick Start. Added Support section with links to issue tracker and OpenCode Go plan.

### Security
- **Exfiltration-Risk Detection for URL-Calling Commands** — `IsHarmfulBashCommand()` now detects data exfiltration risk in `curl`, `wget`, `httpie`, and `netcat` commands. Commands that could leak secrets (file upload via `-d @file`, `-F file=@secret`, `--upload-file`; env var injection via `-H "Auth: $TOKEN"`; subshell expansion `$(cat .env)`; proxy/config redirects) are flagged as **harmful** — they always require human approval and cannot be persisted as "always allow". Benign usage like `curl https://api.example.com/get` is not affected. Covers 4 detection categories across 4 tools with ~50 test cases.

## [Unreleased]

### Added
- **Version** — Bumped from `0.3.2` to `0.3.3`.

## [0.3.2] — 2026-06-07

### Added
- **Skills Management CLI** — New `ocode skills` subcommand for installing, upgrading, listing, and uninstalling bundled skills. Skills are embedded into the binary via `//go:embed` and installed to `~/.config/opencode/skills/`. Subcommands: `list`, `install [name...]`, `upgrade [name...]`, `uninstall <name...>`. Backup-on-overwrite creates a timestamped `.bak.<timestamp>` copy of the existing SKILL.md before replacing it. Symlink safety: refuses to install into symlinked skill directories.
- **Smart Skill Status Detection** — `GetSkillStatus()` distinguishes four states using SHA256 hashes and a `.bundled-hash` sidecar file: `installed` (hash matches bundled), `outdated` (bundled changed, file untouched), `custom-modified` (user edited the file), `missing` (not installed). The `.bundled-hash` file records the bundled version's hash at install time.
- **`/skills` TUI Command** — Enhanced slash command with subcommands: `/skills` (list with status), `/skills install [name...]`, `/skills upgrade [name...]`, `/skills info <name>`, `/skills help`. Status indicators: ✓ installed, ↑ outdated, ✎ custom-modified, ✗ missing.

## [Unreleased] — 2026-05-28

### Added
- **Context-Aware Cancellation** — `chatWithDelta` now derives a `context.Context` from the agent's stop channel, interrupting in-flight HTTP requests when the user presses Escape. New `ChatWithContext()` on `GenericClient` threads context through all provider chat methods (`chatAnthropic`, `chatOpenAI`, `chatCopilot`, `chatOpenAIResponses`). New `ResetCancellation()` and `StopCh()` methods on Agent.
- **Agent Fallback for Unknown Sub-Agents** — `TaskTool` now silently falls back to the built-in `general` agent when an explicitly-named agent is not found, instead of returning an error. A warning is prepended to the result.
- **`/init` Prompt Template** — `/init` now sends a project-analysis prompt to the LLM instead of writing a stub AGENTS.md. Supports an optional focus argument (`/init <topic>`).
- **Embedded Models Snapshot** — `internal/agent/models-snapshot.json` is now populated from `https://models.dev/api.json` and embedded into the binary via `//go:embed`. `loadRegistry` uses the snapshot as a fallback when the network is unreachable or the user has not yet hit the API. The snapshot adds ~492KB to the binary; trim if size becomes a concern.
- **Custom Registry Path** — Set `OPENCODE_MODELS_PATH=/path/to/models.json` to load the model registry from a local file (same JSON shape as `https://models.dev/api.json`). Useful for air-gapped environments and for pinning registry contents in CI. `loadRegistry` consults this env var before the embedded snapshot.
- **CLI `--permission-mode auto|off`** — New top-level flag toggles the LLM auto-permission layer on or off for the session. Persists to `permissions.auto.enabled` in `ocodeconfig.json` so the choice survives across sessions. TUI status bar now surfaces `auto-permission on` alongside YOLO/locked indicators. Hard-blocks remain deterministic; the auto layer only governs Ask fallthrough.
- **CLI `--dangerously-skip-permissions`** — Top-level alias for `-yolo`/`--yolo`. Skips every permission prompt and auto-approves requests that aren't explicitly denied. Works in both the interactive TUI and the `run` subcommand.

### Changed
- **Model Registry Cache TTL** — `modelsCacheTTL` reduced from 24h to 5min. With the background hourly refresh loop removed, the registry now refreshes lazily on each `loadRegistry` call after 5min. Trade-off: more network calls in long-running sessions (one fetch per 5min of activity) for simpler state — no background goroutine, no double refresh path. Custom registries via `OPENCODE_MODELS_PATH` are unaffected.
- **Picker Filter Algorithm** — Model picker filtering now splits the query on whitespace, dashes, and underscores and requires every keyword to fuzzy-match. `"gpt 4o"`, `"gpt-4o"`, and `"gpt_4o"` all match the same models. Previously, filter was a simple case-insensitive substring search.

### Removed
- **Hardcoded Model Fallback** — `ProviderModels(provider)` and `AllProviderModels()` no longer return a hardcoded fallback list when the registry is unavailable. Offline/air-gapped users will see the empty picker state. To work around this, point `OPENCODE_MODELS_PATH` at a local copy of `https://models.dev/api.json` (or rely on the embedded snapshot, which is now populated).

### Changed
- **WaitTool Uses Live Stop Channel** — `WaitTool` now holds a reference to the parent `Agent` and reads `StopCh()` at call time, eliminating stale stop-channel references.
- **TUI Header Layout** — Tab bar and exit button moved into the header row (top of screen) alongside the session title, freeing vertical space. Header width adapts to sidebar state.
- **Agent Step Loop Cancellation Snapshot** — `Step()` captures the stop channel once at invocation so concurrent `ResetCancellation()` calls don't affect in-flight loops.

### Fixed
- **Detail Viewport Scroll Position** — Agent run, process list, and process log detail views now open scrolled to the bottom (`GotoBottom()`), showing the latest output.
- **Constrain View Bottom Preservation** — Fixed `constrainViewPreservingBottom` to correctly truncate when all lines exceed height with bottom-line preservation.

## [Unreleased] — 2026-05-24

### Added

- **Hidden Agents Framework** — Introduced hidden agents (e.g., `title`, `compaction`) that drive runtime helpers but aren't exposed in the UI. Users can override system prompts and model selection via `.opencode/agents/` markdown files.
- **Provider-Specific Prompts** — New `provider_prompts.go` module enables AI-provider-specific system prompts (e.g., Claude vs GPT) to be appended to base prompts during agent initialization.
- **Per-Agent Model Selection** — Agents can now specify a custom model override (e.g., `"gpt-4o"`), with precedence: agent-specific model > small model config > main client.

### Changed

- **Token Estimation Refactor** — Extracted `CurrentContextEstimate()` to separate token counting logic from `shouldCompact()`. Improves accuracy by counting messages appended after the latest Usage-bearing response (tool results, new prompts), not just the cumulative total at that point.
- **Agent Run Detail View Redesign** — Restructured transcript rendering from flat text output to nested card-based view with status indicators, timeline events, sub-agent tracking, and intentional hiding of system prompts for clearer user experience.
- **Scrollbar Metrics Extraction** — Extracted scrollbar calculations into `scrollbarThumbMetrics()` and `scrollbarThumbOffset()` helper functions for composability and testability.

---

## [Unreleased] — 2026-05-23

### Added

- **Question Prompt Tool Support** — New TUI dialog for `AskUserQuestion` tool, rendering multi-choice/text input questions with tab navigation between prompts and cursor/selection tracking per question. Similar UX to permission dialogs.
- **Tool Sentinel Constants** — Extracted hardcoded sentinel strings (`QUESTION_PROMPT:`, `WAITING_FOR_USER_RESPONSE`, `PERMISSION_ASK:`) into constants in `internal/tool/misc.go` for maintainability and consistency across agent, session, and TUI modules.

### Fixed

- **Sentinel String References** — Replaced all hardcoded string literals with `tool.SentinelWaitingForUser`, `tool.SentinelPermissionAsk`, and `tool.SentinelQuestionPrompt` for cleaner, centralized control.

---

## [Unreleased] — 2026-05-22

### Added
- **/context Slash Command** — New token budget inspector showing all sources contributing to base prompt context: mode system prompt, ambient files (AGENTS.md, CLAUDE.md, .cursorrules, .opencode/rules/*.md), plugin instructions, built-in tools, MCP tools grouped by server, available skills (on-demand), and live session token usage. Estimates tokens via `len(text)/4` approximation.
- **Agent Permission Default** — `agent` tool moved from `PermissionAsk` to `PermissionAllow` in default rules, making subagent spawning non-interactive by default.

---

## [Unreleased] — 2026-05-21 (Later)

### Fixed
- **Error Message Duplication in Chat Methods** — Refactored error formatting in `chatCopilot`, `chatOpenAI`, `chatOpenAIResponses`, and `chatAnthropic` to extract message format string once, avoiding redundant formatting and improving maintainability.
- **Silent OpenAI Responses Usage Parse Errors** — Fixed `parseOpenAIResponsesUsage` error handling to emit debug log when parsing fails instead of silently ignoring the error.
- **ESC Key Stream Cancellation** — ESC key now cancels a running stream immediately in `handleChatKeys`, regardless of modal or sub-state focus, ensuring consistent interruption behavior.

## [Unreleased] — 2026-05-21

### Added
- **Model Registry Reasoning Flag** — `ModelSupportsThinking()` expanded to cover OpenAI, Gemini, DeepSeek, and other providers via heuristic matching on model name patterns.
- **Reasoning Effort Mapping** — `reasoningEffortForBudget()` maps thinking budget levels to OpenAI `reasoning_effort` values for both chat and responses APIs.
- **Task Status Tool** — New `task_status` tool for querying async sub-agent run state, plus OpenCode-compatible `task`/`task_id` aliases.
- **Synchronous Sub-Agent TUI Visibility** — Synchronous sub-agent runs now register in the run registry and emit `JobEvent` on completion so they appear in the TUI job panel.
- **Repo Tools** — New `repo_clone` and `repo_overview` tools (`internal/tool/repo.go`) for cloning and analysing remote git repositories under a confined path extension.
- **Plan Tools** — New `plan_enter` and `plan_exit` tools (`internal/tool/plan.go`) for structured multi-step planning workflow; `plan_enter` refuses to overwrite an existing today's plan.
- **OcodeConfig** — TUI and other user-facing settings migrated from flat `Config` to `OcodeConfig` struct (`internal/config/ocodeconfig.go`), loaded from `.opencode/config.yaml`.
- **Git Panel Editor Support** — File editor can be launched directly from the git panel.
- **Sidebar Todo Progress Bar** — Visual progress indicator for todo items in the sidebar.
- **Scroll-Box Height Cap** — Scroll boxes now have a configurable maximum height to avoid over-tall viewports.
- **File Path Formatting** — File path display uses abbreviated formatting for long paths.

### Fixed
- **Sub-Agent `notifyDone` on Synchronous Runs** — Success and error paths for synchronous tasks now call `notifyDone`, fixing silent non-completion in the TUI job view.
- **Nil Pointer on `Ocode` Config** — Guards added in `applyTheme`, keybind setup, leader timeout, and scroll speed to handle a nil `Config.Ocode` pointer without panicking.
- **`plan_enter` Overwrite Protection** — Calling `plan_enter` twice on the same day no longer silently overwrites a partially filled plan template.
- **OpenAI Responses Usage Accounting** — Responses API usage is now parsed from `input_tokens`/`output_tokens`/`total_tokens` so telemetry and spend calculations keep working for OpenAI Responses clients.
- **Git Diff Prefix Handling** — Git hunk parsing now strips `a/` and `b/` prefixes consistently so file selection and diff previews stay aligned across staged/unstaged views.

---

## [Unreleased] — 2026-05-20

### Added
- **Agent Runs Tracking** — `AgentRun` struct and `RunStatus` (running/done/failed) for tracking async subagent executions with lifecycle, transcript capture (capped at 200 messages), and process registry attachment. Supports cancellation via `Cancel()` callback.
- **Background Process Management** — `Process` struct and `ProcStatus` (running/exited/killed) for monitoring background shell processes, exit codes, and circular buffer output (256KB cap). Includes `appendOutput()` and `readSince()` for log tailing without memory bloat.
- **Process Registry** — `ProcessRegistry` for tracking and querying background processes across a session, with thread-safe lifecycle management and process queries by ID or command pattern.
- **Wait Tool** — New tool (`process_tools.go`) that blocks until a background process or async agent run reaches a terminal state, with timeout support and structured result reporting (status, exit code, or error).
- **TUI Detail View** — New `DetailView` component (`detail_view.go`) for drilling into agent run transcripts and process logs, with viewport pagination, search, and status indicators. Accessible from main transcript via Drill/D keybind.
- **Extended Thinking for Anthropic** — `ThinkingBudget` config field and `ModelSupportsThinking()` to enable Anthropic extended thinking (`interleaved-thinking-2025-05-14` beta) on Claude 3.7+/4+ models. Toggle with `Ctrl+T` cycling through off/low/med/high (0, 1024, 8000, 16000 token budgets). Thinking content rendered with distinct italic styling in transcript.
- **Tool Result Truncation** — `agent.TruncateToolResult()` truncates tool outputs >100 lines, writing the full result to `~/.local/state/opencode/tool-results/<toolUseID>.txt` with an inline notice and retrieval instructions for the model. Applied in `agent.Step()`, TUI `executeApprovedTool()`, and `executeToolWithRules()`.
- **Model Context Window Registry** — `ModelWindow()` queries the models.dev registry for provider/model context windows, with fallback to hardcoded values. Used in sidebar telemetry for accurate context usage display.
- **Input Area Mouse Selection** — Text selection in the TUI input area via mouse drag, with visual highlight and automatic clipboard copy on release.
- **Input Area Click Positioning** — Clicking in the input area positions the selection start point for mouse-based text selection.
- **File Tree Mouse Click** — Click on file tree nodes to select/open files or toggle directories, with `treeNodeForClick()` coordinate calculation.
- **Slash Popup File Path Detection** — `looksLikeFilePath()` prevents slash-command popup when pasting absolute file paths (e.g., `/path/to/file.png`) into the input.
- **Themed Selection Styles** — Replaced hardcoded `lipgloss.NewStyle().Reverse(true)` with themed `selectedStyle` across Files tree, Git file list, commits, stashes, branches, and commit viewport. Includes `ApplyThemeColors()` setters for all new styles.
- **Themed Log Viewport** — Debug log entries now use theme-aware styles (`userStyle`, `headerStyle`, `successStyle`, `errorStyle`, `hintStyle`) instead of hardcoded hex colors.
- **Theme Style Setters** — Added `setSelectedStyle`, `setStatusStyle`, `setSuccessStyle`, `setErrorStyle`, `setTextStyle`, `setThinkingStyle`, `setDimStyle`, `setToolBoxStyle`, `setScrollbarStyles`, `setTodoStyles` for full theme support.
- **Theme Test** — `TestApplyThemeColorsUpdatesScrollbarStyles` verifies theme application affects scrollbar and selection styles.
- **Session Title in Header & Sidebar** — Header bar displays `◆ ocode <session-title>` when set; sidebar header shows session title or fallback hint text.
- **Status Bar Thinking Indicator** — Status bar shows `ctrl+t: thinking[off|low|med|high]` when the active model supports extended thinking.
- **Read Tool Continuation Format** — Changed from `…(N more lines, use start_line=M to continue)` to `…(use start_line=M, limit=50 to continue)` for clearer pagination.
- **Read Tool Render Continuation** — Tool render preserves the continuation footer from read results, showing it after the preview with faint styling.

### Fixed
- **Session Title Regex Anchoring** — Changed `ocodeTitleRe` from anchored `^<ocode-title>` to non-anchored `<ocode-title>`, allowing title tags anywhere in the response. Fixed `extractSessionTitle` to properly strip the tag without losing surrounding content.
- **Compact Command Telemetry Reset** — `handleCompactCmd` now resets `sessionTelemetry` to avoid stale context usage data after compaction.
- **File Tree Focus Border** — Border highlight uses `selectedStyle.GetBackground()` instead of hardcoded `#7AA2F7`.
- **Git Model Section Highlights** — All section highlights (changes, commits, stashes, branches) use themed `selectedStyle` for consistent appearance across themes.
- **Slash Popup Test** — `TestChangesFileListHighlight` calls `ApplyThemeColors("opencode")` and checks `selectedStyle` instead of raw ANSI reverse codes.

### Changed
- **config.Config** — Added `ThinkingBudget int` (runtime-only, `json:"-"`) for extended thinking token budget.
- **GenericClient** — Added `ThinkingBudget int` field; `chatAnthropic()` passes `thinking` block and adjusts `max_tokens` when budget > 0. Sets `interleaved-thinking-2025-05-14` beta header for Anthropic.
- **NewClient** — Propagates `cfg.ThinkingBudget` to `GenericClient`.
- **modelEntry** — Now includes `Limit.Context` field for context window size from models.dev registry.
