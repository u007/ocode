# Agent Instructions — ocode

This file is the canonical, always-on briefing for any LLM agent (ocode, Claude
Code, Cursor, etc.) that loads `AGENTS.md` from the repo root. It is loaded
silently and unconditionally at session start by `internal/agent/context.go` —
see "Context Loading" below. Keep the content here focused on **cross-cutting
rules that affect more than one file**: recurring bug classes, architectural
constraints, and process rules. Feature descriptions belong in `README.md` or
the `skills/ocode-*` catalog, not here.

`CLAUDE.md` is a thin pointer to this file (kept for tools that auto-load it by
name). Do not duplicate content between the two — update here only.

## Tech Stack
- Go 1.23
- Charm TUI (Bubble Tea, Lipgloss v2 — note v2 wraps each rune in its own
  SGR sequence; substring assertions on rendered output need `stripANSI` from
  `internal/tui/selection.go`)
- LLM providers: OpenAI, Anthropic, Google, Z.AI, Alibaba, plus the
  `opencode-go` (DeepSeek) and Minimax routes

## Git Worktrees
The default location for `git worktree` checkouts is `.worktrees/` in the
project root. The directory is gitignored — worktree contents are
developer-local state and must never be committed.
```bash
git worktree add .worktrees/feature-branch feature-branch
```

## Coding Standards
- Use modular packages in `internal/`.
- Respect `.gitignore` and `watcher.ignore`.
- Follow Go best practices and standard formatting (`gofmt`, `go vet`).
- **Avoid `git stash` / `git reset --hard` / `git checkout -- <file>` /
  `git clean -fd` as a default coping strategy.** They destroy user state
  the user may not be unable to recover. If a change conflicts, stop and
  ask; do not unwind the user's working tree.

## Context Loading
- `AGENTS.md`, `CLAUDE.md`, `OCODE.md`, and `.cursorrules` are loaded at
  session start by `internal/agent/context.go::LoadContext`.
- If a context file is tracked by git AND has unstaged modifications, the
  committed `HEAD` version is used instead of the working-tree copy. This
  keeps the base prompt stable across edits; commit the changes to make
  them effective. A line is logged to stderr when this swap occurs.
- When reading files, show only the relevant excerpts needed for the
  current task — do not dump entire files.

## Knowledge System (OKF Bundle)
The project supports an **OKF v0.1 knowledge bundle** at `docs/` — a curated set of
markdown files with YAML frontmatter (type, title, description, tags, timestamp,
status). The system activates when both `/docs on` is set AND `docs/index.md`
frontmatter contains `okf_version: "0.1"` (created by `/docs init`).

### Agents

| Agent | Role | Tools |
|-------|------|-------|
| `context` | Knowledge curator and retriever — answers why/decision/playbook questions, sole automated writer | `grep`, `glob`, `read`, `list`, `doc_search`, `doc_get`, `doc_write`, `doc_deprecate` |
| `explore` | Code-level exploration — where/how/what in source | `read`, `glob`, `grep`, `list`, `lsp`, `bash`, `webfetch`, `websearch` |

Guidance in the `DocPromptEnabled` prompt fragment: try `knowledge_lookup` first
for why/decision/playbook questions; use `explore` for code-level questions; for
mixed questions dispatch both in background concurrently, take the first
sufficient answer, `task_cancel` the other.

### Tools

| Tool | Availability | Description |
|------|-------------|-------------|
| `knowledge_lookup` | Always registered | Dispatches the `context` sub-agent to answer knowledge questions. Soft-fails (hints `/docs init`) when inactive. |
| `task_cancel` | Always registered | Cancels a background task **you** dispatched. Cooperative — stops at the next step boundary. Ownership enforced by dispatcher identity. |
| `doc_search`, `doc_get`, `doc_write`, `doc_deprecate` | Context sub-agent only | Full CRUD on the knowledge bundle. Write/deprecate auto-update `log.md` and regenerate `index.md` under cross-instance file lock. |

