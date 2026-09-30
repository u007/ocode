# Agent Instructions — ocode

This file is the canonical, always-on briefing for any LLM agent (ocode, Claude
Code, Cursor, etc.) working in this repo. ocode loads it silently and
unconditionally at session start via `internal/agent/context.go` — see
"Context Loading" below. Keep the content here focused on **cross-cutting
rules that affect more than one file**: recurring bug classes, architectural
constraints, and process rules. Feature descriptions belong in `README.md` or
the `skills/ocode-*` catalog, not here.

This repo deliberately has no root `AGENTS.md`: ocode loads only one of the
two when both exist, so a second file would never be seen. Do not recreate it.

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
  with `ErrTypesafeDecisionOnly`. Its only consumers are the auto-permission
  judge (`consultPermissionModel` → `askPermissionModelTypesafe`), the
  auto-continue triage judge (`Ocode.AutoContinueModel = "typesafe/<model>"` →
  `runAutoContinueJudgeTypesafe`), the discovery relevance judge
  (`judgeDiscoveryCandidates`) and the doc_search relevance judge
  (`judgeDocSearchResults`; wired only for the context subagent's doc tools).
  All send the request as structured state. Each judge has its own confidence
  floor, scaled to the cost of a wrong decision, and an explicitly configured
  `permissions.auto.min_confidence` always governs the opaque relaxation.
  Jev's `confidence` is a distribution-shape statistic that runs below
  `probabilities[choice]`, so never reuse the permission floor for a
  lower-stakes judge. Per-judge detail: `docs/concepts/auto-permission-enforced-categories.md`,
  `docs/concepts/server-auto-continue.md`,
  `docs/concepts/discovery-typesafe-judge.md`,
  `docs/concepts/doc-search-relevance-judge.md`. Never route TypeSafe through the chat,
  compaction, small-model, or interpreter-effects paths.
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
  Because the conversation ID is the session id itself, `/reset-id` re-keys a
  chat to a fresh `ses_…` id (`docs/concepts/session-rekey-reset-id.md`). Any
  new session-id-keyed map or journal must be moved by that path too, or
  `/reset-id` leaves it stranded under the deleted id.

## Git Worktrees
The default location for `git worktree` checkouts is `.worktrees/` in the
project root. The directory is gitignored — worktree contents are
developer-local state and must never be committed.
```bash
git worktree add .worktrees/feature-branch feature-branch
```

A fresh worktree will not build until you copy the two **gitignored embed
inputs** out of the main checkout. `internal/agent/models-snapshot.json` and
`internal/browse/cdp/htr-assets.zip` are `//go:embed` targets that are
deliberately untracked, so the worktree has no copy at all and the build
fails with `pattern models-snapshot.json: no matching files found` — which names
the file but not the reason (it is a build input, not a source file):

```bash
git worktree add .worktrees/feature-branch feature-branch
cp internal/agent/models-snapshot.json .worktrees/feature-branch/internal/agent/
cp internal/browse/cdp/htr-assets.zip .worktrees/feature-branch/internal/browse/cdp/
```

Two testing notes for a worktree (or any checkout) used as a regression
baseline: `go test -race ./...` is a real gate here, not a formality, and a
mutation-testing result is only meaningful if the mutant **compiles** — a mutant
that only breaks the build is `INVALID`, never `CAUGHT`. See
`docs/gotchas/mutation-check-mutants-must-compile.md`. When comparing failures
against a pristine baseline, note that a session-local sandbox may deny
`/dev/ptmx`, which fails every pty-dependent test (e.g. `TestTerminalWS*`) at
pristine HEAD too — that is an environment limit, not a regression from your
diff.

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

`sandbox` is the fourth permission mode (besides `normal`/`yolo`/`locked`). It is
**write-integrity confinement only** — it does NOT protect secrets or prevent
exfiltration. The live mode is per chat session on the web/desktop server.

- Only the agent **shell tool** (`bash`) is wrapped. The interactive PTY terminal
  and the web `!shell` path run unsandboxed. Reads, exec and network stay open, so
  a sandboxed `python`/`node` can still read `.env`/`~/.ssh`/`auth.json` and POST
  them anywhere; the static Ask checks catch direct commands, not interpreter bodies.
- Static checks apply to **every constituent of a compound command**
  (`cd repo && git stash` asks). The Ask → auto-judge hand-off judges the **whole
  line from `req.Args`**, never `Request.Command`
  (`docs/gotchas/auto-permission-harmful-segment-masked-by-earlier-ask.md`).
- Wrappers are peeled before those checks (`effectiveCommandWords`); a command whose
  binary is a shell expansion → Ask (`sandbox.opaque_command`).
- Explicit `/ban` deny rules are a hard Deny, never re-considered by the judge.
- Writes to permission-defining files and loopback `/api/permissions*` → Ask in all
  modes (self-escalation guard).
- Fail-closed on macOS/Linux: sandbox mode with no backend → the command errors
  before starting.

Writable roots, the full Ask list, platform matrix and persistence:
`docs/concepts/sandbox-permission-mode.md`.

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
  current task — do not dump entire files. Whole-file dumps waste the
  context window and obscure the signal.

## Knowledge System (OKF Bundle)
An optional **OKF v0.1 bundle** at `docs/` (markdown + YAML frontmatter). It is
active only when both gates hold: `DocPromptEnabled` (`/docs on`) and `docs/index.md`
carries `okf_version: "0.1"` (created by `/docs init`). Agents, tools, `/docs`
subcommands and the maintenance worker: `docs/concepts/okf-knowledge-system.md`.

- **Nothing from the bundle is injected into the system prompt**, not even
  `docs/index.md`. The main agent reaches it only through the `context` sub-agent
  (`knowledge_lookup` or `task` with `agent=context`). When the bundle is active,
  route ALL non-exploration work (why/decision/playbook, doc updates, mixed
  why+where) through `context`; it also subsumes `explore`, which is then hidden.
- **Sole-automated-writer invariant:** no agent path outside `context` may write
  to the bundle; the main agent never gets `doc_write`/`doc_deprecate`. Deletion
  only via `/docs cleanup` (per-file confirmation). Mutations go through
  `knowledge.WithBundleLock` (`docs/.okf.lock`).
- **Code line anchors in bundle pages drift silently.** Adding or removing any line
  ABOVE a cited `file.go:123` invalidates it. When a change adds/removes lines in a
  file the bundle cites, re-derive EVERY anchor on that page (ranges too — check both
  endpoints) by extracting each `\w+\.go:\d+` and printing the real source line;
  never trust a sub-agent's "anchors corrected" list. Cancel a context sub-agent that
  wanders and do the correction directly.

## Authoring knowledge and skill files: read the target first

`docs/**` bundle pages and `skills/*/SKILL.md` are shared, concurrently-edited
knowledge artifacts — unlike source files, they are often untracked while being
written, so an accidental overwrite has **no git history to recover from**.

- **Read the exact target path before writing it.** A directory listing
  (`ls skills/*/`) is not proof a skill is absent: another session can create
  the file between your listing and your write. This has already happened — a
  `skills/ocode-remote-ssh/SKILL.md` was replaced mid-authoring by a weaker
  draft and survived only because the write tool's undo existed.
- **Prefer the `edit` tool over a whole-file `write` for any path that may
  already exist.** An anchored replace fails loudly when the anchor is missing;
  `write` silently clobbers.
- When you do overwrite by mistake, `undo_file_change` restores the file — but
  the **staged index is separate**: re-stage the restored file, or a later commit
  will land the bad version. Prefer a **path-limited** commit
  (`git commit <path>`) to correct one file without sweeping a shared tree.

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
Terminal mouse capture is **global per frame** — `tea.View.MouseMode` is one
flag for the whole screen, so capture makes tabs/menus/buttons clickable but
**blocks native terminal text selection**, and it cannot be scoped to a region.
Never disable `MouseMode` to regain native selection — that kills every click
target. **Keep capture ON and implement selection in-app** on every content
surface: press records the anchor, motion extends it, release copies if the
anchor moved, else falls through to the click handler. Hover effects need
`MouseModeAllMotion` and cached hit-test maps (the handler fires on every
cursor move). The full recipe (`selectionState`, `applySelectionHighlight`,
`copyToClipboard` — never call `clipboard.WriteAll` from a view) is
`skills/ocode-tui/SKILL.md` §5.

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
`internal/tui/changes_model.go` lists every file the session (main agent + sub-agents)
added or edited, with per-file diffs and file/block-level undo. It derives from the
snapshot store (`internal/snapshot.Store`) plus a pre/post-stat bash detection hook, not
from git. See `docs/changes-tab.md`. The opt-in desktop-control tool is documented in
`docs/computer-use.md`.

## User Interaction
- TUI supports `/commands` and `!shell`. Use `ctrl+x` for leader keys and `ctrl+p` for
  the palette; avoid raw shortcuts likely to conflict with Warp/Ghostty/iTerm2 and prefer
  `ctrl+x` sequences for non-essential toggles. Sessions are saved and resumed automatically.
- **Slash commands entered while streaming or compacting are queued** (`m.queuedItems`)
  and drained in the `agentStreamDoneMsg` / `compactFinishedMsg` handlers; only
  `/exit`/`/quit`/`/q` and "instant" commands bypass. **The `isInstantCmd` chain in
  `handleCommand` (`internal/tui/model.go`) is the sole list of instant commands** — add a
  new synchronous local command there, not here. Commands that mutate persistent state
  mid-stream (`/doc-sync`, `/agents limit <n>`) stay queued by design. Details:
  `docs/concepts/tui-slash-command-queuing.md`.

## Task Output Contracts (`expected_output`)
An optional `expected_output` on a `task` dispatch is verified (shape, not truth)
against the child's final result and retried once in place. Mechanism, contract
resolution and reporting: `docs/concepts/task-output-contracts.md`. Invariants:

- **The retry lives inside `runSyncDispatch`, not via `resume_task_id`** — routing
  it through `TaskTool.Execute` rebuilds the child and trips `subagentDispatchLimit`.
- **`CheckFailed` is not "not satisfied".** A verifier timeout/LLM error/unparseable
  verdict means the result was never judged; every consumer branches on
  `CheckFailed` before treating `!Satisfied` as a failure (`contract ?`, not `✗`).
- **No hardcoded total-call deadline** on the verifier; no built-in agent declares a
  default contract; the badge never means "verified correct".

## Persistent todo plan (`todowrite` / `todoread` / `todo_update`)
A per-session plan at `.ocode/todo/<session-id>.md`; format, revision protocol and
injection details: `docs/concepts/persistent-todo-plan.md`. Invariants:

- **The main agent is the only writer**; subagents have `todoread` only
  (`filterMainOnlyTools`). The in-memory copy is a cache, never a second source of
  truth (`TodoState()` never touches disk; advance it only after a successful write).
- **Mutations hold the flock across the whole read → verify revision → apply →
  write sequence** and must cite the revision they were based on; stale → rejected.
- **Destructive full replacements are rejected with no override flag.** Order is
  `guardNoDroppedItems` → `inheritTodoIDs` → `assignTodoIDs`; assigning ids first
  makes the guard vacuous.
- **Strict parse, never silent reset:** a file that fails to parse refuses writes
  and surfaces the error; legacy headerless content must still parse.
- **Re-anchor injection is user-role, never system-role** (`injectTodoTail`), and only
  when an open item exists, so the cached prefix stays byte-stable.
- `ResetTodoState` clears memory only; it must not delete the outgoing session's file.

## Sub-agent transcripts: `OnSubAgentMessage` + child sessions
A dispatched child's messages must NEVER reach the parent's `OnMessage`/`OnDelta` —
both hosts persist/replay those as the parent's own turns. `attachRunTranscript`
routes them to `run.appendTranscript`, `Agent.OnSubAgentMessage` and
`TaskTool.persistChild` (child session `<parent>_child_<agent>_<ts>`). The persister
from `Agent.SetChildSessionPersistence` MUST be a live async save. Details:
`docs/concepts/subagent-transcripts-child-sessions.md`.

## In-batch task DAG (`id` / `depends_on`)
Parallel `task` batches may declare `id`/`depends_on`; omitting both preserves the
flat fan-out exactly. Scheduler: `internal/agent/task_dag.go`. Spec and semantics:
`docs/concepts/task-dag.md`. Invariants:

- **Only `task`/`agent` calls participate** (`isDAGEligibleCall`); `id` is an ordinary
  param on `agent_status`/`bash_output`/`kill_shell`. Never widen this filter.
- **Waiting happens in the scheduler, never inside the dispatched child** — a child
  that acquires a slot then waits deadlocks a chain under `max_concurrent_agents`.
- **Failure propagation is per-edge** (`predecessorBlocked`), never a graph-global
  abort flag (that makes unrelated nodes race). Cancellation is the only batch-global
  signal. No fallback or partial execution of a node whose inputs are missing.
- Validation errors are reported only on the subagent-dispatch positions, not on
  unrelated parallel calls; `depends_on` on a `run_in_background` node is invalid.
- The group bus brackets the WHOLE DAG; bus agent ids are assigned in batch order.
- `id`/`depends_on` are static schema props: never enumerate live ids in a tool
  description (busts the cached prefix). v1 is in-batch only, no nested DAGs.

## Web/Desktop Server: `Handler.mu` is a map lock, never a work lock
`Handler.mu` guards the `h.agents` map and small config fields and is taken by every
endpoint, so holding it across slow work freezes all sessions ("a session is stuck
while another runs"). Rationale and cases: `docs/concepts/web-server-locking-and-liveness-rules.md`.

- **Never hold `h.mu` across an LLM call, `Step`, compaction, recap, or agent
  construction.** Build outside the lock with `buildAgentSession`, insert with
  `registerAgentSession`; use `ensureAgentSession`/`getOrCreateAgentSession`.
- **Per-turn work belongs under `agentSession.mu`.** Lock order is
  `agentSession.mu` → `h.mu`, never the reverse (same for `SessionManager.mu`); to scan
  sessions, snapshot candidates under `h.mu`, release, then lock each (`findPendingSession`).
- **A tool-call round can pause with several unresolved ask sentinels.** Scan the whole
  trailing round (`trailingToolRunStart`) by `ToolID`; don't re-`Step()` until every ask
  is resolved.
- **A pending ask must be recoverable from live session state** (`pending_asks` via
  `livePendingAsks`). Don't "fix" the async path by returning 409 pre-dispatch
  (`TestAsyncTurnRefusedWhilePermissionPending`).
- **Live `as.messages` reads from an HTTP handler use `as.mu.TryLock()`, never `Lock()`**;
  on failure fall back to persisted state. Same for any fan-out over `h.agents` from a
  request or poll path (`PendingPermissionAsks`, `applyLimitsToLiveSessions`): a blocking
  `as.mu.Lock()` there freezes the Settings save and the desktop UI for the whole turn.
- **Don't pin an HTTP connection for a turn.** `POST /api/chat` and
  `/api/sessions/{id}/message` take `"async": true` and reply `202`; output rides SSE.
- **One project must never block another:** per-item deadline (`exec.CommandContext`)
  plus per-item fan-out for any shared loop; never a bare `exec.Command`.
- **A session-scoped SSE event is a two-part change** (`sessionScopedEvents` and, if it is
  replayable, `liveFrameEvents`); momentary events stay out of `liveFrameEvents`.
- **Every path holding `turnActive=true` publishes `turn_heartbeat`**
  (`startTurnHeartbeat`).
- **Push emitters cover only VIEWED projects** (`EventBus.ViewedProjects()`); a badge
  needing live data for other projects must poll.
- **Routes live in `Server.registerRoutes()` behind one of four middleware wrappers**;
  pick one deliberately, and thread `?host=` on session/project-scoped routes.

## Git subprocesses: `gitexec`, never a bare `exec.Command("git", …)`

ocode polls git constantly (git-status emitter, web Git tab, TUI badge ticker),
and `git status`/`git diff` take an *optional* `.git/index.lock`, so a bare
`exec.Command("git", …)` makes ocode the background process that makes the
user's own `git add` fail with "Unable to create '<repo>/.git/index.lock': File
exists". Full analysis:
`docs/gotchas/git-index-lock-contention.md`.

- **Every lock-capable git subprocess sets `cmd.Env = gitexec.Env()`**
  (`internal/gitexec`, adds `GIT_OPTIONAL_LOCKS=0`). Safe on mutations too —
  only *optional* locks are skipped. Commands that never lock (`rev-parse`,
  `log`, `show`) don't need it.
- **Index-writing mutations are wrapped in `gitexec.WithLockRetry`** (retries
  only on `gitexec.LockHeld(err)`, bounded ~900ms; re-running is safe because a
  failed lock acquisition means git never started the operation).
- In `internal/server` and `internal/tui` this is centralised in `gitRunInDir` /
  `runGit`, with the executable a package var (`gitBinary`) so tests can stub
  it. Remote projects get the same treatment via `remoteGitCommand` (leading
  `GIT_OPTIONAL_LOCKS=0`) and `remoteGitMutation`.

## Web/Desktop Server: project dirs are per-session, not per-process
`h.workDir` is only the **default** project for a web/desktop server, never a
session's working directory. Details, endpoint params and remote-project routing:
`docs/concepts/web-server-project-scoping.md`.

- **Session work follows the session's project root** (`SessionManager` →
  `buildAgentSession` → `ag.SetWorkDir`, `h.lspManagerFor(projectRoot)`). Never route a
  session-scoped operation through `h.workDir`; a new directory-bound endpoint takes an
  explicit project param validated against `allowedProjectRoots()`.
- **Never call `os.Getwd()` inside an `internal/server` handler** — a Finder/Dock-launched
  desktop app starts with cwd `/`, which broke file writes and made
  `resolveWithinWorkdir`'s containment check pass trivially. Resolve against `h.workDir`.
- **Remote (SSH/WSL) project chat, sessions and terminals are proxied to the host's
  `ocode serve --remote`**; files, git, `!` commands and port forwards stay per-request.
  Only saved project hosts may be proxied. The remote token never reaches the browser,
  and the proxy must restore the browser's websocket subprotocol on a 101. A remote
  failure blocks the turn (502 `remote-connect`) — never fall back to a local agent.
  `~` is expanded only by the server owning that `$HOME` (`projects.ExpandHome`). The SPA
  resolves the host per session (`resolveSessionHost`). Version mismatch is reused, never
  auto-restarted.
- **The open-terminal tab list is server state** (`GET/PUT /api/terminal-tabs`,
  `internal/termtabs`): keys are opaque (never `filepath.Clean`); the PUT is a MERGE
  (emit an explicit empty entry for a project whose last tab closed); closing a tab is a
  PERMANENT discard (`DELETE /api/terminal/{id}` also removes disk history).

## Web/Desktop Context gauge: provider-reported first, estimate as fallback
The gauge (`TUIStatus.context_current_tokens`) and `/context` (`current_tokens`) use
the provider-reported occupancy whenever one exists; a chars/4 transcript estimate is
a last-resort fallback only. `Handler.applySessionContext` and
`Handler.HandleSessionContext` MUST walk the SAME chain (live TUI value →
`Agent.LastInputTokens()` → `Agent.CompactedContextTokens()` → transcript estimate) and
label `ContextSource` by origin — `"actual"` for a provider reading, `"estimated"` for
the fallback, never the reverse. Details: `docs/concepts/web-context-gauge-resolution.md`.

## Plugins: ocode and Claude Code formats share one namespace

`internal/plugins` loads ocode plugins (`plugin.json`) and Claude Code plugins
(`.claude-plugin/plugin.json`, including Claude Code's own installs from
`~/.claude/plugins/installed_plugins.json`). Details: `docs/plugins.md`.

- **One plugin per name, ocode first.** `LoadAllPluginsForProject` dedupes
  before any enable filtering, so an ocode plugin shadows a Claude Code
  install of the same name even when the ocode copy is disabled.
- **ocode's config decides enablement; Claude Code's is only the default.**
  Never write Claude Code's settings or delete from its plugin store —
  `plugins.Remove` refuses paths under it.
- **Listing vs loading.** UI lists and toggles must use
  `LoadAllPluginsForProject` (it includes plugins that are off by default);
  runtime paths use `LoadPluginsForProject(enabled, root)`.
- **SessionStart hook output is cached per process** (`SessionStartContext`)
  because it lands in the cached prompt prefix; re-running per turn would
  both spawn subprocesses and risk busting the prefix. Invalidate on plugin
  install/remove, never per turn.

## Data Storage
All persistent state lives under `internal/paths.GlobalDataDir()` (macOS
`~/.local/share/opencode`, Linux `$XDG_DATA_HOME/opencode`, Windows
`%LOCALAPPDATA%\opencode`); layout and sub-directories:
`docs/concepts/data-storage-layout.md`.

- Resolve the logs dir via `paths.LogsDir()`, never `filepath.Join(GlobalDataDir(), "logs")`.
- `tabs.json` (and `terminals.json`) are shared by every server process, so the store
  merges under a cross-process lock: a `PUT` replaces only the projects it names. Never
  turn this back into a whole-map replace.
- Sessions are keyed by a SHA-256 slug of the git repo root; the TUI's `m.workDir` (not
  `os.Getwd()`) is the source of truth for project resolution.

