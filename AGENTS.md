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
- `typesafe` (TypeSafe AI System One, model `jev-latest`) is **decision-only**:
  `POST /v1/systemone` answers typed choice/score/noul questions and never
  generates text. `NewClient` builds a `TypesafeClient` whose `Chat` fails
  with `ErrTypesafeDecisionOnly`; its only consumers are the auto-permission
  judge (`consultPermissionModel` → `askPermissionModelTypesafe`), the
  auto-continue triage judge (`Ocode.AutoContinueModel = "typesafe/<model>"` →
  `runAutoContinueJudgeTypesafe` in `internal/agent/autocontinue_typesafe.go`,
  a Decide() call answering a typed continue/end choice over the transcript
  tail; the advisory `reason` becomes decisive for `awaiting_user`, which
  vetoes a `continue` verdict so a reply that asks the user a question or
  requests feedback is never auto-resumed), the discovery relevance judge
  (`judgeDiscoveryCandidates` in
  `internal/agent/discovery_typesafe.go`, one noul question per
  embedder-selected skill/doc/MCP candidate), and the doc_search relevance
  judge (`judgeDocSearchResults` in `internal/agent/doc_search_typesafe.go`,
  one noul question per returned knowledge doc; wired only for the context
  subagent's doc tools). All send the request as structured state. Thresholds
  scale with the consequence of getting the decision wrong: the high-stakes
  auto-permission judge uses `permissions.auto.min_confidence` (default 0.85),
  and drops to `autoJudgeOpaqueMinConfidenceDefault` (0.75) when the judge's own
  concern answer is `truncated_or_unknown` — a request whose effects it could
  not establish (an undefined-variable command head, an unreadable script). The
  opaque default applies only when `min_confidence` is unset; an explicitly
  configured value always governs, so the relaxation never tightens or loosens
  the user's own bar;
  the two *relevance* judges (discovery + doc_search) share the lenient
  `relevanceJudgeMinConfidenceDefault` (0.5) because a relevance veto only
  hides a retrieval result and the product rule is "even slight relevancy
  should be presented, only a different scope is skipped"; the auto-continue
  triage uses its own lower floor `autoContinueMinConfidenceDefault` (0.6) —
  auto-continue is bounded and reversible, and Jev's `confidence` is a
  distribution-shape statistic that runs below `probabilities[choice]`, so the
  permission floor rejected legitimate mid-task resumes. Never route TypeSafe
  through the chat, compaction, small-model, or interpreter-effects paths.
- Every request to an `opencode*` provider must carry `X-Opencode-Session`, an
  opaque ID stable for one conversation (Zen/Go pin the conversation to one
  upstream for prompt caching; some Go models 400 without it). All transports
  capable of serving `opencode*` providers call `setOpencodeSessionHeader`.
  The identity source is `Agent.SetSessionID` for CLI/server sessions,
  `Agent.SetOpenCodeSessionID` for TUI sessions, or one lazily resolved
  fallback on the owning agent. Compaction, replacement, and side-query
  clients inherit the same identity. New transports must call the helper too.
- Every request to `openrouter` must carry `x-session-id` with the same
  stable conversation ID (OpenRouter's explicit sticky-routing key: pins
  model+provider from turn one so prompt caching engages immediately; also
  groups the session in OpenRouter logs). `setOpenRouterAttributionHeaders`
  sets it alongside the `HTTP-Referer`/`X-Title` attribution headers.
  Because the conversation ID is the session id itself, `/reset-id`
  (`session.RekeyForDir` + `Agent.RekeySession`/`RekeyOpenCodeSession`, and
  `snapshot.Store.RekeySession` for the undo journal) re-keys a chat to a
  fresh `ses_…` id while preserving the transcript. Any new session-id-keyed
  map or journal must be moved by that path too, or `/reset-id` leaves it
  stranded under the deleted id.

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
    bad edit. It **refuses** (returns a conflict, leaving the file untouched) if
    any affected file was changed outside ocode's tracked write tools since the
    original call — e.g. the user edited it in an external editor — rather than
    overwriting those changes. (Every file
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
toggled from the TUI permission-mode click cycle, `/sandbox`, or the web
sidebar permission pill / `/sandbox` command. On the web/desktop server the
live mode is **per chat session** (persisted in the session's metadata;
`PUT /api/permissions/mode` and `/yolo` require a `session_id`), so toggling
one chat never changes another. It is **write-integrity confinement only** —
it does NOT protect secrets or prevent exfiltration.

What it confines:
- Only the agent **shell tool** (`bash`) is wrapped. The interactive PTY
  terminal (`handler_terminal.go`) and the web `!shell` path run unsandboxed.
- Filesystem **writes** fail at the OS level unless the target is under a
  writable root: workspace/`extra_allowed_paths`, the opencode shared data dir
  (`~/.local/share/opencode`, including per-project `project/**` state —
  sessions, snapshots, md-summaries, memory), language dependency caches
  (npm/pip/cargo/go/maven/gradle), `~/.claude`, the fixed global git-ignore
  files (`paths.GitIgnoreFiles`: `$XDG_CONFIG_HOME/git/ignore` else
  `~/.config/git/ignore`, plus `~/.gitignore_global` and `~/.gitignore` —
  exact files only, never `~/.config`/`$HOME`, never an arbitrary
  `core.excludesFile`), and temp dirs
  (`/tmp`, `/var/tmp`, `os.TempDir()`, plus on macOS the uid-owned
  `/var/folders/*/*/{T,C}` confstr dirs — found by ownership, not `$TMPDIR`,
  so `mktemp`/Python/Node/clang caches work even when ocode runs without
  `TMPDIR`, e.g. under launchd; see `pathscope.DarwinUserDirs`).
- **Reads and exec stay global, network egress is open** — toolchains need
  them. A sandboxed `python`/`node` can still read `.env`/`~/.ssh`/`auth.json`
  and POST them anywhere.

What still asks (permission layer, not the OS):
- `auth.json` (read or write) → Ask
- ocode config dir (writes only) → Ask
- `~/.ssh`, `.env` (read or write) → Ask
- secret material reads in general (`isSecretMaterialPath`: `.netrc`,
  `.npmrc`, `.pypirc`, SSH private-key filenames, `.pem`/`.key`/`.p12`/`.pfx`/
  `.secrets`, `.aws/`) → Ask
- **repo-metadata dirs (`.git/`, `.github/workflows/`) → Ask on WRITE only.**
  Reading/listing them (`ls .git/`, `cat .git/config`) auto-allows in sandbox
  exactly as in normal mode; a write/delete (a planted `.git/hooks/*` or
  workflow) still asks, because it stays inside the workdir where the OS
  write-wall is blind. `sandboxSensitivePath` splits them from
  `isSecretMaterialPath` via `isRepoMetadataPath`, and
  `sandboxSensitiveTargets` returns a per-target write map (fail-closed:
  unknown commands mark their path args as writes) so
  `truncate`/`dd`/`chmod`-style forms cannot slip through as "reads".
- danger-`rm` heuristics → Ask
- destructive git forms (`git stash`/`checkout`/`reset`/`clean`/`restore`/
  `switch`, plus force-flagged `git push`/`pull`) → Ask — the OS write-wall is
  blind to a repo mutation that stays inside the allowed workdir (history
  rewrite, branch switch, stash create/drop, untracked removal). Read-only
  forms (`git stash list`/`show`) are unaffected and still auto-allow. The
  check is applied to **every constituent of a compound command**, not just the
  whole line: `cd repo && git stash` asks, because
  `IsHarmfulBashCommand`/`isHarmfulForceCommand` only recognize a command whose
  first word is `git` (the sandbox gate parses first; see `Decide` in
  `internal/agent/permissions.go`).
- explicit user bash deny rules (`permissions.bash.prefixes`, written by
  `/ban add` or the permission dialog) → Deny (hard, never re-considered by the
  auto-judge), enforced in sandbox too. A `git stash` ban targets the mutating
  family (push/pop/apply/drop/clear, bare stash); the read-only inspection
  forms (`list`/`show`) keep auto-allowing (`matchBashPrefixRule`).
- the Ask → auto-judge hand-off (`IsHarmfulRequest` in `agent.go`) is per
  constituent as well: any harmful fragment sends the whole line to a human,
  never to the Jev judge. It judges the **whole line from `req.Args`**, not
  `Request.Command` — Decide fills the latter with only the first segment that
  needed a human, so `curl … && git reset --hard` would otherwise slip past
  (see `docs/gotchas/auto-permission-harmful-segment-masked-by-earlier-ask.md`).
  The same gate runs on the Deny → auto-judge branch.
- wrappers are peeled before those checks (`effectiveCommandWords`,
  `permissions_wrappers.go`): launcher prefixes (`env`, `command`, `nohup`,
  `exec`, `time`, `nice`, `timeout`, `xargs`, `stdbuf`, `sudo`, `doas`, …),
  path-qualified binaries (`/usr/bin/git`), and shell re-exec / `eval` bodies
  (`bash -c 'git stash'`) are judged as the command they really run — for
  both the harmful gate and `/ban` prefix denies. A command whose binary is a
  shell expansion (`$g stash`, `$(which git) stash`) → Ask in sandbox
  (`sandbox.opaque_command`), since nothing static can resolve it.
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

Sandbox **persists** like any other mode: `persistPermissions` calls
`SavePermissionModeSwitch`, which writes `permissions.mode` verbatim — the
former sandbox→normal clamp is gone, so a restart comes back in sandbox. Cron
jobs are unaffected: each resolves its own per-job mode independently
(`resolveCronPermissionMode`, blank → `normal`), so a persisted sandbox default
never leaks into scheduled runs.
Fail-closed on macOS/Linux: if the mode is sandbox but no backend is available,
the command errors before starting (no silent unsandboxed execution).

## Context Loading

- `CLAUDE.md`, `AGENTS.md`, `OCODE.md`, and `.cursorrules` (plus every
  `.opencode/rules/*.md`) are loaded at session start by
  `internal/agent/context.go::LoadContext`. **`CLAUDE.md` takes
  priority over `AGENTS.md`**: when both are present, only
  `CLAUDE.md` is loaded; when only `AGENTS.md` is present, it is
  loaded as a fallback.
- **The always-on context files resolve from the session's project root**
  (`root`), not the server process cwd. A server hosts many projects and the
  desktop `.app` boots with cwd `/`, so a cwd-relative read injected the wrong
  project's rules into a session's cached system prompt. `root == ""` keeps
  the legacy cwd-relative behavior for callers that never threaded a root
  through.
- If a context file is tracked by git AND has unstaged modifications, the
  committed `HEAD` version is used instead of the working-tree copy. This
  keeps the base prompt stable across edits; commit the changes to make
  them effective. A line is logged to stderr when this swap occurs.
- When reading files, show only the relevant excerpts needed for the
  current task — do not dump entire files.

## Knowledge System (OKF Bundle)
The project supports an optional **OKF v0.1 knowledge bundle** at `docs/` — a
curated set of markdown files with YAML frontmatter (type, title, description,
tags, timestamp, status). When active, the agent gains the `knowledge_lookup`
tool (and the `context` sub-agent) for retrieval; nothing from the bundle —
not even `docs/index.md` — is injected into the system prompt (see "OKF docs
access scope" below).

**Activation** (two gates, both required):
1. `DocPromptEnabled` flag — toggled via `/docs on` (persisted in config).
2. Bundle marker — `docs/index.md` must have `okf_version: "0.1"`
   frontmatter, created only by `/docs init`.

### Agents

| Agent | Role | Tools |
|-------|------|-------|
| `context` | Knowledge curator/retriever and **sole automated writer** to the bundle. Answers why/decision/playbook questions; verifies doc claims against code before writing; prefers updating existing docs over near-duplicates; deprecates rather than deletes. Uses the configured context model if set and enabled, else the small model, else the main model (see `/context-model`). When the bundle is active, also handles codebase exploration (where/how/what) — subsumes `explore` — with full explore toolkit plus doc tools; `explore` is hidden from the schema. Priority: `doc_search` first, then `doc_get`, then code tools; parallel `doc_search` + code search in the same batch is allowed for mixed questions. | `grep`, `rgrep`, `glob`, `read`, `list`, `lsp`, `bash`, `webfetch`, `websearch`, `doc_search`, `doc_get`, `doc_write`, `doc_deprecate` |
| `explore` | Code-level exploration — where/how/what in source; general codebase research, not pinned to the knowledge bundle (hidden when `context` is active) | `read`, `glob`, `grep`, `rgrep`, `list`, `lsp`, `bash`, `webfetch`, `websearch` |

Guidance in the `DocPromptEnabled` prompt fragment: try `knowledge_lookup` first
for why/decision/playbook questions; when the bundle is active use `context` (which subsumes `explore`) for code-level or mixed (why+where) questions — `explore` is hidden — with knowledge-first priority: `doc_search` first, `doc_get` as needed, then code tools, parallel batch allowed; when the bundle is inactive, use `explore` for code-level questions as before.

**Sole-automated-writer invariant:** no agent path outside the `context`
subagent may write to the bundle — the main agent's tool set never includes
`doc_write`/`doc_deprecate`. Deletion happens only via `/docs cleanup`
(per-file confirmation required).

**OKF docs access scope:** OKF documentation (the `docs/` bundle, including
`index.md`) is NOT loaded into the main agent's system context. The main
agent only accesses OKF docs through the `context` sub-agent (via
`knowledge_lookup` or `task` with `agent=context`). The `context` sub-agent
discovers and reads docs dynamically through `doc_search`/`doc_get` and
manages the bundle through `doc_write`/`doc_deprecate`. When the bundle is
active, ALL non-exploration work (why/decision/playbook questions, doc
updates, mixed why+where questions) must route through the `context`
sub-agent; pure code exploration (no doc involvement) may use the explore
toolkit directly.

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
- **Spawn goroutines through `crashguard.Go(fn)`** (or `defer
  crashguard.Recover()` first thing in a `go func(args…)`) in `internal/tui`
  and `internal/agent` — never a bare `go func()`. Bubble Tea only recovers
  panics in `Update`/`View` and its own cmd goroutines; a panic in a raw
  goroutine kills the process before it can disable mouse tracking and leave
  the alt-screen, so the user's shell fills with `[<35;20;10M` garbage on
  every mouse move. `tui.Run` registers the terminal-reset hook via
  `installCrashTerminalReset`; the crash still terminates the process and
  prints the original stack. Sites that intentionally `recover()` to turn a
  panic into an error (stream goroutine, `ask.go`, `subagent.go`, scheduler)
  keep their own recover — that is a different contract.

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

The opt-in host desktop-control tool is documented in `docs/computer-use.md`.

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
  - `/btw`/`/by-the-way` IS instant: it runs an independent side-query loop
    on its own child agent + client (`Agent.AskLoopAsync`), so it never touches
    the main turn's `OnDelta`/`OnUsage` callbacks and runs concurrently with an
    in-flight stream. A mid-stream snapshot may carry an assistant tool_call
    with no result yet; `repairToolCallSequence` synthesises a placeholder
    before send so the request stays valid.
  - **Queued by design (mutates persistent state mid-stream, so it must
    wait for the current turn to end):** `/doc-sync`,
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
  "verified correct". The result handed to the verifier is bounded
  (`truncateVerifierResult`, head+tail with a marker the prompt understands) so
  an oversized report does not needlessly slow the one call. **Do not add a
  hardcoded total-call deadline** — the verifier is bounded like every other LLM
  call (client pre-stream timeout + stream idle watchdog); `verifierContext`
  applies a deadline only when `Agent.RequestTimeout` is explicitly set, and when
  that deadline fires it is reported as a **timeout** (`TimedOut`, a subset of
  `CheckFailed`): the deficiency says `timed out after <d>`, status text says
  `Contract: timed out`, and the web tooltip says so. A malformed/unparseable
  verdict, LLM error, or timeout is a **check failure** (`CheckFailed`), never a
  silent "satisfied".
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
  verdict is recorded on the `AgentRun` as a `ContractOutcome`
  (`Checked` / `Satisfied` / `CheckFailed` / `Deficiency`) via
  `SetContractVerdict` and surfaced in `agent_status` / `task_status`, the TUI
  agent strip + detail view, and the web Agents tab (DTO field `contract`).
- **`CheckFailed` is not "not satisfied".** When the verifier itself fails
  (timeout / LLM error / empty or unparseable response) the child's result was
  never judged; `Satisfied` is false only because no judgement exists. Every
  consumer must branch on `CheckFailed` **before** treating `!Satisfied` as a
  contract failure: the tool result is prefixed `Output contract NOT verified`
  (not `not met`), status text says `Contract NOT verified`, and the TUI/web
  badge is the muted `contract ?` (not the red `contract ✗`). Otherwise a slow
  or broken verifier reads as a failing child — this was the dominant cause of
  spurious `contract ✗` badges (verifier timeouts).
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

## Sub-agent transcripts: `OnSubAgentMessage` + child sessions
A dispatched child's messages must NEVER reach the parent's `OnMessage` /
`OnDelta`. Both hosts hang their transcript on `OnMessage` — the server's
`wireLivePersist` appends every message to the parent session file, the TUI
appends to `m.messages` (whose `raw` is replayed to the parent LLM next turn)
— so forwarding there wrote background children's turns into the parent as
if the parent had done the work (and misordered tool results against the
parent's own `agent_status` call). `internal/agent/subagent.go`
(`attachRunTranscript`) instead routes each child message to
`run.appendTranscript`, `Agent.OnSubAgentMessage(run, msg)`, and
`TaskTool.persistChild`, which saves the run under its own child session
`run.SessionID` = `<parent>_child_<agent>_<ts>` (title `Child: <agent>`;
metadata `parent_session_id`, `agent_name`, `run_id`, `status` =
`running` → `done`/`failed`). The persister comes from
`Agent.SetChildSessionPersistence` and MUST be a live async save (server:
`session.SaveAsyncForDir(projectRoot, …)`; TUI: `session.SaveAsync`) — it
fires per streamed child message. Live sub-agent UI comes from the run
transcript (`/api/agents/runs/stream`, TUI agent strip), not the parent chat.

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
- **A pending ask must be recoverable from live session state, not only from
  the persisted transcript.** The paused tool result is normally saved as a
  `PERMISSION_ASK:`/question sentinel, but that save can fail or conflict, or
  the pause can post-date the last write — and a live SSE `permission` frame
  sent while the tab was untracked is gone. The session then answers every send
  with `ErrPermissionPending` while the browser has no dialog to resolve it.
  `GET /api/sessions/{id}/state` therefore carries an optional `pending_asks`
  object read from the live agent transcript (`livePendingAsks` in
  `handler_session_state.go`). Three client triggers hydrate the dialog from
  it: web reconcile (`reconcileOpenSessions`), a 409 send (`useChat`'s
  `hydratePendingAsks`), and — needed because the web client always sends
  `async:true`, so a refused send resolves 202 and the refusal arrives later as
  a `turn_error`/`error` frame instead of a 409 — that frame's handler
  (`scheduleHydratePendingAsks` in `sessionEvents.ts`). Do not "fix" the async
  path by returning 409 pre-dispatch: the async contract deliberately
  persist-then-202s and leaves the refused message queued for retry
  (`TestAsyncTurnRefusedWhilePermissionPending`). Any new surface that consumes
  asks should keep this fallback. See
  `docs/gotchas/pending-ask-recovery-live-session-state.md`.
- **Reading a live session's `as.messages` from an HTTP handler uses a
  non-blocking `as.mu.TryLock()`, never `Lock()`.** `runTurn` holds `as.mu` for
  the whole turn (minutes), and endpoints like `GET /api/sessions/{id}/state`
  are polled by the browser and the turn watchdog — a blocking read parks those
  requests behind the turn and re-creates the "stuck session" symptom from the
  server side. When the try-lock fails, skip the live read and fall back to
  persisted state; a turn holding the lock cannot yet be paused on an ask (the
  pause and the unlock happen together when the step returns). Follow
  `livePendingAsks`.
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
- **One project must never block another: bound every subprocess and fan out
  shared loops.** `h.mu` is process-wide, so anything held across slow work
  freezes unrelated projects and sessions — including a background emitter
  goroutine, which has no request context to cancel it. Local git was the
  violation: `gitStatusForDir` (`handler_git.go`) ran `exec.Command("git", …)`
  with no deadline (unlike the remote path's `remoteExecTimeout`), and the
  git-status emitter (`emitters.go` `watchEmittersLoop`) computed every viewed
  project **sequentially on one goroutine** — so one repo whose git wedged
  stalled `git_status` for every other project. It now runs every probe under
  `gitStatusTimeout` via `exec.CommandContext`, and fans due projects out
  through `forEachGitStatusConcurrently`, yielding results as they arrive on
  the loop goroutine (shared state like `lastGit` stays single-writer). Any new
  per-project work driven from a shared loop must follow that shape: a
  per-item deadline plus per-item fan-out, never a bare `exec.Command`.

## Git subprocesses: `gitexec`, never a bare `exec.Command("git", …)`

Every git child ocode spawns is a potential `.git/index.lock` contender, because
`git status` and `git diff` refresh the index as an *optional* locked side
effect. ocode polls git constantly — the git-status emitter for every viewed
project every 10s, the web Git tab, the TUI file-tree badge ticker — so a bare
`exec.Command("git", …)` makes ocode the background process git(1) warns about:
the user's own `git add` then fails with `fatal: Unable to create
'<repo>/.git/index.lock': File exists. Another git process seems to be running
in this repository, or the lock file may be stale`. A probe SIGKILLed by its
deadline (`gitStatusTimeout`) can also strand that lock.

- **Every lock-capable git subprocess sets `cmd.Env = gitexec.Env()`**
  (`internal/gitexec`), which adds `GIT_OPTIONAL_LOCKS=0` (git's
  `--no-optional-locks`, de-duplicating an inherited value). It is safe on
  mutations as well as probes: only *optional* locks are skipped, so `git add` /
  `commit` / `stash` / `apply --cached` still take the real lock and still fail
  loudly. Commands that never take a lock (`rev-parse`, `log`, `show`) don't
  need it.
- **Index-writing mutations are wrapped in `gitexec.WithLockRetry`**, which
  retries only when `gitexec.LockHeld(err)` (git's "Unable to create
  …index.lock" / "Another git process seems to be running") with a bounded
  ~900ms schedule, then keeps git's stderr and appends the retry count so a lock
  still present reads as stale rather than live. A failed lock acquisition means
  git never started the operation, so re-running is safe.
- In `internal/server` and `internal/tui` this is centralised in `gitRunInDir` /
  `runGit`, and the git executable is a package var (`gitBinary`) so tests can
  substitute a stub. Remote projects get the same treatment in the command
  string `remoteGitCommand` builds (a leading `GIT_OPTIONAL_LOCKS=0`, valid in
  both remote shells) plus `remoteGitMutation` for the retried mutations.
  Regression suites: `internal/gitexec/gitexec_test.go`,
  `internal/server/git_lock_retry_test.go`,
  `internal/server/remote_git_lock_retry_test.go`,
  `internal/tui/git_lock_retry_test.go`.

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
  boundary): git uses `?project=`, terminal uses `?project_path=` (plus
  `&host=` for a registered remote project, which spawns ssh/wsl.exe instead
  of a local shell), file tree
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

### Remote projects: chat and terminals run on the host; files/git/forwards stay per-request

A sidebar remote (SSH/WSL) project's **chat/agent/session traffic and its
terminal websocket + terminal HTTP calls** are reverse-proxied to an
`ocode serve --remote` process **on that host**, reached through
`/api/remote/{host}/api/{rest...}` (`internal/server/handler_remote_proxy.go`,
route registered in `server.registerRoutes`). `Handler.remoteHosts`
(`internal/server/remote_hosts.go`) owns one lazily-connected
`remote.RemoteWorkspace` per host, shared by every remote project on that host
and closed at shutdown; the proxy builder is
`remote.NewAPIProxy`/`remote.InjectAuth` (`internal/remote/proxy.go`), which
`internal/desktop/proxy.go` also uses. Terminal pty's are therefore children of
the **host's** server, not the local one: a remote project's shell survives a
laptop sleep or a desktop restart (24 h detach TTL in `--remote` mode; 30 min
local). The Files tab, git, `!` commands, and port forwards keep their existing
per-request ssh/wsl.exe paths — they are not proxied.

Terminal routing for a project with a `host`:

```
/api/remote/{host}/api/terminal/ws?project_path=…
/api/remote/{host}/api/terminal/{id}/history   (GET)
/api/remote/{host}/api/terminal/{id}           (DELETE)
/api/remote/{host}/api/terminal/processes
/api/remote/{host}/api/terminal                (GET — live named sessions, for reattach)
```

Rules:

- **Only saved project hosts may be proxied.** `{host}` must be the `Host` of
  at least one saved project (the same trust boundary `remoteWorkFor`
  enforces); a non-matching host is rejected before any connect, and the route
  never consults the local `allowedProjectRoots` path allowlist.
- **The remote token never reaches the browser.** The proxy strips the local
  `token` query param and `Authorization` header and injects `Bearer
  <ServeState.Token>` server-side.
- **The proxy must restore the browser's websocket subprotocol on a 101.** In
  `--remote` mode the host expects `ocode.bearer.<remoteToken>` as the offered
  `Sec-WebSocket-Protocol` and echoes it on the 101. Forwarding that header
  unchanged both breaks the handshake (the browser offered a different value)
  and leaks the remote token. `remote.InjectAuth` records the browser's offer
  on the request context and `ModifyResponse` restores exactly that value on a
  101 (deleting the header when the browser offered nothing). Any new
  Upgrade-capable proxy path must preserve this.
- **`~` is expanded only by the server owning that `$HOME`**
  (`projects.ExpandHome`, called from `Store.Add`, `HandleAddProject`, and
  `HandleChat`). A remote project's saved path stays verbatim locally and is
  expanded by its host; a local `~/…` project is expanded locally.
- **A remote failure blocks the turn** (502 `{error, stage:
  "remote-connect"}`); never fall back to running a remote project's agent
  locally.
- **The SPA resolves the host per session, not per active project**
  (`resolveSessionHost` in `web/src/hooks/useSessionHost.ts`, from the tab's
  project binding). New session-scoped calls must pass that host — plus an
  `X-Ocode-Project` header when they carry a path — or they hit the wrong
  machine. Local calls pass no host and keep byte-identical URLs.
- **Version mismatch is reused, never auto-restarted.** `EnsureRemoteServer`
  reuses an alive, healthy but version-mismatched server and flags
  `ServeState.Outdated` (a dead/unhealthy one is still replaced). The sidebar
  shows an amber outdated marker and an explicit **Restart**; `POST
  /api/remote/{host}/restart` kills the pid, drops the registry entry, brings
  up a fresh server at the local version, and re-registers the host's saved
  projects. It is unguarded: running turns and terminals are not drained, so
  the SPA reconnects with its normal backoff and a dead shell shows the
  existing "shell exited" state. `GET /api/remote/{host}/status` reports what
  the registry knows without connecting; `POST /api/remote/{host}/connect` is
  the explicit connect.
- **Terminal reattach keys are host-qualified.** `terminalPersistence.ts`'s
  `projectTerminalsKey(path, host)` is `<host>::<path>` for remote projects and
  the bare path for local ones, so a local and a remote project at the same
  path cannot share terminal tabs. `GET /api/terminal?project_path=…` backs the
  sidebar's reattach list when localStorage is empty.


## Web/Desktop Context gauge: provider-reported first, estimate as fallback
The web/desktop Context gauge (`TUIStatus.context_current_tokens`) and the
`/context` summary (`current_tokens`) carry the backend's provider-reported
context occupancy whenever one exists. A character-count estimate over the
session transcript is used ONLY as a last-resort fallback, never in preference
to a provider reading.

- **Source of truth:** `Agent.LastInputTokens()`, an atomic set from
  `resp.Usage` inside `Step` (so it covers every provider, not just the
  streaming-usage ones). The bridged TUI session keeps using its own live
  `ContextCurrentTokens`.
- **Resolution chain (identical in both entry points):** live TUI value →
  `Agent.LastInputTokens()` → `Agent.CompactedContextTokens()` (a `/compact`
  clears `LastInputTokens`, so the agent records a post-splice estimate) →
  `estimateContextFromTranscript` / `estimateContextFromMessages` (chars/4 over
  the persisted transcript, for a restored / idle-evicted session with no live
  agent in this process).
- **One resolution, two entry points:** `Handler.applySessionContext` (status
  snapshots) and `Handler.HandleSessionContext` (the `/context` endpoint) MUST
  walk the SAME chain, and the report's `contextbudget.Input.ContextTokens`
  override is fed the same value so the report's Context row and the summary
  cannot disagree. Label the override's `ContextSource` by origin: a provider
  reading (or its post-compaction tail estimate) is `"actual"`; the chars/4
  transcript fallback is `"estimated"` — never present an estimate as "actual".
- **Why the fallback exists:** the earlier rule forbade any estimate, so a
  session whose agent had been idle-evicted or restored-after-restart rendered
  the gauge as "unknown" until its next turn — a confusing regression on every
  tab switch. Provider numbers still always win, and the fallback is the same
  `CurrentContextEstimate` heuristic the TUI already uses for compaction, so the
  web and TUI agree rather than diverging.
- **API shape:** `GET /api/sessions/:id/context` returns `current_tokens`
  (renamed from `estimated_tokens`), alongside `max_tokens` / `model` / the
  optional `report`.


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
- `projects.json` / `project_groups.json` — web sidebar project list
- `tabs.json` — open session tabs per project for the web/desktop UI
  (`GET/PUT /api/tabs`). Server-side, never `localStorage`: localStorage is
  per-origin, so a shared URL or a different port would otherwise open with
  zero tabs. Every server process on the machine shares this one file, so the
  store merges under a cross-process lock (`internal/tabs`): a `PUT` replaces
  only the projects it names, an empty tab list deletes a project, and
  projects absent from the body are preserved. Never turn this back into a
  whole-map replace — that silently drops another window/process's projects.

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
- **Discovery candidates pass through the TypeSafe relevance judge before
  attaching.** `Session.Select` ranks and returns the not-yet-attached
  candidates *without* mutating the sticky set; `RunDiscovery` then judges them
  when TypeSafe is connected (one `noul` question per candidate in a single
  `Decide` call) and seeds only those whose noul meets the lenient shared
  relevance floor (`relevanceJudgeMinConfidenceDefault` = 0.5; the rubric asks
  for same-scope relevancy, not necessity — see
  `internal/agent/relevance_typesafe.go`). It never changes
  `renderDiscoveryContext` — the names-index stays a function of the doc set;
  only which ids become attached changes. Not connected, a judge transport/
  decode error, or a missing candidate answer all fail open (seed everything):
  the judge may only veto, never attach fewer docs because of a failure. The
  judge client is resolved once per discovery state (`discoveryState.judge`,
  `sync.Once`) so the factory's no-key refusal log fires once, not every turn.
  See `internal/agent/discovery_typesafe.go` and
  `docs/concepts/discovery-typesafe-judge.md`.
- **doc_search results pass through the same TypeSafe relevance judge before
  the context subagent sees them.** The context subagent's doc tools are built
  via `newDocToolsWithJudge` with `Agent.docSearchJudge()` (nil when TypeSafe is
  not connected), so `doc_search` asks Jev one `noul` question per returned
  document ("is this doc in the same scope as the query?") and hides
  different-scope docs; slight relevancy is kept. Filtering happens before
  `get_top` body inlining. Fail-open: no judge, a judge error, or a missing
  answer shows every result. `DocSearchJudge` is the injection seam
  (`internal/agent/doc_tools.go`); the judge lives in
  `internal/agent/doc_search_typesafe.go` and shares the mechanics (Decide,
  side-usage, lenient floor, per-candidate fail-open, debug lines) with
  discovery via `judgeRelevanceQuestions` in
  `internal/agent/relevance_typesafe.go`.
- **Role determines caching, not array position — because of the hoist.** The
  Anthropic builder (`chatAnthropic` → `collectAndRemoveSystemMessages` in
  `client.go`) pulls **every `system`-role message — including tail ones — into
  the top-level `system` field**, which carries `cache_control`. So a
  `system`-role message appended at the tail is NOT in the uncached suffix; it
  rides the **cached** system block. Consequence: any tail `system` injection
  whose content **varies per turn** (e.g. growing) rewrites and busts the whole
  cached system prompt. Every volatile tail injector is therefore user-role:
  `injectNotesTail`, `injectTodoTail`, `injectDirMDTail`, `injectLSPDelta`.
- **LSP diagnostics are message-level, never system-role** (`internal/agent/
  lsp_inject.go`). A single-file write tool (`write`, `edit`, `multiedit`,
  `replace_lines`, `format`) re-syncs its file with the language server and
  waits up to 2s for a fresh publish; non-empty diagnostics are appended to
  that tool's result (transcript history, byte-stable once persisted). Files
  whose diagnostics changed elsewhere (package-level fallout, user edits
  between turns) are rendered once as a `[ocode:lsp]` user-role tail block by
  `injectLSPDelta` before each model call; the agent keeps a per-URI
  fingerprint of what it last reported, so an unchanged store adds nothing.
  LSP server problems (binary missing) stay out of the prompt entirely: they
  ride `Message.Notice` via `NoticedError` and show only in the transcript UI.
- **Per-turn UI state and transcript notices are user-role too.** The TUI file
  selection (`[ocode:selection]`) is appended by `PrepareMessages` at the
  tail as user-role, not by `BasePromptMessages` (which is system-only and
  takes no per-turn arguments). Background agent/process completion notices
  (`[ocode:event]`) and "add file to context" blocks (`[ocode:context]`) are
  persisted as user-role transcript messages with an explicit marker line so
  the model still reads them as out-of-band. Never persist a `system`-role
  transcript message for anything that can happen more than once per session.
- **Subdirectory `CLAUDE.md`/`AGENTS.md`/`OCODE.md` are lazy and volatile**
  (`internal/agent/dir_docs.go`). After a path-touching tool succeeds
  (`read`, `write`, `edit`, `multiedit`, `replace_lines`, `format`, `lsp`,
  `ast`, `glob`, `list`, `grep`, and every `edits[].path` of
  `multi_file_edit`; `apply_patch` is not covered), the agent walks from the
  touched path up to the project root, exclusive, and queues each unseen
  directory's docs for one user-role `[ocode:discovery]` tail block before the
  next model call. A directory is seen once per session; compaction
  (`runCompact`) clears the seen set because the injected blocks were never
  persisted and are gone with the splice.
- **Anthropic breakpoints (`applyAnthropicConversationBreakpoints` in
  `client.go`):** tools (last tool), system (single block), and the last two
  user-role messages (tool results are user-role). The marker on the most
  recently appended turn is what lets the next request read the whole prior
  transcript from cache; the old placement on the FIRST user message cached
  nothing after turn one. Do not add a fifth marker — Anthropic allows four.
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
  (owned by the knowledge system — nothing from it is injected into the prompt;
  `knowledge_lookup` / the `context` sub-agent retrieve any concept doc on
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
  Platform: darwin
  Today's date: <resolved at session start>
</env>
```

A session bound to a **remote (SSH/WSL) project** gets one extra line —
`Project host: <[user@]host|wsl:distro> (remote project — …)` — right after
the git-repo lines (`Agent.SetProjectHost`, from `Handler.projectHostFor`).
That line was added because a locally-built agent for a remote project saw a
remote project root beside the local machine's config/session/skill/runtime
paths. Remote chat/agent traffic is now reverse-proxied to the host's
`ocode serve --remote` (see "Remote projects: chat runs on the host" above),
so the answering agent runs on the host; the line still marks the residual
case of a locally-built agent bound to a remote project (e.g. a direct
`/api/chat` call that does not go through the host prefix). Local projects
emit no such line, so their prompt is byte-identical.

There is no `Git branch` line: the git branch is not resolved or injected.
