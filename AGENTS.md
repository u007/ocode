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
- Go 1.26.1 (per `go.mod`)
- Charm TUI (Bubble Tea, Lipgloss v2 — note v2 wraps each rune in its own
  SGR sequence; substring assertions on rendered output need `stripANSI` from
  `internal/tui/selection.go`)
- LLM providers: OpenAI, Anthropic, Google, Z.AI, Alibaba, plus the
  `opencode-go` (DeepSeek) and Minimax routes
- Every request to an `opencode*` provider must carry `X-Opencode-Session`, an
  opaque ID stable for one conversation (Zen/Go pin the conversation to one
  upstream for prompt caching; some Go models 400 without it). All transports
  capable of serving `opencode*` providers call `setOpencodeSessionHeader`.
  The identity source is `Agent.SetSessionID` for CLI/server sessions,
  `Agent.SetOpenCodeSessionID` for TUI sessions, or one lazily resolved
  fallback on the owning agent. Compaction, replacement, and side-query
  clients inherit the same identity. New transports must call the helper too.

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
  the user may not be unable to recover.
  - To *revert a file edit you just made*, prefer the **`undo_file_change`**
    tool: pass the `tool_call_id` of the write/edit/patch/delete/multi-edit/
    replace that produced the change, and it restores exactly the files that
    call touched to their pre-edit state. It is valid within your most recent
    agent steps (default 10, configurable via `ocode.undo_max_age_delta` in
    `ocodeconfig.json`; `0`/unset uses the default), so call it promptly after a
    bad edit. (Every file
    write is backed up automatically by the snapshot store, so no extra setup
    is needed.)
  - Only fall back to git (`git checkout`/`git restore`/`git revert`) when you
    specifically need to undo a *commit*, switch branches, or restore files
    the write-snapshot did not capture (e.g. untracked files). Never use
    `git checkout -- <file>` / `git clean -fd` just to undo your own recent
    edits.
  - If a change conflicts, stop and ask; do not unwind the user's working
    tree.
- **Never overwrite production or remote `.env` files** (`.env`,
  `.env.production`, `.env.local`, or any environment-specific variant used
  in deployed/remote contexts) unless the user explicitly requests it.
  These files often contain secrets, API keys, or configuration that is
  different from local dev — blindly replacing them can break deployments
  or leak credentials. When in doubt, ask.

## Sandbox permission mode

`sandbox` is the fourth permission mode (besides `normal`/`yolo`/`locked`),
toggled from the TUI permission-mode click cycle, `/sandbox`, or the web mode
selector. It is **write-integrity confinement only** — it does NOT protect
secrets or prevent exfiltration.

What it confines:
- Only the agent **shell tool** (`bash`) is wrapped. The interactive PTY
  terminal (`handler_terminal.go`) and the web `!shell` path run unsandboxed.
- Filesystem **writes** fail at the OS level unless the target is under a
  writable root: workspace/`extra_allowed_paths`, language dependency caches
  (npm/pip/cargo/go/maven/gradle), `~/.claude`, and temp dirs
  (`/tmp`, `/var/tmp`, `os.TempDir()`, plus on macOS the uid-owned
  `/var/folders/*/*/{T,C}` confstr dirs — found by ownership, not `$TMPDIR`,
  so `mktemp`/Python/Node/clang caches work even when ocode runs without
  `TMPDIR`, e.g. under launchd; see `pathscope.DarwinUserDirs`).
- **Reads and exec stay global, network egress is open** — toolchains need
  them. A sandboxed `python`/`node` can still read `.env`/`~/.ssh`/`auth.json`
  and POST them anywhere.

What still asks (permission layer, not the OS):
- `auth.json` (read or write) → Ask
- ocode config/data dir (writes only) → Ask
- `~/.ssh`, `.env` (read or write) → Ask
- danger-`rm` heuristics → Ask
- writes to permission-defining files (`.ocode/settings.json`,
  `.claude/settings.json`, ocode config gating files) and loopback requests to
  `/api/permissions*` → Ask (self-escalation guard, all modes)

