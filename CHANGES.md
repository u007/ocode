# Changelog

## [Unreleased]

### Fixed
- **Command File Parser** — Fixed 50-line cap in `parseCommandFile` that silently truncated command prompt bodies longer than 50 lines after frontmatter (e.g., `/git-commit-push.md` dropped its "Step 4: Stage" and "Step 5: Commit and push" sections). Now reads the full file content.
- **Multi-Session Permission Clobbering** — `SaveOcodePermissions` no longer overwrites the on-disk `auto.permissions.model` (owned exclusively by `SavePermissionModel`). Also preserves the entire disk auto block when the calling session has no auto block.
- **Transcript Auto-Scroll** — Changed to sticky-bottom behavior: only follows when pinned to the bottom; one wheel-up stops auto-scroll and stays put while the LLM continues streaming.
- **Model Picker Favorite Toggle** — Changed favorite toggle keybinding from `f` (which conflicted with type-to-filter) to `ctrl+f`.
- **Git Tab Mouse Click Mapping** — Corrected section panel and file-list click coordinate mapping (border offset, staged/unstaged header row offset); clamped negative content lines in diff click.
- **Files Tab Content Search Cancellation** — Pressing Esc now properly cancels an in-flight content search.

### Added
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

### Changed
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

## [0.3.1] — 2026-06-04

### Added
- **CONTRIBUTING.md** — Development setup guide, code conventions, and PR guidelines for contributors.
- **TEAM_ONBOARDING.md** — Comprehensive team onboarding documentation covering architecture, build/test/run commands, and development workflow.
- **Team Onboarding Skill** — New bundled skill (`skills/team-onboarding/`) for generating onboarding docs from codebase analysis.

### Changed
- **README.md** — Added "Why ocode?" section highlighting lightweight design, auto-permissions, and extensibility. Added link to CONTRIBUTING.md in Quick Start. Added Support section with links to issue tracker and OpenCode Go plan.

### Security
- **Exfiltration-Risk Detection for URL-Calling Commands** — `IsHarmfulBashCommand()` now detects data exfiltration risk in `curl`, `wget`, `httpie`, and `netcat` commands. Commands that could leak secrets (file upload via `-d @file`, `-F file=@secret`, `--upload-file`; env var injection via `-H "Auth: $TOKEN"`; subshell expansion `$(cat .env)`; proxy/config redirects) are flagged as **harmful** — they always require human approval and cannot be persisted as "always allow". Benign usage like `curl https://api.example.com/get` is not affected. Covers 4 detection categories across 4 tools with ~50 test cases.

## [Unreleased] — 2026-06-02

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