### `/docs` subcommands

| Subcommand | Behavior |
|------------|----------|
| `on` / `off` | Toggle doc-first development prompt (master switch for knowledge system) |
| `status` | Bundle presence, doc counts (conforming/non-conforming/deprecated), last log entry, active state |
| `init` | Bootstrap `docs/` into an OKF bundle — add frontmatter, generate index + log, emit staleness report. Non-destructive, idempotent. |
| `update [focus]` | Force a maintenance pass (checks transcript for durable knowledge) |
| `cleanup [--yes]` | List deprecated docs; `--yes` deletes them under lock with log+index update |

### Maintenance
A post-job doc maintenance worker mirrors memory maintenance:
1. **Triage (small model):** Decides if the last turn produced durable knowledge (decisions, gotchas, playbooks, schema changes). Q&A and routine edits are noops.
2. **Execute:** Dispatches the `context` sub-agent to apply create/update/deprecate actions.
3. All mutations go through `knowledge.WithBundleLock` (`docs/.okf.lock`, flock-style).
4. Worker drains on `Agent.Shutdown()` — drops queued items, finishes the current one.

### Relationships
- **`docs` primary agent** (ModeDocs) has the `task` tool so it can dispatch `context` for knowledge lookups, but has no direct doc tools.
- **`/doc-sync`** (rules/skills sync) is unrelated — its scope is `AGENTS.md`, rules, skills, never `docs/`.
- **Memory scopes** (`/mem`) are orthogonal — knowledge bundle is project docs, memory is agent state.

## File Reading
When reading files, show only the relevant excerpts needed for the current
task. Whole-file dumps waste the context window and obscure the signal.

## OKF Knowledge Bundle (`docs/`)
The project supports an optional **OKF (Open Knowledge Format) knowledge
bundle** rooted at `docs/`. When active, the agent receives a
`[ocode:knowledge]` index in its system prompt and gains the
`knowledge_lookup` tool for semantic retrieval.

**Activation** (two gates, both required):
1. `DocPromptEnabled` flag — toggled via `/docs on` (persisted in config).
2. Bundle marker — `docs/index.md` must have `okf_version: "0.1"`
   frontmatter, created only by `/docs init`.

**`context` subagent** (the sole automated writer):
- `task(agent="context", prompt="...")` dispatches a small-model subagent that
  has exclusive write access to the bundle via injected doc tools
  (`doc_search`, `doc_get`, `doc_write`, `doc_deprecate`).
- The `context` agent verifies doc claims against code before writing, prefers
  updating existing docs over creating near-duplicates, and deprecates rather
  than deletes. It is the only code path that can mutate the bundle.
- Use for "what/why did we decide X", "find the playbook for Y", "update the
  schema doc for Z".
- The `explore` subagent is for general codebase research; `context` is pinned
  to the knowledge bundle.

**`knowledge_lookup` tool**: injected into every agent turn when the bundle is
active. Performs semantic search across the bundle index. Results are injected
into the `[ocode:knowledge]` system block.

**`task_cancel` tool**: cancels a running sub-agent (including async context
agents) by run ID. Available to the main agent whenever sub-agents are active.

**Sole-automated-writer invariant**: No agent path outside the `context`
subagent may write to the bundle. The main agent's tool set never includes
`doc_write` or `doc_deprecate`. Deletion happens only via `/docs cleanup`
(per-file confirmation required).

**`/docs` subcommands** (TUI):
- `/docs on|off` — toggle the knowledge system flag.
- `/docs status` — show bundle presence, doc counts (conforming / non-conforming
  / deprecated), last log entry date, whether the system is active.
- `/docs init` — create bundle marker (`docs/index.md` + `docs/log.md`) and
  dispatch the `context` subagent to scan & annotate existing docs. Idempotent
  (re-run re-audits without clobbering).
- `/docs update [focus]` — force a maintenance pass (scan for staleness,
  duplicates, orphans). Queued asynchronously.