Ask routes to the auto-permission LLM judge when `auto` is on, else a human
prompt. These static checks catch direct commands (`cat auth.json`), but NOT a
read/write hidden inside an interpreter (`python -c ...`) — the backends are
write-walls only and never OS-block secret reads (that would make approval
impossible). Real config/secret changes should be made outside sandbox.

Platform matrix:
- macOS: real — Seatbelt via `/usr/bin/sandbox-exec` (trusted absolute path)
- Linux: real — Landlock (kernel ≥5.13, ABI-probed, `PR_SET_NO_NEW_PRIVS`)
  with `bubblewrap` (`/usr/bin/bwrap`) fallback
- Windows: no backend — selecting sandbox behaves like `normal` (prompts),
  no confinement

Toggling is per-session and never persisted: restarts return to `normal`.
Fail-closed on macOS/Linux: if the mode is sandbox but no backend is available,
the command errors before starting (no silent unsandboxed execution).

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
The project supports an optional **OKF v0.1 knowledge bundle** at `docs/` — a
curated set of markdown files with YAML frontmatter (type, title, description,
tags, timestamp, status). When active, the agent receives a
`[ocode:knowledge]` index in its system prompt and gains the
`knowledge_lookup` tool for semantic retrieval.

**Activation** (two gates, both required):
1. `DocPromptEnabled` flag — toggled via `/docs on` (persisted in config).
2. Bundle marker — `docs/index.md` must have `okf_version: "0.1"`
   frontmatter, created only by `/docs init`.

### Agents

| Agent | Role | Tools |
|-------|------|-------|
| `context` | Knowledge curator/retriever and **sole automated writer** to the bundle. Answers why/decision/playbook questions; verifies doc claims against code before writing; prefers updating existing docs over near-duplicates; deprecates rather than deletes. Uses the configured context model if set and enabled, else the small model, else the main model (see `/context-model`). When the bundle is active, also handles codebase exploration (where/how/what) — subsumes `explore` — with full explore toolkit plus doc tools; `explore` is hidden from the schema. Priority: `doc_search` first, then `doc_get`, then code tools; parallel `doc_search` + code search in the same batch is allowed for mixed questions. | `grep`, `glob`, `read`, `list`, `lsp`, `bash`, `webfetch`, `websearch`, `doc_search`, `doc_get`, `doc_write`, `doc_deprecate` |
| `explore` | Code-level exploration — where/how/what in source; general codebase research, not pinned to the knowledge bundle (hidden when `context` is active) | `read`, `glob`, `grep`, `list`, `lsp`, `bash`, `webfetch`, `websearch` |

Guidance in the `DocPromptEnabled` prompt fragment: try `knowledge_lookup` first
for why/decision/playbook questions; when the bundle is active use `context` (which subsumes `explore`) for code-level or mixed (why+where) questions — `explore` is hidden — with knowledge-first priority: `doc_search` first, `doc_get` as needed, then code tools, parallel batch allowed; when the bundle is inactive, use `explore` for code-level questions as before.

**Sole-automated-writer invariant:** no agent path outside the `context`
subagent may write to the bundle — the main agent's tool set never includes
`doc_write`/`doc_deprecate`. Deletion happens only via `/docs cleanup`
(per-file confirmation required).

### Tools

| Tool | Availability | Description |
|------|-------------|-------------|
| `knowledge_lookup` | Always registered | Dispatches the `context` sub-agent to answer knowledge questions. Soft-fails (hints `/docs init`) when inactive. |
| `task_cancel` | Always registered | Cancels a background task **you** dispatched (including async context agents), by run ID. Cooperative — stops at the next step boundary. Ownership enforced by dispatcher identity. |
| `doc_search`, `doc_get`, `doc_write`, `doc_deprecate` | Context sub-agent only | Full CRUD on the knowledge bundle. Write/deprecate auto-update `log.md` and regenerate `index.md` under cross-instance file lock (`knowledge.WithBundleLock`, `docs/.okf.lock`). |

### `/docs` subcommands