## Backend / Sync URL Split (2026-09-03)
`backend_url` is local-dev-only (empty or `http://localhost|127.0.0.1[:port]`); the
production hub belongs in `sync_url`. A legacy `backend_url: "https://hub.mercstudio.com"`
is preserved on disk but resolves to same-origin — when diagnosing a user whose hub
routing broke after an upgrade, check `ocodeconfig.json` and migrate it to `sync_url`.
Full migration notes: `docs/concepts/backend-sync-url-split.md`.

## Prompt Cache Stability
Anthropic prompt caching reads one linear prefix in a **fixed order: `tools` →
`system` → `messages`** (breakpoints in `internal/agent/client.go`), so **any change
to the tools array invalidates the `system` block and the message prefix too**. Full
rules and rationale: `docs/concepts/prompt-cache-stability.md`. For any change that
touches tools or the base prompt:

- **Never put per-turn-varying content in `tools` or `system`.** `LoadContext` must be a
  function of stable, preload-time state (config flags), not a per-turn result.
- **`GetToolDefinitions` must emit a deterministic order** (`sort.Strings` over names);
  `a.tools` is a map and unsorted iteration busts the cache every request.
- **Tool sets that grow are grow-only/sticky within a session** (discovery `Session`), so
  a no-new-attachment turn sends a byte-identical tools array.