- `/docs cleanup` — list deprecated docs with path and reason; use `--yes` to
  confirm deletion. Deletes files under bundle lock, logs deletions, regenerates
  index.

## TUI Output Safety (alt-screen)
The TUI runs in Bubble Tea's alt-screen. Any raw write to `os.Stdout` /
`os.Stderr` from a path the running TUI invokes paints directly over the
rendered frame and corrupts it (text overlap / "hairwire" at the bottom of
the chat, status line pushed off-screen). This is a recurring bug class —
when fixing rendering glitches, suspect raw writes, not just layout.

In any code reachable while the TUI is live (agent loop, tools, hooks,
session, plugins, auth, config reload):

- **Never** `fmt.Print*`, `fmt.Fprint*(os.Stdout|os.Stderr, …)`, `println`,
  or raw `os.Stderr.Write` for diagnostics. Use `agent.emitDebug` /
  `agent.DebugAppendf` inside the `agent` package, or the stdlib `log`
  package elsewhere — `tui.Run()` calls `log.SetOutput(debugLogWriter{})`,
  so `log.Printf` lands in the debug panel, never the terminal. `emitDebug`
  falls back to stderr only when no sink is set (headless `run`/`serve`/`acp`).
- **Capture subprocess output** (`cmd.Stdout = &buf`) — never inherit the
  terminal with `cmd.Stdout = os.Stdout`. Surface captured output via
  `log`/the error, not the inherited fd.
- **Clamp one-line status/activity rows** with `.Width(w).MaxHeight(1)` so
  long content can't wrap and grow the bottom chrome past the terminal
  height.
- **Never use double-width emoji as inline status prefixes** (e.g. `⏳`, `⌛`, `⚙️`). Wide emoji are 2-cell characters; VS Code's terminal renderer shifts all following text right, making rows appear crooked/misaligned. Use single-width ASCII symbols (`~`, `*`, `>`) for inline status indicators in `appendDiscoveryNotice` and similar helpers.

## TUI Mouse: clickable chrome vs selectable content
Terminal mouse capture is **global per frame** — `tea.View.MouseMode` is
one flag for the whole screen, not per-region. Enabling capture makes
tabs/menus/buttons clickable but **blocks native terminal text selection**.
The two are mutually exclusive and cannot be scoped to a region. Never
disable `MouseMode` to regain native selection — that kills every click
target.