| Subcommand | Behavior |
|------------|----------|
| `on` / `off` | Toggle doc-first development prompt (master switch for knowledge system) |
| `status` | Bundle presence, doc counts (conforming/non-conforming/deprecated), last log entry, active state |
| `init` | Bootstrap `docs/` into an OKF bundle — add frontmatter, generate index + log, emit staleness report; dispatches the `context` subagent to scan & annotate existing docs. Non-destructive, idempotent (re-run re-audits without clobbering). |
| `update [focus]` | Force a maintenance pass (scan for staleness, duplicates, orphans). Queued asynchronously. |
| `cleanup [--yes]` | List deprecated docs with path and reason; `--yes` deletes them under lock, logs deletions, regenerates index. |

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

## TUI Changes Tab
The `internal/tui/changes_model.go` model implements a **changes tab** that
lists every file the current chat session has added or edited (main agent +
sub-agents), with per-file unified diffs and file-level or block-level undo.
The list is not git-based — it derives from the snapshot store
(`internal/snapshot.Store`) and a pre/post-stat bash detection hook. See
`docs/changes-tab.md` and `docs/superpowers/specs/2026-07-22-changes-tab-design.md`.

## User Interaction
- TUI supports `/commands` and `!shell`.
- **Slash command queuing.** All slash commands entered while the agent is
  streaming or compacting are queued (`m.queuedItems`, a unified queue
  preserving insertion order) and executed one-at-a-time after the current
  work ends — not run immediately. Only `/exit`, `/quit`, `/q` bypass the
  queue unconditionally. Synchronous local UI/config commands that do not
  start a new agent request may also bypass the queue ("instant" commands —
  e.g. `/model`, `/small-model`, `/explorer-model`, `/recap`, `/mask`,
  `/permissions`, `/discover`, theme/editor pickers). Drain `m.queuedItems`
  in `agentStreamDoneMsg` and `compactFinishedMsg` handlers, after input
  items are processed, so a command never fires while another stream is in
  flight.
  - **The `isInstantCmd` boolean chain in `handleCommand`
    (`internal/tui/model.go`) is the sole, authoritative list of instant
    commands — do not duplicate it here.** When adding a new synchronous
    local command, add it there (the single chokepoint covering every
    caller: enter key, palette, keybinds, leader shortcuts, hotkeys); this
    doc only explains the *category*, not the membership.
  - `/btw`/`/by-the-way` is deliberately NOT instant: it starts an independent
    side-query agent loop (its own child agent with its own client — see
    `Agent.AskLoopAsync`), and running it mid-stream would interleave a second
    concurrent LLM loop with the main turn (the popup also blocks input while
    open), so it queues like any other work. Unlike the old shared-client
    one-shot, there is no `OnDelta` race — the child's client is fresh — so
    queuing is about stream interleaving and UI, not token leakage.
  - **Queued by design (mutates persistent state mid-stream, so it must
    wait for the current turn to end):** `/add-dir`, `/add-dirs`, `/doc-sync`,
    `/agents limit <n>`.
- Use `ctrl+x` for leader keys and `ctrl+p` for palette.
- Avoid introducing raw shortcuts that are likely to conflict with host
  terminals like Warp, Ghostty, and iTerm2; prefer `ctrl+x` leader
  sequences for non-essential UI toggles.
- Sessions are automatically saved and resumed.

## Task Output Contracts (`expected_output`)
A `task` dispatch may carry an optional `expected_output`: a short
natural-language description of the shape/content the caller requires of the
result. When set, the child's final result is verified against it before being
returned, and retried **once** in place if it does not match. The mechanism:

- **Contract resolution** (`resolveContract` in `internal/agent/subagent.go`):
  the call-supplied `expected_output` wins; otherwise the agent definition's
  `expected_output:` frontmatter (parsed in `internal/agent/agent_loader.go`)
  applies; neither → verification is skipped entirely (byte-identical to the
  pre-contract path, zero added cost).
- **Verification** (`internal/agent/task_contract.go`): one call against the
  contract + the child's final text, using the configured small model when
  enabled, else the session client. The prompt lives in
  `internal/agent/prompts/task_verifier.txt` with the other subagent prompts.
  The verifier checks **shape, not truth** — a result that claims completion
  satisfies a contract it never fulfilled. Do not present the badge as
  "verified correct". A malformed/unparseable verdict is logged and treated as
  not-satisfied, never as satisfied.
