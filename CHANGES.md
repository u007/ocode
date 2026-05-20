# Changelog

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