- **Every discovery attach path is judged** (`runDiscovery` and `discover_more` both run
  `judgeDiscoveryCandidates` before `Seed`; the judge may only veto, failures fail open),
  and **discovery must never fail open the MCP tool gate** (`discoveryAllows` consults no
  warm/corpus state; a cold turn starts with zero MCP tools; a failed `Session.Select`
  attaches nothing and never sets `disco.enabled = false`). `doc_search` results pass
  through the same judge before the context subagent sees them. See
  `docs/concepts/discovery-typesafe-judge.md`,
  `docs/concepts/discovery-mcp-tool-gating.md`,
  `docs/concepts/doc-search-relevance-judge.md`.
- **Role determines caching, not array position.** `collectAndRemoveSystemMessages`
  hoists EVERY `system`-role message (tail ones too) into the cached `system` field, so a
  tail `system` injection that varies per turn busts the whole system cache. Volatile tail
  injectors are therefore **user-role**: `injectNotesTail`, `injectTodoTail`,
  `injectDirMDTail`, `injectLSPDelta`, the TUI `[ocode:selection]`, and the
  `[ocode:event]`/`[ocode:context]` transcript notices. Split by volatility
  (`injectDiscoveryContext` is the model): stable content → `system`; volatile content →
  `user`, wrapped in an `[ocode:discovery]`-style marker. **Never persist a `system`-role
  transcript message for anything that can happen more than once per session.**
- **LSP diagnostics are message-level, never system-role** (`lsp_inject.go`): appended to
  the writing tool's result, or one `[ocode:lsp]` user-role tail block via `injectLSPDelta`;
  LSP server problems stay out of the prompt (`Message.Notice`).
- **Subdirectory `CLAUDE.md`/`AGENTS.md`/`OCODE.md` are lazy and volatile**
  (`dir_docs.go`): queued once per directory per session into a user-role
  `[ocode:discovery]` tail block; compaction clears the seen set.
- **Anthropic breakpoints** (`applyAnthropicConversationBreakpoints`): last tool, system,
  and the last two user-role messages. Do not add a fifth marker — Anthropic allows four.
- **Project `*.md` files are part of the discovery corpus** (`md_discovery.go`) except the
  always-on briefing set and files inside an active OKF bundle's `docs/`; summaries are
  cached in `.ocode/md-summaries.json`.

## Environment Prompt
The LLM receives an `<env>...</env>` block at session start from
`internal/agent/prompt.go` (working directory, git-repo flag, platform, date; a
remote-project session adds a `Project host:` line; there is no git-branch line).
Parse the block at runtime rather than assuming a shape. Details:
`docs/concepts/environment-prompt.md`.