The correct pattern (and the only one that satisfies "nav is clickable AND
content is selectable"): **keep mouse capture ON and implement selection
in-app.** Every scrollable/content surface follows the same recipe (see
the transcript, log tab, files preview, git diff, sidebar, and agent-detail
drill-in for working copies):

- A `selectionState{dragging, startLine, startCol, endLine, endCol, active}`
  field per surface.
- **Press** inside the content region → record start + `dragging:true`
  (return handled).
- **Motion** while dragging → update end, set `active` only once the
  anchor actually moved, re-render with `applySelectionHighlight(styled, raw, …)`.
- **Release** → if `active`, `extractSelectionText(rawLines, …)` +
  `clipboard.WriteAll` (log copy errors, never swallow); if **not active**
  (no drag distance) clear and **fall through to the click handler** so a
  plain click still toggles/opens. This press-starts-drag /
  release-decides-click-vs-copy split is what lets one region be both
  clickable and selectable.
- Track the surface's styled + ANSI-stripped (`stripANSI`) visual lines so
  highlight and extract operate on the same coordinate space. Selection
  coords are **screen-row/col relative to the content's top-left**
  (`contentTopY`, left chrome = border(1)+padding(1) = 2 cols for bordered
  boxes).

Mouse-mode gotcha for **hover** effects (underline-on-hover):
`MouseModeCellMotion` only emits motion while a button is held — it
delivers no plain-hover events. Hover requires `MouseModeAllMotion`, and
the motion handler must process `MouseNone` motion (don't early-return on
`Button != MouseLeft` before the hover check). `AllMotion` fires on every
cursor move, so the hover handler must be cheap: read cached
geometry/hit-test maps populated during render, and only return a redraw
when the hovered target actually changes.

## TUI Clickable URLs — confirm before opening
URLs in the chat transcript (markdown `[text](url)` and raw `https?://...`)
are clickable on the chat tab. **A click always opens a Y/N confirmation
dialog before launching the browser** — `m.showURLDialog` in
`internal/tui/model.go`. There is no "trust once for the session"
shortcut. The URL is sanity-checked by `looksLikeURL` (http/https only,
host has a dot or is `localhost`) but is not otherwise sanitized; the
dialog is the safety layer. Adding a new URL surface (sidebar, log tab,
file preview) must follow the same confirm-before-open pattern.

## TUI In-Chat Find Bar
`ctrl+f` on the chat tab opens a find bar above the input area (NOT on
other tabs — the model picker, file search, and the log tab all bind
`ctrl+f` for themselves). The bar is closed when the user leaves the chat
tab (`closeChatSearchIfLeavingChat`). Implementation lives in
`internal/tui/chat_search.go`; do not add a second find surface without
consolidating the dispatch.

## User Interaction
- TUI supports `/commands` and `!shell`.
- **Slash command queuing.** All slash commands entered while the agent is
  streaming or compacting must be queued (`m.queuedCommands`) and
  executed one-at-a-time after the current work ends — not run
  immediately. Only `/exit`, `/quit`, `/q` bypass the queue
  unconditionally. Synchronous local UI/config commands that do not start
  a new agent request may also bypass the queue; keep any such exceptions
  centralized in `handleCommand` (the single chokepoint covering all
  callers: enter key, palette, keybinds, leader shortcuts, hotkeys) and
  **document them in the running list below** (so the next contributor
  knows the rule). Drain `m.queuedCommands` in `agentStreamDoneMsg` and
  `compactFinishedMsg` handlers, after `queuedInputs` are processed, so
  a command never fires while another stream is in flight.
  - Current instant commands: `/model`, `/models`, `/help`, `/thinking`,
    `/details`, `/login`, `/new`, `/clear`, `/sidebar`, `/commands`,
    `/permissions`, `/yolo`, `/small-model`, `/editor`, `/editor-mode`,
    `/themes`, `/theme`, `/lsp`, `/usage`, `/share`, `/connect`, `/agent`,
    `/mcp`, `/advisor`, `/mask`, `/btw`, `/by-the-way`, `/rc`,
    `/remote-control`, `/search`, `/find`, `/docs`, `/doc-mode`, `/recap`.
  - **Queued by design (mutates persistent state mid-stream, so it must
    wait for the current turn to end):** `/add-dir`, `/add-dirs`, `/doc-sync`.
  - The list above is the source of truth; keep the in-code check in
    `handleCommand` in sync.
- Use `ctrl+x` for leader keys and `ctrl+p` for palette.
- Avoid introducing raw shortcuts that are likely to conflict with host
  terminals like Warp, Ghostty, and iTerm2; prefer `ctrl+x` leader
  sequences for non-essential UI toggles.
- Sessions are automatically saved and resumed.

## Data Storage
All persistent state lives under a single cross-platform global directory
resolved by `internal/paths.GlobalDataDir()`:

| Platform | Path |
|----------|------|
| macOS    | `~/.local/share/opencode` |
| Linux    | `$XDG_DATA_HOME/opencode` (or `~/.local/share/opencode`) |
| Windows  | `%LOCALAPPDATA%\opencode` |

Sub-directories:
- `project/{slug}/sessions/` — chat session JSON files (one per session)
- `usage/` — LLM token usage records (`records.jsonl`)
- `auth.json` — provider API keys and OAuth tokens

The `{slug}` is a SHA-256 prefix of the git repo root path, making sessions
project-scoped even when working from different checkouts. The TUI's
`m.workDir` is the source of truth for project resolution (set via
`/cd`, `--dir`, or `session.SetWorkDir`); `os.Getwd()` is not — `/cd`
can change the project root without changing the process CWD on every
caller.

## Prompt Cache Stability
Anthropic prompt caching reads one linear prefix in a **fixed order: `tools` →
`system` → `messages`** (breakpoints set in `internal/agent/client.go` — last
tool ~:2013, system ~:1961, first user message ~:1971). Because tools come
first, **any change to the tools array invalidates the `system` block and the
message prefix too** — they sit downstream of tools in the prefix. This is the
dominant cost when adding features that vary what gets sent.

Rules for any change that touches tools or the base prompt:
- **Never put per-turn-varying content in `tools` or `system`.** `LoadContext`
  (system) must be a function of stable, preload-time state (config flags), not
  of a per-turn computed result.
- **`GetToolDefinitions` must emit a deterministic order** (`sort.Strings` over
  names). `a.tools` is a map — unsorted iteration randomizes the tools array
  every turn and busts the cache on every request.
- **Tool sets that grow must be grow-only/sticky within a session** (see the
  discovery `Session`). A no-new-attachment turn then sends a byte-identical
  tools array → full cache hit; only growth turns pay a re-cache.
- **Role determines caching, not array position — because of the hoist.** The
  Anthropic builder (`chatAnthropic` → `collectAndRemoveSystemMessages` in
  `client.go`) pulls **every `system`-role message — including tail ones — into
  the top-level `system` field**, which carries `cache_control`. So a
  `system`-role message appended at the tail is NOT in the uncached suffix; it
  rides the **cached** system block. Consequence: any tail `system` injection
  whose content **varies per turn** (e.g. growing) rewrites and busts the whole
  cached system prompt. `injectLSPDiagnostics` and `injectNotesTail` are
  system-role and carry this cost when their content changes — keep their content
  stable across turns, or move the volatile part to user-role.
- **Split tail injection by volatility (`injectDiscoveryContext` is the model):**
  - *Stable* content (e.g. the discovery name index + prompt contract — names
    don't change turn to turn) → **`system`-role** → hoisted into the cached
    system block, so it caches.
  - *Volatile* content (e.g. attached-skill full descriptions and attached
    project-doc full file content, which grow with the sticky set) →
    **`user`-role** → `collectAndRemove` leaves it in the messages array
    (uncached suffix), where it coalesces with the current user turn and never
    busts the system cache. Wrap it in the `[ocode:discovery]` marker so the
    model reads it as system-origin, not user speech.
- **Markdown docs are part of the discovery corpus (`md_discovery.go`).** Every
  project `*.md` except the always-on briefing set (`AGENTS.md`, `CLAUDE.md`,
  `OCODE.md`, `.cursorrules`, `.opencode/rules/*.md`, which `LoadContext` injects
  in full) is a `Kind:"md"` Doc whose `Text` is an LLM summary (small model when
  configured, else the main client), cached at `.ocode/md-summaries.json` keyed
  by file content (mtime+size gate, then sha256). The first activation runs a
  **blocking** pass (`mdSummarizePass`, bounded concurrency `mdSummaryWorkers`)
  so the corpus is fully summarized before the turn proceeds; failed
  summarizations are negative-cached (`mdFailBackoff`) and never become
  placeholders. The names-index lists `path — summary`; the full file content is
  attached to the volatile tail only on query match. Editing a doc invalidates
  its summary on the next throttled scan (`mdScanThrottle`), so `/doc-sync` edits
  are reflected automatically.

## Environment Prompt
The LLM receives environment context at the start of each session via
`internal/agent/prompt.go`. The exact shape is the ` <env>...</env>` block
in that file; if you are reading the values out of the prompt at runtime,
parse the block — do not assume the example below is current. The
illustrative shape is:

```
<env>
  Working directory: /path/to/project
  Workspace root folder: /path/to/project
  Is directory a git repo: yes
  Git branch: main
  Platform: darwin
  Today's date: <resolved at session start>
</env>
```

The git branch is resolved via `git rev-parse --abbrev-ref HEAD` when the
workspace is a git repo.