- **Retry must live inside `runSyncDispatch`, not via `resume_task_id`.**
  The retry steps the same still-live child (`executeSubAgentWithTranscript`)
  with the deficiency appended to its full transcript, then re-verifies. It
  must happen before the `defer subAgent.shutdownTransient()` fires in
  `runSyncDispatch` (and, for background runs, inside the dispatch goroutine
  after `executeSubAgentWithTranscript` and before `finishOK`/`finishErr`).
  Routing the retry through `TaskTool.Execute` would rebuild the child from
  scratch and trip the re-dispatch guard (`subagentDispatchLimit`); the public
  `resume_task_id` path requires a terminal run, which does not hold
  mid-dispatch.
- **Reporting:** satisfied → result returned unchanged (no decoration). Not
  satisfied after retry → result **prefixed** with an explicit warning naming
  the contract and the deficiency; the full child result stays present. The
  verdict (checked / satisfied / deficiency) is recorded on the `AgentRun` via
  `SetContractVerdict` and surfaced in `agent_status` / `task_status`, the TUI
  agent strip + detail view, and the web Agents tab (DTO field `contract`).
- **No built-in agent declares a default contract** — contracts are opt-in per
  call (or via a user-authored agent's frontmatter). This keeps built-ins
  (e.g. `knowledge_lookup` via the `context` agent) free of verification
  overhead.

## Persistent todo plan (`todowrite` / `todoread` / `todo_update`)

A per-session todo plan lives at `.ocode/todo/<session-id>.md` — the durable,
user-visible, git-diffable record the model uses to keep its own plan in scope
across long runs and post-compaction turns. The store lives in
`internal/tool/todo_store.go`; the re-anchor injection lives in
`internal/agent/todo_inject.go`.

- **File format** (strict):
  ```
  # Todo (revision 3)

  - [ ] t1 First item
  - [•] t2 Second item
  - [✓] t3 Third item
  ```
  Every item carries a stable `tN` id so targeted updates can land without a
  full rewrite. The revision is the file's optimistic-concurrency token.
- **In-memory copy is a cache, not a second source of truth.** `TodoState()`
  is on the TUI render path and must never touch the disk. The memory copy is
  populated only by (a) loading the file on `SetTodoSession`, or (b) a
  mutation that just succeeded in writing the file. If a write fails, the
  memory copy is not advanced and the error is returned.
- **The main agent is the only writer.** Subagents have `todoread` only
  (`filterMainOnlyTools` in `internal/agent/subagent.go` strips `todowrite`
  and `todo_update` from the subagent tool set). A child reports its outcome
  to its dispatcher; the dispatcher — which owns the plan — records it. This
  is the load-bearing rule that prevents in-process concurrent writers.
- **Mutations are serialized** by an advisory flock
  (`internal/filelock.WithFileLock(todoLockPath(), fn)`) so a second ocode
  instance, a resumed session, or the same agent writing twice cannot
  interleave. The cross-process lock catches contention; the revision
  protocol below catches stale reads.
  **The lock must span the whole read → verify revision → apply → write
  sequence, not just the read.** A lock released between the read and the
  write does not prevent two processes from both observing revision N, both
  passing the staleness check, and both writing N+1 — the second rename
  silently discards the first's items. The in-process mutex does not help;
  cross-process is the only case this lock exists for.
- **Optimistic concurrency via a revision token.** `todoread` returns
  `revision: N`. Every mutation (`todowrite`, `todo_update`) must cite the
  revision it was based on. A stale citation is rejected with the current
  content so the model re-reads and retries. This catches the cross-instance
  and resume cases that the lock alone cannot: the lock serializes writes,
  it does not stop a write based on stale reads.
- **Targeted updates via `todo_update`** (`set_status`, `edit_text`,
  `append`, `insert_after` by item id). A model that only wants to tick one
  box can no longer accidentally rewrite the whole plan. `todowrite` (full
  replace) survives only for creating or deliberately rewriting the list.
- **Destructive full replacements are rejected.** A `todowrite` that drops an
  existing item (particularly a completed one) is refused with a message
  pointing the model at `todo_update`. There is **no override flag** — the
  rejection is unconditional, and the rejection text is the fix.
  **Ids must be assigned *after* the guard runs** (`guardNoDroppedItems`, then
  `inheritTodoIDs`, then `assignTodoIDs`). Stamping positional `t1..tN` on the
  incoming items first makes the guard vacuous — every old id appears to
  survive because the new items were just handed those same ids — so a
  same-length replacement of unrelated items silently destroys completed work.
  `inheritTodoIDs` then lets each id-less new item adopt the id of the existing
  item with the same text, so a legal reorder keeps `t1` pointing at the same
  item and a later `todo_update` cannot mutate the wrong one.
- **Strict parse, never silent reset.** If the file fails to parse, writes
  are refused and the parse error (with the file path) is surfaced. The
  last-good file stays on disk for the user to fix or revert. `TodoState()`
  renders the parse error rather than returning `""` — otherwise the sidebar
  prints "No todo list yet" over a corrupt file, making the render surface the
  one place the failure is silent.
- **Both content shapes parse** (`parseTodoContent`): the canonical
  header+ids form, and legacy headerless raw text, which `restoreTodoState`
  feeds through `SetTodoState` for sessions predating the file store.
  Demanding the header would make `todoread` hard-fail on every call for any
  resumed pre-change session, and would make `baseItems` return nil — skipping
  the destructive guard, so the first `todowrite` would wipe the restored list.
- **Durable writes** (every mutation): lock → re-read → verify revision →
  apply → write to a temp file in the same directory → `fsync` → atomic
  rename → release. A crash mid-write leaves either the old file or the new
  one, never a half-written one.
- **Snapshot capture.** `TodoWriteTool` / `TodoUpdateTool` implement
  `ContextualTool` so the per-agent snapshot store sees the write; on a
  successful write the file is also `Backup`/`RegisterWrite`-ed, so
  `undo_file_change` can restore it by `tool_call_id`.
- **Re-anchor injection is user-role, not system-role** (`injectTodoTail` in
  `internal/agent/todo_inject.go`). The plan is injected on every Step at
  the very tail of the messages slice, after the discovery tail. It is
  wrapped in an `[ocode:todo]` marker so the model reads it as system-origin
  even though it is `role: "user"`. **It must not be `role: "system"`**:
  `collectAndRemoveSystemMessages` hoists every system-role message
  (including tail ones) into the cached `system` field, so a system-role
  block that grows with the plan would rewrite and bust the cached system
  prompt on every turn. The user-role tail rides the uncached suffix and
  coalesces with the current user turn — exactly what we want.
- **Inject only when the list is non-empty and has at least one open item.**
  A finished or absent list injects nothing. This is the cache-stability
  invariant: a no-op turn keeps the messages slice byte-identical so the
  cached prefix survives.
- **Session resume and `/new` semantics.** `SetTodoSession` reloads the file
  from disk (so resume survives a process restart). `ResetTodoState` (called
  by `/new` and `/clear`) clears the in-memory copy **only** — it must not
  delete the file, because the call sites move to a *different* session id,
  and deleting the outgoing session's plan would destroy exactly the state
  this mechanism exists to preserve.

## In-batch task DAG (`id` / `depends_on`)

A parallel batch of `task` tool calls may declare an in-batch DAG instead
of a flat parallel fan-out. Two new optional properties on the `task`
schema:

- `id` — a caller-chosen label, unique within the batch.
- `depends_on` — array of `id`s in the same batch that must complete
  successfully before this dispatch starts.

Omitting both is the common case and **preserves today's behavior
exactly**: a flat parallel fan-out over the same `WaitGroup`. The
scheduler lives in `internal/agent/task_dag.go` and is only consulted
when at least one call in the batch declares `id` or `depends_on`.

**Only `task` / `agent` calls participate** (`isDAGEligibleCall`). `id` is an
ordinary parameter name — `agent_status`, `bash_output`, and `kill_shell` are
all `Parallel()` with a *required* `id` — so parsing `id`/`depends_on` off every
parallel call would route no-task batches through the scheduler and make two
`bash_output` calls on different shells collide on the duplicate-id rule. Other
parallel calls still run; they are simply invisible to the id namespace. Never
widen this filter.

### Validation (rejected as a hard error, no node in the affected component runs)

1. `depends_on` set with empty `id`.
2. Duplicate `id`s.
3. Self-edge (a node names its own `id` in `depends_on`).
4. Unknown `id` in `depends_on`.
5. Cycle in the resolved graph (first back-edge reported with its two endpoints).
6. `depends_on` naming a node that sets `run_in_background`. A background
   dispatch returns a `state: running` placeholder immediately instead of a
   result, so releasing a dependent against it would silently run the child
   without the input the schema promised it.

The error is reported **only on the subagent-dispatch positions**. Other
parallel calls in the batch (a `read`, a `grep`) are dispatched normally — one
bad `depends_on` must not cancel unrelated work that merely shared the batch.

### Scheduling

A wave scheduler driven by an in-degree map. A node does **not**
acquire a concurrency slot until every one of its predecessors has
resolved — the wait happens in the scheduler, **never inside the
dispatched child**. The shared `AgentRunRegistry` limiter is the only
slot acquisition; the scheduler does not introduce a second limiter.

This is load-bearing. Routing `depends_on` into `TaskTool.Execute` and
letting the child block on its predecessors would acquire a slot in
`AcquireForRun` and *then* wait, so a 3-node chain under
`max_concurrent_agents=2` hangs forever. (Same hazard the
`pauseOwnSlotForNestedCall` machinery exists to defuse for nested
dispatches.)

### Dependency output injection

When a node starts, each satisfied predecessor's final result is
prepended to the child's `context`, labelled with the predecessor's
`id`. This reuses the existing `Background Context:` system message
that `TaskTool.Execute` already builds for `params.Context`. A
predecessor's output is truncated through the helpers in
`internal/agent/truncate.go` so a verbose child cannot blow out its
dependents' context.

A child's first system message is therefore:

```
Background Context:
Predecessor "a" output:
  <truncated text of a's final result>

Predecessor "b" output:
  <truncated text of b's final result>

<caller-supplied context, if any>
```

### Failure semantics

If a node fails, its transitive dependents do **not** run. Each
skipped node returns a result of the form
`skipped: dependency "<id>" failed`, naming the **first** failing
predecessor in the chain (not the intermediate skipper). No fallback, no
substitution, no partial execution of a node whose inputs are missing.

Failure propagation is per-**edge**, evaluated in `predecessorBlocked` when a
node's predecessor waits return. It must never be a graph-global abort flag:
with one, a node holding no dependencies (including every non-task call in the
batch) gets skipped or not depending on how fast the first failure lands
relative to the other goroutines reaching the check — a race, not a property of
the graph. Cancellation is the one genuinely batch-global signal.

### Cancellation

The scheduler checks `isCancelled` at every wait point. A node that
becomes ready while cancelled is marked skipped without running. A
node already running when cancellation arrives is allowed to finish
(cooperative); its result still flows to dependents. No goroutine
remains parked on a dependency that will never resolve.

### Interaction with the group bus

Orthogonal, with two lifecycle requirements:

- The bus (`Bus.Start(ctx)` / `Bus.Stop()`) **brackets the entire DAG
  execution**, not a single wave. Late nodes start after early nodes
  have finished, so a per-wave bus would drop shared history and
  leave `groupTracker` with completions it never sees. The reconcile
  hand-off runs after the last node resolves.
- Group-bus agent ids are assigned in **batch order** (`a1`, `a2`, …)
  and stay stable even though execution order now varies. The id is
  never derived from launch order.

Worth stating plainly: nodes on opposite ends of a dependency edge
never run concurrently, so for them the bus degrades from live
collaboration to an append-only log the later node can read. That is
fine — the dependency edge already carries the predecessor's output
directly — but `shared_notes` and `depends_on` solve different
problems and neither substitutes for the other.

### Cache stability

`id` and `depends_on` are static schema properties (one-time tools
change). They travel in call arguments only, never in the tool
description — a description that enumerated live ids would rewrite the
tools array every turn and bust the whole cached prefix.

### Scope

- **In-batch only, v1.** A `depends_on` referencing a `task_id` from
  an earlier turn (or an in-flight background run) is out of scope.
  Recorded in root `TODO.md` as a deferred item.
- **No nested-batch DAGs.** A child's own `task` batch gets the same
  scheduler independently; edges do not cross dispatch boundaries.
- **Not a workflow engine.** No persistence, no retries-on-edge, no
  conditional routing, no `@router`-style branching. If a declarative
  multi-step pipeline is wanted later, that belongs in
  `internal/orchestrator`, built on this.

## Web/Desktop Server: `Handler.mu` is a map lock, never a work lock

`internal/server.Handler.mu` guards the `h.agents` map (and a few small config
fields) and is taken by **every** endpoint — the session list, the run-state
polls behind the desktop dock badge, the config routes, the permission and
question resolvers. Holding it across slow work therefore does not slow one
session down, it freezes all of them: the recurring "a session is stuck and
won't run while another session is running" bug class.

Rules for anything in `internal/server`:

- **Never hold `h.mu` across an LLM call, a `Step`, a compaction, a recap, or
  agent construction.** Agent construction is slow in ways that are easy to
  miss: `tool.InitBuiltinTools` and `LoadExternalTools` touch the filesystem
  and can spawn plugin processes, `agent.NewAgent` may auto-start a local model
  server, and `mcpCache.wait()` blocks until the process-wide MCP enumeration
  finishes (unbounded — an unreachable MCP server makes it slow). Build outside
  the lock with `buildAgentSession`, then insert with `registerAgentSession`,
  which double-checks the map and shuts down the loser of a construction race.
  `ensureAgentSession` / `getOrCreateAgentSession` wrap both; use them.
- **Per-turn work belongs under `agentSession.mu`.** That serializes turns
  within one session and leaves different sessions fully parallel.
- **Lock order is `agentSession.mu` → `h.mu`, never the reverse.** A turn holds
  the session lock and then takes `h.mu` (via the title generator), so taking a
  session lock while holding `h.mu` deadlocks. To scan sessions for a pending
  ask, snapshot the candidates under `h.mu`, release it, then inspect each
  candidate under its own lock — that is what `findPendingSession` does. Reading
  `as.messages` under `h.mu` alone is a data race against a running turn. The
  same order applies to `SessionManager.mu` vs `agentSession.mu` (e.g.
  `EvictIdle` checking for a pending ask before releasing an agent).
- **A tool-call round can pause with more than one unresolved `PERMISSION_ASK`/
  question sentinel at once**, not just as the literal last message — parallel
  tool dispatch runs several calls before the pause check, and each one needing
  approval appends its own sentinel. Never assume "the pending ask" is
  `messages[len(messages)-1]`; scan the whole trailing tool-call round
  (`trailingToolRunStart` in `run_states.go`) by `ToolID` instead, and don't
  re-`Step()` the turn until every ask in that round is resolved — stepping on
  top of one still-raw sentinel feeds the model a malformed tool result, which
  it typically "fixes" by retrying the call and raising a brand new ask (looks
  like the same permission dialog popping back up after being answered).
- **Do not pin an HTTP connection for the length of a turn.** A browser allows
  only six concurrent connections per origin over HTTP/1.1 (the server is plain
  HTTP, so there is no h2 multiplexing), and the SPA already spends some on the
  session mirror and the agent-run stream. A request held open per running turn
  starves the other sessions' requests — the same "stuck session" symptom with a
  client-side cause. `POST /api/chat` and `POST /api/sessions/{id}/message`
  therefore take `"async": true` (the web client always sets it) and reply `202`
  once the turn is dispatched; output reaches the browser over the SSE mirror,
  which is the UI's rendering source anyway. The synchronous path stays for
  non-browser callers (scheduler, Telegram, external API clients).

## Web/Desktop Server: project dirs are per-session, not per-process

`h.workDir` (process cwd at startup, or what desktop boot passes to
`SetWorkDir`) is only the **default** project for a web/desktop server — never
the working directory of a session's work. The rules:

- **Session work follows the session's project root.** `SessionManager` binds
  session id → project root; `buildAgentSession` calls `ag.SetWorkDir` with it
  and takes its LSP manager from `h.lspManagerFor(projectRoot)` — one manager
  per project root, shared across tabs on the same repo. Never route a
  session-scoped operation through `h.workDir`.
- **Project-scoped endpoints take an explicit project param**, validated
  against `allowedProjectRoots()` (workdir + saved projects — the shared trust
  boundary): git uses `?project=`, terminal uses `?project_path=`, file tree
  confines `?path=`, command-context and uploads use `?project=` (uploads
  must land in `<project>/.ocode/uploads` — chat and terminal reference them
  by the relative path `.ocode/uploads/<name>`, which resolves against the
  session's project dir). A new endpoint that binds work to a directory must
  follow the same pattern, not read `h.workDir`.
- **The TUI RC bridge passes its own workdir** to `RegisterExternalSession`
  explicitly; `h.workDir` is only the empty-root fallback.
- **Never call `os.Getwd()` directly inside an `internal/server` handler.** A
  Finder/Dock-launched desktop `.app` starts with process cwd `/`, so a raw
  `os.Getwd()` silently resolves to the filesystem root instead of the actual
  project — this has both broken file reads/writes (`HandleInit` writing
  `AGENTS.md` to `/`) and defeated a path-containment security check
  (`resolveWithinWorkdir`'s "must stay inside the working dir" guard passes
  trivially when the working dir is `/`). Always resolve against `h.workDir`
  (falling back to `os.Getwd()` only if `h.workDir` is itself empty, which in
  practice it never is once `NewHandler`/`SetWorkDir` have run).
- The TUI itself is unaffected by any of this: it drives `internal/agent`
  directly with `m.workDir` and only touches `internal/server` for RC bridge
  types.

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

## Backend / Sync URL Split (2026-09-03) — user-facing migration
`backend_url` (`config.NormalizeBackendURL`) is now **local-dev-only**: empty
(same-origin) or `http://localhost[:port]` / `http://127.0.0.1[:port]`. The
production hub (`https://hub.mercstudio.com`) is no longer accepted there.

The dedicated config/auth sync channel is the new `sync_url` field
(`config.NormalizeSyncURL`): any `https://` origin plus `http://localhost` /
`http://127.0.0.1`; empty falls back to `OCODE_SYNC_URL`, then the production hub
(`sync.DefaultBaseURL` is now `https://hub.mercstudio.com`, up from
`http://localhost:3201`). `internal/sync` resolves via `sync.ResolveBaseURL`;
a one-time diagnostic (`sync.logBaseURLNotice`) is emitted at client
construction so an unconfigured local kakiit dev machine that silently
reaches production after the flip is visibly flagged.

**Migration for existing hub users:** an on-disk `backend_url:
"https://hub.mercstudio.com"` is preserved verbatim in `ocodeconfig.json`
(not silently dropped) and logged as a warning on load, but resolves to
same-origin at runtime. To restore hub connectivity, move the value to
`sync_url` (Settings > Backend > Sync server, or
`PUT /api/config/ocode/sync-url`). Posting the legacy hub to
`PUT /api/config/ocode/backend` returns **400** with a `use sync_url instead`
hint. See `CHANGES.md` `[Unreleased]` for the full list of affected symbols.

When diagnosing a user whose hub routing silently broke after an upgrade, check
`ocodeconfig.json` for a `backend_url: "https://hub.mercstudio.com"` entry and
migrate it to `sync_url`.

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
  in full) **and files inside an active OKF knowledge bundle's `docs/` directory**
  (owned by the knowledge system — `docs/index.md` is injected as the
  `[ocode:knowledge]` TOC and `knowledge_lookup` retrieves any concept doc on
  demand) is a `Kind:"md"` Doc whose `Text` is an LLM summary (small model when
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
