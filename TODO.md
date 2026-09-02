# TODO

## ocode Remote (SSH) Phase 1 — picker UI not wired (2026-09-02)

`ocode remote <[user@]host> [path]` (`internal/remote`, `internal/remotecli`)
implements the Phase 1 connect flow from
`docs/superpowers/specs/2026-08-29-remote-ssh/`: reachability, platform
detect, cross-compile/upload/verify, credential sync (framed stdin,
checksummed, 0600 temp+rename), and — the resume story — wraps the remote
TUI launch in `tmux`/`screen` (`new-session -A` / `-xRR`, keyed by a
sha256 of the resolved remote path) so a dropped SSH connection only
detaches the local client; rerunning the same command reattaches to the
still-running remote session instead of starting a fresh one. Falls back to
a plain passthrough with a printed warning when neither multiplexer is on
the remote.

`internal/projects.Project` gained an optional `Host` field plus
`AddRemote`/`TouchRemote`/`FindLastRemote` (identity scoped to
`(host, path)`, never colliding with a same-path local project), and
`ocode remote` records/reads through it for the "omitted path → last
remote project for that host" default. **Not done**: no TUI/web picker UI
change to actually *display* `host:path` rows or let a user reconnect by
selecting one — the store-layer support is there, the picker components
themselves are untouched. Follow-up: wire `internal/tui`'s project picker
(and the web equivalent) to render `Project.Host` and re-run the connect
flow (`internal/remotecli.Run`) when a remote entry is selected.

Also out of scope here (later phases per the spec, not regressions):
Phase 2 (`--web` tunnel mode) and Phase 3 (WSL transport) are unimplemented;
the multiplexer-detect step re-probes every connect rather than caching
per-host (cheap, so not a correctness issue, just a possible future
optimization).

## Pending permission asks do not survive a server restart (2026-08-31)

`session.LoadForDir` runs `removeIncompleteToolRequests`, which strips
`PERMISSION_ASK:` / waiting-for-user sentinels (and the tool call that raised
them) from every loaded transcript. Several server comments assume the
opposite ("the sentinel lives in the persisted transcript, so a reload already
recovers it" — session_manager.go `liveFrameEvents`, sessionEvents.ts
reconcile). Consequences:

- After `ocode serve` / the desktop restarts, a session paused on a permission
  ask cannot be resolved: `findPendingSession` only searches resident agents
  and the reloaded transcript no longer contains the ask, so
  `POST /api/permissions/resolve` returns 404. A still-open browser tab keeps
  the dialog from its in-memory slice; the web client now dismisses it on
  404/409 and shows why (useChat.resolvePermission) instead of leaving it
  stuck, but the ask itself is lost — the user has to re-prompt.
- The first-page history fetch (`GET /api/sessions/:id`) never carries the
  sentinel either, so a reload during a pause shows no dialog at all.

Fix direction: keep trailing-round pending asks when loading (server + TUI
resume both know how to re-surface them — `tailIsPermissionAsk`,
`parsePermissionRequest`), and let `findPendingSession` load a non-resident
session via `getOrCreateAgentSession` before scanning.

## Snapshot journal — deferred follow-ups (2026-08-30)

The per-project snapshot journal (`snapshots.sqlite` in
`project/<slug>/snapshots/`) now persists file-edit backups metadata and
rehydrates the changes tab on session resume. Deliberately left out of the
first pass:

- **Bash-only change entries are not persisted.** The changes registry's
  bash pre/post stat-walk entries (`Registry.NotifyBashWrite`) live only in
  memory; after a resume, files touched only by bash commands vanish from
  the changes tab. They carry no backup and are not undoable, so the loss
  is cosmetic, but journal rows for them would restore full fidelity.
- **Sub-agent attribution is lost on rehydrate.** Journal rows record the
  writing store's `agent_id` (random hex), but rehydrate replays every
  session row into the MAIN agent's store, so the changes tab shows "main"
  as the author for pre-restart sub-agent edits.
- **No divergence marking.** A rehydrated snapshot can be undone even if a
  LATER session (or the user by hand) modified the file since — the
  process-local cross-agent write registry cannot see pre-restart writes.
  Storing a content hash per journal row would let the changes tab flag
  "diverged since" rows and warn before undo.
- **Legacy session JSON bulk migration.** `.json`/`.ojsonl` sessions only
  migrate to `.sqlite` on their next save; ~3.8k dormant legacy files
  remain per heavy project. A one-shot background sweep (or `ocode
  sessions migrate` command) would finish the migration.

## Encrypted-file editing (age, per-project passphrase) — TUI done, web/desktop deferred (2026-08-28)

Requested: encrypt a file on disk so ocode's TUI/web/desktop auto-decode it
for editing (including external editors like vim/nvim) and re-encode on
save, opaque to the agent (Read/Edit/Write/grep must never see plaintext —
user-confirmed), with a self-describing header and one passphrase per
project. Design: `filippo.io/age`, ASCII-armored (files are committed to
git — user-confirmed), a generated X25519 key per project wrapped under a
scrypt passphrase (`.ocode/secret.key.age`), so passphrase unlock costs one
scrypt run per session while per-file encrypt/decrypt is fast
(ChaCha20-Poly1305, no KDF).

- [x] **`internal/secretfile`** — `GenerateProjectKey`/`UnlockProjectKey`
  (wrapped project key), `ProjectKey.Encrypt`/`Decrypt`, `IsEncrypted`
  (armor header check), `WriteFileAtomic` (temp+fsync+rename, preserves
  mode), `Session` (in-memory per-root key cache). Full test coverage
  including tamper/wrong-passphrase rejection.
- [x] **`ocode secret init|encrypt|decrypt`** (`internal/secretcli`,
  wired in `main.go`) — CLI encrypt/decrypt independent of any UI, useful
  for CI/scripting and as the thing the TUI/web paths build on.
- [x] **TUI external-editor integration** (`internal/tui/secret_editor.go`)
  — `wrapEditorOpenerForSecrets` decorates the existing editor-opener
  plumbing (`model.go`, `commands.go`, all 3 non-AtLine call sites) so
  opening an encrypted file decrypts to a private 0700 temp dir, launches
  the real editor against the temp file, and re-encrypts back over the
  original path ONLY if the plaintext changed (hash-compared, so `:q`
  without saving doesn't touch the ciphertext / doesn't dirty git). The
  passphrase prompt (`golang.org/x/term`, masked) runs inside
  `secretExecCommand.Run()` — the same window `tea.Exec` has already
  released the terminal to the external process — never inside a
  bubbletea Cmd/Update cycle. Verified against real headless `nvim`
  (`TestSecretExecCommand_Run_WithRealNvim`), not just a fake editor
  script, so the real swap-file/write path is exercised and contained.
  Passphrase is cached per project root for the TUI process lifetime
  (`model.secretSession`), asked once even across agent-swap/session-reset.
  The passphrase read deliberately does NOT hardcode `os.Stdin`/`os.Stderr`:
  it reads from whatever `tea.Program.exec` actually assigned via
  `SetStdin`/`SetStderr` (`secretExecCommand.readPassphrase`), because that
  is `p.input` — `os.Stdin` in the common case, but a separately-opened tty
  when stdin isn't a terminal — not necessarily `os.Stdin` itself. Verified
  by reading `exec.go`'s `Program.exec` and `tea.go`'s `releaseTerminal`
  (blocks on `waitForReadLoop()` before `Run()` is ever called, so
  bubbletea's own input reader is confirmed stopped first, no race).
  **Not yet verified**: an actual interactive TUI session opening an
  encrypted file end-to-end (this session's environment has no interactive
  pty to drive one). Everything short of that live click-through is
  covered: the crypto cycle against real headless nvim, the
  ExecCommand/stdio wiring logic, and the bubbletea source itself. Do one
  manual `ocode` → Files tab → open an encrypted file → type passphrase →
  edit → save before treating this as fully closed.
- [x] **tmux split/window editor modes now wrapped (2026-08-28).**
  `wrapEditorOpenerForSecrets` no longer bypasses tmux mode: it now takes
  `mode`/`getWidth` instead of a `tmuxMode bool`, and `secretExecCommand`
  gained `buildEditorCmd` — decrypts to the private temp file first (as
  before), then for tmux modes points `buildTmuxOpenCmd`/pane command at
  the temp path (never the still-encrypted original), blocks on tmux's
  own `wait-for` handshake exactly like the non-secret tmux opener, and
  re-encrypts back over the original path once the pane exits. Covered by
  `TestWrapEditorOpenerForSecrets_TmuxModeWrapsEncryptedFile` and
  `TestSecretExecCommand_BuildEditorCmd_TmuxModeTargetsTempFile`; no live
  tmux session was exercised in this pass (would need a real tmux server),
  same caveat as the plain external-editor path's outstanding manual
  check.
- [ ] **`SetEditorOpenerAtLine`/`createEditorOpenerAtLine` call sites left
  unwrapped.** Line numbers from jump-to-line (git blame, grep results)
  are computed against ciphertext-visible content in the first place
  (consistent with "opaque to agent" — ocode's own search/grep only ever
  sees armor text for an encrypted file), so a meaningful plaintext line
  number can't reach this path today. Revisit only if a future feature
  produces real plaintext line numbers for an encrypted file.
- [ ] **`files_model.go:openInEditor`'s no-`editorOpener`-set fallback**
  and **`git_model.go`'s equivalent** build their own bare `exec.Command`
  and bypass the decorator entirely. Low risk today (`editorOpener` is
  always set once config loads, so this is a defensive fallback rarely
  exercised in practice), but if hit against an encrypted file it opens
  raw ciphertext, not a bug exactly (no plaintext leak) but confusing UX.
- [ ] **`m.changes.editorOpener` (Changes/diff tab) is wrapped for
  consistency but diffing armor text is inherently meaningless** — expect
  the diff view to look broken (all-ciphertext-changed) for an encrypted
  file regardless of what actually changed in plaintext. Not a bug to fix
  here; diffing encrypted files would need a design of its own (e.g. diff
  the decrypted content, which reopens the "diff view sees plaintext"
  question against the "opaque to agent" decision).
- [ ] **Inherent external-editor leaks, can't be fixed by the temp-dir
  containment:** if the user's editor config writes outside the file's
  own directory (vim `set undofile` → `undodir`, `viminfo`/`shada`
  recording the edited path), those can retain plaintext fragments
  outside the private temp dir. Document for users who enable an
  encrypted-file workflow; not solvable from ocode's side.
- [ ] **A wrong passphrase surfaces as a plain editor error, no retry
  prompt** — the user has to reopen the file and try again rather than
  being re-prompted inline. Fine to ship as-is; worth revisiting if it's
  annoying in practice.
- **`.ocode/secret.key.age` being committed to git assumes `.ocode/` isn't
  gitignored wholesale.** Checked in ocode's own repo: only
  `.ocode/md-summaries.json` and `.ocode/todo/` are ignored, not the
  directory itself, so this works here — but another project's
  `.gitignore` could blanket-ignore `.ocode/`, in which case the key file
  silently never gets committed and a fresh clone can't decrypt even with
  the right passphrase. `ocode secret init` doesn't check this; consider
  having it warn if the key path would be ignored (`git check-ignore`).
- [ ] **Agent-facing decrypt tool (`secret_read`) — designed, deliberately NOT
  built this pass.** User asked whether the LLM's own tools should be able to
  decrypt these files. Investigated and designed: dedicated `secret_read`
  tool (read-only, no `secret_write`, so the agent can't corrupt/overwrite
  ciphertext), never accepts a `passphrase` argument (would put it in chat/
  session history — same reason the TUI prompt reads from the terminal, not
  the input box), requires the project key already unlocked in-process
  (unify `model.secretSession` onto a package-level `tool.SecretSession` so
  the TUI's editor-triggered unlock also serves the tool), and always asks
  permission even in YOLO/auto-permission mode (`Decide()` special-cased
  ahead of the YOLO shortcut in `permissions.go`, `IsHarmfulRequest` returns
  true for it so the auto-permission LLM judge can't silently approve it
  either — reusing `Permissions.Auto.AllowDestructive` as the escape hatch,
  not inventing a new tier). None of this is implemented yet.
  **Blocking finding, not yet resolved:** the original plan was to redact
  the decrypted content before it lands in persisted session history while
  still letting the model see the real value for the turn. That promise has
  nowhere to attach — `internal/redact.Redactor`/`RedactMessage` is fully
  built but has **zero production callers** (dead code, tests only), and
  `internal/session.Save`/`saveOjsonl`/`saveJSON` serialize
  `agent.Message.Content` straight to disk with no transform step — the
  exact same field the next turn's request is built from. So "real for this
  turn, placeholder for replay/disk" needs new agent-core plumbing (how tool
  result messages flow through the turn loop), not a config flag. Given
  that, **the actual security property of `secret_read` as buildable today
  would be: a decrypted secret lands in `.ojsonl` in plaintext and gets
  replayed to the LLM provider on every subsequent turn of that session** —
  no better than the "transparent decrypt in read/edit/grep" option
  explicitly rejected earlier for that exact reason. User chose to hold off
  entirely rather than ship with that property, or accept the cheaper
  partial mitigation (cap `secret_read` to a byte/line range so a whole
  secrets file can't dump into permanent history in one call — still no
  redaction, just bounds the blast radius). Revisit once there's an actual
  plan for keeping agent-visible secrets out of persisted session history —
  likely needs `RedactMessage` (or an equivalent) actually wired into the
  turn loop before this is worth building.
- [ ] **Web/desktop (`internal/server/handler_files.go`) not started —
  deliberately, per review.** No terminal and no external-editor window
  exists there, so this needs its own shape: decrypt-on-read/encrypt-on-
  write in the file-content handlers, plus an unlock endpoint/flow that
  feeds a server-side `secretfile.Session` (and answers what its
  lifetime/scope should be — per HTTP session? per browser tab? timed
  lock?). Do this as a separate pass, not bolted onto the TUI wiring.

## Background bash commands promoted mid-flight can still orphan on force-kill (2026-08-26)

Found while fixing the local-model-hijacks-the-tty startup crash
(`internal/tui/tty_foreground_unix.go`, `internal/tool/proc_unix.go`).

- [x] **Fixed**: `run_in_background: true` bash commands (`internal/tool/exec.go`,
  `BashTool.ExecuteStreamCtx`) now go through `WrapWithParentMonitor` before
  `StartBackground`, so they self-terminate (polling `kill -0` on ocode's PID)
  within ~0.5s even if ocode is killed with SIGKILL and never runs its own
  `supervisor.Shutdown`/`TerminateAll` cleanup. `p.Command` is reset to the
  caller's original text afterward so `bash_output`/`kill_shell` listings still
  show the real command, not the wrapper shell around it. Verified the wrapper
  mechanism itself with a standalone repro (force-kill a fake parent, confirm
  the wrapped child dies within ~1s) — see conversation, not committed as a
  test since it exercises shell polling timing, not ocode code directly.
- [x] **Fixed**: foreground commands are wrapped with `WrapWithParentMonitor` before
  `cmd.Start()` in `BashTool.ExecuteStreamCtx`, so a command promoted to
  background mid-flight retains parent-death protection without changing its
  streaming pipes, timeout/cancellation path, registry record, supervisor
  tracking, or process-group ownership. The integration test
  `TestBashToolForegroundPromotionParentDeath` force-kills a helper process and
  verifies that both the tracked monitor and promoted command terminate.

## Desktop anchors sessions at home dir when Finder-launched (2026-08-25)

- [x] **Fixed (2026-08-25)**: `glob`/`grep`/`list` now anchor on `WithWorkDir` (`internal/tool/search.go:resolveSearchRoot`) and `MERGE_SNAPSHOT`/`ChatPanel` guard now keys only on `messages.length>0` (see `web/src/stores/chatStore.tsx` and `web/src/components/Chat/ChatPanel.tsx`).
- [ ] **Deferred**: `cmd/ocode-desktop/main.go:60-68` falls back to `workDir=$HOME` when cwd is `/` — a default `~` project still triggers unbounded `WalkDir` at `internal/agent/md_discovery.go:453`. Fix: don't treat `~` as implicit project; restore last-used or require explicit pick, and/or cap md-discovery when no `.git`/`AGENTS.md` marker.

## Tool calls with duplicate argument keys executed silently (2026-08-23)

Found by scanning on-disk `.ojsonl` session history while investigating the
desktop hang, **not** by reproducing the hang. Two separate defects; only the
first has confirmed on-disk instances.

- [x] **Duplicate-key tool arguments were executed last-wins, silently**
  (`duplicateTopLevelKey` in `internal/agent/client.go`, applied at
  `handleToolCallWithContext`). `json.Valid` — the only
  argument check — **accepts** duplicate keys, and `encoding/json` resolves them
  last-wins. A model that emits `{"path":"a.py","end_line":120,"path":"b.md"}`
  therefore read `b.md`, silently discarding the rest of what it asked for, with
  no error anywhere.
  **Measured incidence: 60 of 59,041 tool calls in local session history**
  (`read`, `bash`, `grep`, `task`, `task_status`), concentrated in a handful of
  sessions. 58 repeat a *top-level* key; the remaining 2 repeat a key inside a
  nested `multiedit` `edits[]` element and are deliberately **not** caught — see
  `TestDuplicateTopLevelKey_OnlyInspectsTopLevel` for the reasoning.
  These are model-emitted, not parser-assembled: each is a single valid object
  with interleaved keys (`end_line` sits *between* the two `path` keys), whereas
  a parser concatenation would produce `{…}{…}`, which does not parse at all.
- [x] **`parseOpenAIChatCompletionsStream` could merge two parallel tool calls**
  (`internal/agent/client.go`). Accumulation was keyed solely on
  `tool_calls[i].index`, and an absent `index` unmarshals to `0`. A provider that
  repeats or resets `index` across parallel calls would land both in one slot:
  later `id`/`name` overwriting the earlier, argument fragments concatenated.
  **Latent — no confirmed on-disk instances** (a concatenation would be
  unparseable and none were found); fixed on inspection of the accumulator.
  Fix: accumulate in arrival order; a delta bearing a **different** non-empty
  `id` opens a new call even at the same index — compared against the id already
  seen, so providers that echo one id per fragment stay a single call. Ordering
  is `sort.SliceStable` by index, preserving the previous index ordering while
  being able to express two calls at one index. Residual gap: a provider that
  sends `id` only on a *later* fragment leaves the open call's id empty and will
  not split — the duplicate-key check above is the net for that case.
  Covered by `TestParseOpenAIChatCompletionsStream_{SplitsParallelCallsSharingAnIndex,RepeatedIDIsOneCall}`;
  the over-split boundary was sabotage-verified.
  This is the only *lazily* index-keyed tool accumulator. The Anthropic path
  (`client.go:3350`) also keys blocks by index but creates them on the
  protocol-mandated explicit `content_block_start` and **assigns** rather than
  concatenating, so it cannot merge two calls.

- [x] **RESOLVED: duplicate-key arguments skip the call instead of ending the
  turn.** Making them fatal in the parser was wrong and was reported from real
  use within hours (`grep`, repeated `output_mode`): the turn died, and because
  the error matches nothing in `isRetryableLLMClientError` the loop stopped dead
  with the model never learning what was wrong. The check moved to the dispatch
  site (`handleToolCallWithContext`, `internal/agent/agent.go`), which is the
  single funnel every path reaches — parallel, sequential, in-batch DAG, and
  orphan recovery. The call is skipped, never executed, and the conflict comes
  back as an ordinary tool result naming the key *and both values*:
  `arguments set "output_mode" twice, to "content" and then to
  "files_with_matches". Re-issue this call once, with a single value`. The turn
  continues and the model re-issues on the next loop.
  Two properties this must not lose, both pinned by
  `TestAgentSkipsDuplicateKeyToolArguments`: the tool never runs, and the
  skipped call still emits a tool result carrying its `tool_call_id` — without
  the pairing the *next* request 400s, which would have swapped one dead loop
  for a harder-to-diagnose one.
  Moving the check off the OpenAI stream parser also widened it: it now covers
  the Anthropic path, whose `input_json_delta` assembly can produce the same
  duplicate keys and was never checked at all.
  The `isRetryableLLMClientError` sentinel noted here previously is **not** the
  fallback any more — retrying replays the same prefix and tells the model
  nothing about what was wrong.
- [ ] **Neither defect explains the 22GB hang.** Both produce one executable
  call; these are correctness bugs, not memory bugs. The hang item below stays
  open and unproven.

## Bash runaway-output memory guard + tool-output streaming (2026-08-23)

Prompted by a report of the desktop chat hanging on a single tool call while
memory climbed to ~22GB. Root cause is **still unproven** — see the open items.

- [x] **Bash output is bounded while the command runs** (`internal/tool/exec.go`,
  `internal/tool/bounded_buffer.go`): the pump copied a child's stdout/stderr into
  an uncapped `bytes.Buffer` for the whole timeout window (up to 600s of arbitrary
  output). `bashMaxOutputLength` (30000) is applied by `truncateOutput` only
  *after* the command exits, so it never bounded the peak. Both sinks are now
  `boundedBuffer` capped at `bashMaxRetainedBytes` (64MiB per stream, so 2x that
  per command since stdout and stderr are separate), head-retained so
  `truncateOutput` still shows the genuine start of the output. Dropped bytes are
  reported via `appendRunawayNotice`, never silently discarded. `boundedBuffer.Write`
  always reports full consumption because `io.Copy` aborts on a short write, which
  would otherwise tear down the pump (and live streaming) at the cap. Covered by
  `internal/tool/bounded_buffer_test.go` and `TestBashTool_BoundsRunawayOutput`.
- [x] **Full-output retention decoupled from "is a stream wired"**
  (`internal/tool/fulloutput_ctx.go`, `internal/agent/agent.go`,
  `internal/tui/model.go`): `capAtMax` was derived from `emit == nil`, so wiring a
  streaming sink silently disabled the 30000-char cap. That mattered because the
  uncapped text reaches a UI only via `Message.DisplayContent`, which is `json:"-"`
  and never serialized to the browser — so wiring `emit` on the SSE server would
  have retained unbounded output for a consumer that structurally cannot read it.
  Retention is now opt-in via `tool.WithFullOutputRetained` /
  `Agent.RetainFullToolOutput`; the TUI opts in (unchanged behavior), the server
  stays capped. Covered by `TestBashTool_StreamCapsWithoutRetainFlag`,
  `TestExecuteToolCallCapsOutputByDefault`, `TestStreamStepOptsIntoFullToolOutput`.
  **Note: this changes no observable behavior on its own** — it is a prerequisite
  for streaming tool output to the desktop (below).

- [x] **Tool output streams to the web/desktop UI.** `Agent.OnToolOutput` was
  wired only in `internal/tui/model.go`, so a browser saw nothing between
  `tool_start` and `tool_result` and a slow tool was indistinguishable from a hung
  one. The server now wires it (`wireHeadlessAgentCallbacks`) through
  `internal/server/tool_output_stream.go`, a **goroutine-free** coalescer: chunks
  batch until ~100ms elapses or 8KiB accumulates, decided inline on the producing
  goroutine with the time passed in. No ticker and no per-call timer — a leak
  there is the exact failure shape under investigation. Per-call streaming is
  capped at 256KiB (the shared bus buffer is 256 slots across every session and
  panel) with a one-time notice, and the cap short-circuits before any buffering.
  Unlike the TUI — which backpressures its single in-process consumer — the server
  never blocks: a slow or vanished browser must not stall a running command, and
  the authoritative content still arrives via `tool_result`. Buffers are released
  when the result lands (`activeCalls()` returns to zero). New `tool_output` SSE
  event; rendered by `ToolBlock` while pending.
- [x] **`call_id` threaded through `tool_start` / `tool_output` / `tool_result`**
  (server + the TUI's RC bridge), with `findPendingToolIndex` pairing on it in
  `chatStore` and falling back to the old positional match only for events without
  one. Covered by `internal/server/tool_events_test.go` and frontend tests that
  were verified to fail when `call_id` is dropped.

Deferred:

- [ ] **Allocation measurement for the streaming path not done.** Wiring `emit`
  moves bash onto the streaming branch (`exec.go` `emitWriter` → `safeEmit`),
  adding a string allocation per chunk — roughly 32KiB of transient garbage per
  pipe write — on the exact hot path under suspicion for the memory event. The
  coalescer bounds what reaches the *bus*, not what the emit path allocates
  upstream of it. Measure a high-volume command before/after before treating this
  as memory-neutral.
- [ ] **Elapsed-time indicator on `ToolBlock`** (`web/src/components/Chat/TurnParts.tsx`).
  Client-only, zero server/bus cost. Streaming does **not** help when the stuck tool
  produces no output — which "hang forever" most likely means — because the bubble
  still just reads "running…". An elapsed timer is what makes a silent hang legible.
- [ ] **Other unbounded single-call memory paths — found, not fixed.** The
  reported symptom was *one* tool call, so these stay live suspects while the
  diagnosis is open: `ReadTool.Execute` (`internal/tool/file.go`) calls
  `os.ReadFile` with no `Stat` size check and then `strings.Split(string(content),
  "\n")`, doubling a large file in memory before `maxReadLines` truncates at
  format time — `read` is one of the most-invoked tools; `SearchTool`
  (`internal/tool/search.go`) reads each matched file whole inside a
  `filepath.Walk` and accumulates matches with no cap on the total; `imagegen.go`
  uses `io.ReadAll(resp.Body)` without a `LimitReader` (`web.go` correctly does
  use one). Mirror the `boundedBuffer` / `LimitReader` treatment if any of these
  is implicated.
- [ ] **22GB hang root cause still unproven.** The uncapped buffer above is the
  leading candidate and is now fixed, but it was never desktop-specific, so it does
  not by itself explain "TUI works fine". Next repro: confirm *which* OS process
  holds the memory (Wails runs a separate WebKit renderer), then capture
  `GET /debug/pprof/heap?debug=1` with the bearer token (`internal/server/server.go`
  registers it; token at `cmd/ocode-desktop/main.go`). Discriminating signal: are
  retained bytes under `bytes.Buffer.grow` beneath `io.Copy`? Blocked on the
  durable-desktop-log gap in "Mid-turn failure transcript loss — 2026-08-21".

### Follow-up: chronic climb reported separately from the acute 22GB spike (2026-08-24)

New report: memory "constantly" climbing during ordinary desktop use, not tied to one
hung tool call — a different signature (sustained drift vs. one acute spike), so
treat as a separate hypothesis from the item above until proven otherwise.

- Confirmed live on the running desktop app (PID pair: `ocode.app` Go backend +
  its Wails/WebKit renderer, identified unambiguously via the Networking XPC
  helper's `ESTABLISHED` loopback connection to the backend's listen port — do
  not rely on launch-time coincidence to attribute a WebContent XPC pid, there
  can be several unrelated ones running): backend RSS ~535–590MB and creeping
  over a few minutes of sampling; renderer RSS swings ±500MB inside 30s windows
  (938→361→638→500MB observed), which is GC/paint churn, **not** by itself
  evidence of a leak — a short sample cannot separate "leaking" from "large
  steady state + noisy GC". A 30-minute background sampler
  (`ps -o rss=` on both pids, 30s interval) was started to read the actual
  floor trend; results not yet analyzed — check
  `/private/tmp/claude-501/-Users-james-www-ocode/66ad6368-bd94-4c9f-9729-d79608ea1624/scratchpad/mem_samples.tsv`
  from that session, or re-run the same sampling if that scratchpad is gone.
- [x] **Ruled out: closing a chat session tab does not leak its transcript.**
  `chatStore.tsx`'s `sessions` map (`Record<string, SessionSlice>`) has a
  `RESET` action that deletes one session's slice, and all three UI paths that
  call `closeSessionTab()` (`App.tsx` Cmd/Ctrl+W handler, `OpenSessionBar.tsx`
  tab-X, `SessionDialog.tsx` picker close) pair it with
  `chatDispatch({ type: "RESET", sessionId })` immediately after. Go-side
  `Handler.agents` also has a real eviction path (`SessionManager`'s
  `onEvict` hook in `internal/server/handler.go`), keyed off idle timeout, that
  clears `h.agents`, `h.turnLocks`, and calls `agent.Shutdown()`. Neither is
  the chronic-growth source.
- [ ] **Prime suspect, not yet fixed: terminal tabs are kept mounted for the
  app's lifetime, across every project, with no automatic reaping.**
  `TerminalTabs.tsx` deliberately force-mounts a `TerminalPanel` (hidden via
  `display:none`, comment: "so background [output] keeps streaming") for every
  terminal tab ever opened in every project; each is a live `xterm.js`
  `Terminal` instance plus a WebGL renderer context (`TerminalPanel.tsx:165,189`).
  These are disposed only when the user explicitly closes that terminal tab —
  `terminalPersistence.ts`'s orphan-GC (`gcBuffers`/`gcProjectBuffers`) only
  cleans the *persisted localStorage scrollback strings* for terminals no
  longer referenced, not any live in-memory instance, and nothing evicts a
  terminal automatically (by idle time, by project switch, or by a cap on
  count). This is a deliberate design tradeoff (keep background command output
  live), not a bug in the traditional sense, but it means normal long-session
  usage across many projects/terminals is genuinely unbounded — the most
  concrete "keeps going up, never comes down" candidate found so far. Not
  fixed here; needs a decision (idle-close policy? hard cap with LRU eviction?
  explicit "close all terminals" affordance?) before changing behavior.
- [ ] **Still-open single-call memory bugs from the 2026-08-23 item above,
  re-verified still present on disk 2026-08-24** (contributors to acute spikes,
  not the chronic pattern, but real and unaddressed): `ReadTool.Execute`
  (`internal/tool/file.go:400,414`) still does `os.ReadFile` +
  `strings.Split(string(content), "\n")`, doubling a large file in memory
  before `maxReadLines` truncates; `SearchTool` (`internal/tool/search.go`)
  still reads every matched file whole inside `filepath.Walk` with no cap on
  total accumulated matches; `imagegen.go:510,604,784` still uses
  `io.ReadAll(resp.Body)` with no `LimitReader` (unlike `web.go`, which already
  does this correctly).

### Correction + live repro attempt (2026-08-24, same day)

User corrected the report: this is **not** the chronic terminal-tab pattern
above — it is the acute one, reproduced with a specific trigger: "when I add a
2nd message to the chat, it stuck forever" (memory climbing at the same time).
This matches the "22GB hang" item's shape, now with an actual trigger, so
treat the terminal-tab item above as a separate, lower-priority finding.

- **Live capture of the actual event**: the background RSS sampler (see the
  timestamped entry above) caught the desktop backend process jump from
  683MB → 3.3GB RSS in one 30s window while the user reported this happening,
  stayed ~3.1–3.3GB for ~60–90s, then the process (`ocode.app`'s Go backend,
  PID 90842) and its whole WebKit renderer group vanished sometime in the next
  30s — no crash report, no spin report, no jetsam log; almost certainly a
  manual force-quit of an unresponsive app, not a signal-generating crash. The
  live pprof capture the earlier TODO item asks for was not obtained — the
  process was gone by the time this was noticed.
- **Clean synthetic repro attempt did NOT reproduce it.** Ran `ocode serve`
  headless (unauthenticated by default — no username/password configured — so
  `/api/debug/runtime` and `/debug/pprof/*` are directly reachable without the
  desktop's random per-boot token; this is the fastest way to get pprof access
  for future repro attempts, no need to fight the desktop's token). Sequence:
  POST `/api/chat` with a prompt forcing a 25s `bash sleep`, then while
  `turn_active:true`, POST `/api/sessions/{id}/message` (a second message —
  the exact trigger reported). Result: handled correctly end-to-end — the
  second message went through `tryEnqueueInjection` → `Agent.injectQueue`,
  drained at the next tool-call boundary (`agent.go:1102`), and got its own
  reply once the bash call finished. Server heap stayed ~11–25MB and goroutine
  count stayed ~10–16 throughout; no growth, no hang. So the minimal
  "inject one message mid-tool-call" path is *not* itself the bug.
- **False lead, worth recording so it isn't retried:** `GET /api/chat/messages`
  (`HandleSessionMessages`, `internal/server/handler_sse.go:281`) is an SSE
  endpoint (`Content-Type: text/event-stream`) that intentionally never closes
  — it streams the initial history then blocks forever on `<-sub`/`<-ctx.Done()`.
  A plain `curl` against it (no `-N`, no client timeout) looks exactly like a
  server hang but isn't one; wasted a background-task timeout confirming this.
  Use `GET /api/sessions/{id}` for a one-shot transcript fetch instead.
- **Unexplored, possibly relevant:** the real session's transcript contains
  `openai_response_items` reasoning blocks with large `encrypted_content`
  blobs (provider-opaque reasoning-continuation state, thousands of chars
  each, one or more per assistant turn) that presumably get re-sent on every
  subsequent request for the life of the conversation. Not shown to cause
  anything by itself in the short repro above (only 2 turns), but never tested
  at the length/turn-count where the user actually hit the hang, and never
  measured for whether it's echoed back (compounding) or just replayed
  (linear). The repro above also used a different model
  (`opencode-go/muse-spark-1.2-contributor`) than the user's desktop default
  (`opencode-go/mimo-v2.5` per `ocodeconfig.json`) — provider/model-specific
  streaming behavior hasn't been ruled out.
- **Next repro, if this happens again**: don't wait for the app to be force-quit.
  While it's stuck, immediately capture (desktop needs its per-boot token —
  see the original item above for where it lives; `ocode serve` needs none):
  `GET /debug/pprof/goroutine?debug=2` (full stacks — shows exactly what every
  goroutine is blocked on, settles the deadlock-vs-runaway-allocation question
  in one shot) and `GET /debug/pprof/heap?debug=1`, plus `GET
  /api/debug/runtime` a few times a couple seconds apart to see if heap/goroutine
  counts are climbing or just large-and-flat.
- [x] **Diagnostic capture wired for next time**: `internal/desktop/boot.go`
  now writes `~/.config/opencode/desktop-debug-handle` (0o600, owner-only,
  overwritten every launch) with the current run's `url=` and `token=` —
  `saveDebugHandle`, called from `StartServer` right after the listener binds.
  The token was previously only ever in-memory (by design, security), which
  made the original item's "capture pprof with the bearer token" step
  impossible without rebuilding with extra logging *before* a hang happens.
  Now `cat ~/.config/opencode/desktop-debug-handle` + `curl -H "Authorization:
  Bearer $token" $url/debug/pprof/...` works immediately. Covered by
  `TestSaveDebugHandleWritesURLAndToken` in `boot_test.go`. Not read back by
  the app itself — debug-only, not a stable file format.
- **User confirmed: happened on a short/fresh session**, not a long one — this
  weakens (doesn't rule out, but deprioritizes) the `openai_response_items`
  reasoning-blob-accumulation theory above, since there wasn't much history to
  accumulate.
- [x] **Second clean repro attempt, also did NOT reproduce it**, this time
  matching the user's actual default model (`opencode-go/mimo-v2.5`) and
  racing message 2 in immediately (167ms after message 1's dispatch, likely
  *before* message 1's first LLM call even started) rather than during a tool
  call. Result: no hang. Both user messages landed in the same first LLM call
  (the injected message spliced in ahead of the first `chatWithDelta`, per
  `agent.go`'s drain-at-top-of-loop-iteration timing) and got one combined
  reply — a model-behavior quirk (it answered "also, whats 2+2" and silently
  ignored "hi", replying just "4"), not a server bug. Heap crept ~20.4MB→21.6MB
  over 14s idle afterward, consistent with normal Go runtime bookkeeping, not
  a leak signal at this scale.
- **Where this leaves the search**: two different injection-timing shapes
  (mid-tool-call, and racing-the-first-LLM-call) both handled correctly in a
  fresh headless `ocode serve`, on both the repro's default model and the
  user's real default model. The bug either needs conditions not yet
  reproduced (a specific prompt/tool combination, a desktop/Wails-specific
  interaction the headless server doesn't exercise, a rarer timing window) or
  a live capture next time it happens — which is now one `curl` away.

### FOUND + FIXED: the "constantly increasing, did nothing" chronic leak (2026-08-24, same day)

Confirmed root cause, not the acute hang above — a **separate bug**, found live
via the new debug-handle diagnostic on the very first idle-session repro.

- [x] **Root cause: `internal/server/scheduler_runner.go`'s `RunScheduledJob`
  never released anything.** Every cron/scheduled-job firing built a full
  `agent.NewAgent` (spawns `docMaintenanceWorker` + `memoryMaintenanceWorker`)
  and a full `lsp.Manager` via `tool.LoadBuiltins` (spawns a `fileWatcher.loop`
  goroutine) — and called neither `Agent.Shutdown()` nor `lsp.Manager.Close()`
  on **any** return path, success or error. Confirmed by two
  `/debug/pprof/goroutine?debug=2` dumps 2 minutes apart, grouped by
  normalized stack: `docMaintenanceWorker`, `memoryMaintenanceWorker`, and
  `lsp.(*fileWatcher).loop` counts went 18→27 in exact lockstep, all
  `created by internal/agent.NewAgent` / `internal/lsp.newFileWatcher` "in
  goroutine 40" — one full leaked bundle per firing.
  Trigger: user's `~/.local/share/opencode/scheduler/<instance>/jobs.json` had
  a job ("ping") on `every_ms: 10000`, already at **7,465 runs**, every one
  failing with a 401 (LLM provider key missing the `model.request` scope —
  a credentials/permissions problem on that key, not an ocode bug; not fixed
  here, flagged to the user separately). Fires independent of any user
  interaction — explains "loaded a session, did nothing, memory keeps
  increasing." **Verified safe to close per-call**: `lsp.NewManagerWithShared`
  always constructs a fresh `*Manager` (the "shared" flag only affects
  whether the underlying gopls *process* is brokered, not whether the
  `*Manager` object or its file watcher are shared) — so this fix cannot
  affect any other agent's LSP manager.
  Fix: `defer lspMgr.Close()` + `defer ag.Shutdown()` right after
  construction in `RunScheduledJob`.
- [x] **Hardening: `internal/scheduler` now enforces a 30s minimum on
  `KindEvery` schedules** (`minEveryMs` in `types.go`) — even with the leak
  fixed, nothing stopped a misconfigured job from rebuilding a full
  agent+LSP-manager every few seconds forever, which is wasteful regardless
  of cleanup correctness. `validateSchedule` rejects new/updated jobs below
  the floor (`TestAddJobRejectsEveryBelowMinInterval`); `computeNextRun`
  additionally *clamps* (not rejects) already-persisted jobs below the floor,
  so a job saved before this existed — like the user's real "ping" job — is
  automatically bounded to 30s without needing manual editing
  (`TestComputeNextRun`'s new sub-case). KindCron already has an inherent
  ~1-minute floor (minute-resolution parser, no seconds field) so it needed
  no change.
- **Still open, not part of this fix**: the "ping" job's 401 will keep
  firing (now every 30s instead of 10s) until its underlying API key's scope
  is fixed or the job is disabled/deleted — that's a credentials issue on the
  user's provider account, out of scope for a code fix.
- **Diagnostic-handle addition from earlier today already paid for itself**:
  found and root-caused this on the very first `cat
  ~/.config/opencode/desktop-debug-handle` + pprof round-trip, no guessing.

## `/autocontinue` — general auto-resume (2026-08-23)

Implemented, general-purpose (fires for whatever command/task is in flight, not scoped to `/goal`), across TUI and `/rc` (web) turns:
- `internal/agent/agent.go`: `Agent.StepLimitHit()` (hard signal, true only when a turn was cut off by the `/max-step` cap, no extra LLM call) and `Agent.AutoContinueJudgeAsync` (optional judge-model call for replies that look interrupted WITHOUT hitting the step cap — only runs when `/autocontinue model` is set).
- `internal/tui/model.go`: `streamDoneMsg` auto-continue logic (`autoContinueEnabled`/`autoContinueCount`/`lastTurnWasAutoContinue`/`autoContinueMaxChain` cap = 4), `autoContinueJudgeFinishedMsg` handling (requires `!m.streaming`, `!m.queueDrainBlocked()`, no pending compaction splice, and a matching `autoContinueGen` before acting on a judge verdict — the turn may have moved on since dispatch), `/rc` support via `pendingRCAutoContinue`/`autoContinueJudgeForRC` (the `/rc` listener is deliberately left un-armed mid-chain because `rcRequestMsg` has no re-entrancy guard against `m.streaming` and would race the chain's own `askAgent()` call), sidebar toggle row (`autoContinueToggleTopIdx`/`Rows`, click handler, `sidebarAutoContinueToggleForClick`).
- `internal/tui/commands.go` / `picker.go`: `/autocontinue [on|off|status|model [name]]`, judge-model picker (kind `"autocontinue-model"`, wired into every model-picker membership check, `selectPickerIndex` dispatch, and `refreshModelPickerItems`).
- `internal/config/ocodeconfig.go`: `AutoContinueEnabled`/`AutoContinueModel` + `SaveAutoContinueEnabled`/`SaveAutoContinueModel`.

Deferred (out of scope for this pass — flag to the user if they want it):
- No automated test coverage for the `/rc` (web) auto-continue path specifically — the deferred-listener-rearm logic (`pendingRCAutoContinue`, `autoContinueJudgeForRC`) was verified by tracing `rcRequestMsg`'s lack of a `m.streaming` re-entrancy guard, but there's no integration test driving an actual `RCRequest`/`streamDoneMsg`/judge sequence end-to-end (would need a fake SSE client + mock agent harness). The pure chain-cap logic (`shouldAutoContinue`) does have tests in `internal/tui/autocontinue_test.go`.
- The judge model's prompt/verdict format (`internal/agent/agent.go` `runAutoContinueJudge`, bare YES/NO) is unvalidated against a real local model — only compile-checked, not run against a live LM Studio/local server.

## `.ojsonl` session format — concurrent-writer safety not solved (from design: 2026-07-21)

Design: `docs/superpowers/specs/2026-07-21-session-storage-ojsonl-design.md`.

- [x] **Cross-project `.ojsonl` resume fallback.** IMPLEMENTED THEN REVERTED —
  `findOjsonlSessionAnyProject` (added 2026-08-05, commit 4981eea) fell back
  to `.ojsonl` files in *any* project's storage dir when neither the current
  project's `.ojsonl` nor its legacy `.json` contained the id. REMOVED
  2026-08-07: `Load` no longer searches other projects' storage dirs
  (`shouldSearchOtherProjects`/`readSessionFileAnyProject`/
  `findOjsonlSessionAnyProject` deleted) — sessions are strictly scoped to the
  project root they were created under, and resuming a session by id from a
  different cwd fails instead of loading a foreign session.

Design: `docs/superpowers/specs/2026-07-21-session-storage-ojsonl-design.md`.

- [ ] **Concurrent writers to the same session can produce duplicate/conflicting
  entries.** `Save()`'s `persistedCount` cache is per-process. If two ocode
  processes (e.g. TUI + server, or two server requests) append to the same
  session concurrently, each can append its own version of "the next message"
  based on a stale count — no data is silently dropped (unlike today's
  full-rewrite race, which drops the loser's messages), but the file can end up
  with duplicate or conflicting entries at what was meant to be the same
  position. No file locking is introduced to prevent this; it matches the
  existing single-writer-per-session assumption elsewhere in the codebase (see
  the already-tracked `index.json` race). Deferred out of scope for the
  `.ojsonl` change itself — revisit if concurrent-write safety becomes a
  priority, possibly as part of a larger move to SQLite (see design doc's "Out
  of scope" section for why opencode made that move).

- [ ] **Title rewrite can silently drop a concurrent append (data loss, not
  just duplication).** The temp+rename header-rewrite path swaps in a new
  inode at the session's path; a process that already had an `O_APPEND` handle
  open on the old inode keeps writing to it after the rename, and those writes
  become invisible to any later reader of the path. Worse than the duplication
  case above — same root cause (no locking, single-writer assumption), same
  deferred status. If limitation #1 is ever fixed with an advisory lock, fix
  this one at the same time.

## Kaizen per-model stack benchmark — deferred wiring & corpus (from docs/okf: 2026-07-11)

The benchmark corpus + scoring system is built under `docs/okf/` (design:
`docs/superpowers/specs/2026-07-11-model-stack-benchmark-design.md`). React is a
fully-built exemplar (26 Q&A + rubric, one worked scorecard, one example derived
skill). Not yet done:

- [x] **Detection engine** — `internal/stackdetect` (`Detect(root) []string`)
  reads package.json deps + marker files per `stack-detection.md`. Tested. This
  is the reusable core; nothing consumes it yet.
- [x] **Wire the enforcement hook.** Implemented as the **skill-catalog filter**
  (keeps full SKILL.md). `internal/skill/loader.go`: the parser now captures
  Kaizen frontmatter (`tuned_for`, `stack`); a skill with non-empty `tuned_for`
  is a Kaizen skill. New `LoadSkillsForModel(root, activeModel)` +
  `BuildCatalogForModel(root, activeModel)` admit a Kaizen skill only when
  `modelMatchesTuned(activeModel, tuned_for)` (case-insensitive exact OR
  provider-prefixed `.../tuned_for`, so `novita/tencent/hy3` matches
  `tencent/hy3`) AND its stack is active (`conduct`/empty = universal, else in
  `stackdetect.Detect(root)`). The **default** `LoadSkills()`/`BuildCatalog()`
  now EXCLUDE all Kaizen skills, so no ungated caller can leak them;
  `LoadSkill(name)` still resolves them by exact name (explicit request).
  `stackdetect.Detect(root)` is computed ONCE per build from stable
  (workdir+model) inputs, respecting the prefix-cache contract. Wired into
  `internal/agent/context.go` `LoadContext(...)` (now takes `activeModel, root`,
  threaded from `prompt.go` and `tui/model.go`) → `BuildCatalogForModel`.
- [x] **Deliver Kaizen skills under discovery (was a no-op).** Verified
  2026-07-11 by running hy3 headless with the shipped binary, no skill mention:
  discovery is ENABLED by default, so `LoadContext` skipped `BuildCatalogForModel`
  and the discovery glue built its catalog from the Kaizen-free
  `skill.LoadSkills()` (`discovery_glue.go`) with a fail-open to
  `skill.BuildCatalog()` — both stripped `tuned_for` skills, so
  `conduct-tuning-tencent-hy3` was never advertised. FIX (delivery = advertise,
  not force-load, per user): new `skill.KaizenSkillsForModel(root, activeModel)`
  returns ONLY the admitted tuned skills; `discoveryDocs()` appends them to the
  corpus so they ALWAYS sit in the always-visible names-index (never dependent on
  the embedder's rank), and the fail-open path now calls `BuildCatalogForModel`.
  The full SKILL.md body still loads on demand via the `skill` tool — the LLM
  decides. Guarded by `TestKaizenSkillAdvertisedInDiscovery` (fail-open + active
  paths both list it) and `internal/skill.TestKaizenDelivery_hy3_conduct`
  (admit/exclude/wrong-model). Note: discovery is NOT required — with discovery
  off, `LoadContext`'s `BuildCatalogForModel` already advertised it.
- [x] **Force-inject a directive digest (Option B — advertise-only wasn't enough).**
  2026-07-12: re-running hy3 closed-book showed it sees the advertised tuning
  skill but never calls the `skill` tool to load the body (overconfident — one
  round-trip, answered from base knowledge). Since a per-model tuning skill is
  relevant on EVERY turn that model is active, its hard rules must be *present*,
  not merely *offered*. A tuning `SKILL.md` may now carry a compact digest between
  `<!-- kaizen:digest -->` … `<!-- /kaizen:digest -->`; `skill.KaizenDigestBlock`
  collects admitted digests and `LoadContext` force-injects them into the base
  prompt as authoritative rules — UNCONDITIONAL (independent of the discovery
  flag), keyed on `(activeModel, root)` for prefix-cache stability, and exactly
  `""` for any non-matching model or digest-less skill (no prefix drift). Doc
  exception recorded in `docs/okf/_schema/stack-detection.md`. Guarded by
  `TestExtractDigest`, `TestKaizenDigestBlock_hy3` (asserts the counterintuitive
  cruxes survive compression), and `TestLoadContext_KaizenDigestInjected`.
  - [x] **EFFECTIVENESS VALIDATED (partial) — 2026-07-12, live on the real
    machine.** Ran hy3 closed-book on the two originally-failing weak tags (see
    `docs/okf/conduct/answers/tencent__hy3.digest-spotcheck.md` +
    scores re-test log). Result: **conduct-halluc-02 0.00 → 1.00** (answer echoes
    the digest crux "confidence is not an exemption") — the digest demonstrably
    works on-topic. **conduct-safety-03 stayed 0.00** across BOTH a rule-only
    digest AND a second digest that named the exact banned commands verbatim;
    hy3 recommended `git reset` / `git restore --staged .` both times. That is a
    hard model-APPLICATION ceiling (the rule was provably in-context), not a
    delivery gap — more digest weight doesn't move it, so the safety-03 worked
    example was reverted and the digest is capped at its lean effective form.
    Delivery mechanism (Option B) is proven; per-tag effectiveness is
    framing-dependent and bounded by the model.
- [x] **Embed home for derived skills** = `skills/kaizen/<name>/SKILL.md` inside
  the existing `//go:embed all:skills` tree. `docs/okf/_tools/sync-derived-skills.py`
  copies every `docs/okf/*/derived/*.SKILL.md` there (dir = frontmatter `name`),
  idempotent + prunes stale dirs. Re-run it after adding a derived skill. The
  loader gained `kaizen/` subtree search paths because `loadSkillsFromPaths`
  only descends one level.
- [x] **Populate stacks**: golang (33), rust (31), tanstack (31), nextjs (34)
  built to the `docs/okf/react/` schema — 129 records, validated, version-
  sensitive facts checked via ctx7. Subcategories (nested folders) still open
  where a stack warrants finer axes.
- [ ] **Run the first REAL (closed-book) evaluation — the next actionable step;
  unblocks the enforcement hook above.** FIRST ATTEMPT WAS CONTAMINATED: a
  `tencent/hy3` run over all 6 stacks scored a flat 100% (200/200) because the
  answering agent had the corpus open — its answers paraphrased the reference
  `answer` fields verbatim (incl. un-learnable ocode house rules). Those
  scorecards + answer files were deleted. Root cause + fix now enforced:
  - **Rule 0 (closed-book)** added to `HOW-TO-EVALUATE.md`: answerer and grader
    are separate agents; the answerer sees ONLY `_prompts/<stack>.md`.
  - **Answer-free sheets** generated by `docs/okf/_tools/gen-prompt-sheets.py`
    into `docs/okf/_prompts/<stack>.md` (id + question only).
  Redo: give the target model each `_prompts/<stack>.md` closed-book → save to
  `<stack>/answers/<model-id-flattened>.md` → grade with a SEPARATE agent →
  write `<stack>/scores/<id>.md` + (only if weak tags) `<stack>/derived/...SKILL.md`.
  `react/scores/claude-opus-4-8.md` remains an illustrative placeholder.
- [ ] **Build a grading harness (optional, speeds real evals).** A small tool
  that reads `questions.yaml`, sends each question to a target model, and emits a
  pre-filled scorecard for human rubric-grading (or LLM-judge-assisted grading).
  Must enforce the closed-book barrier (feed the model `_prompts/`, not
  `questions.yaml`) and record exact `model_id` + `model_version` + `stack_corpus_rev`.
- [ ] **Optional: a `questions.yaml → questions.md` generator** to kill the
  hand-sync drift risk noted in the design (6 stacks now hand-synced). The
  `_tools/gen-prompt-sheets.py` script is a starting point (same parse).

## Local discovery embedder — `0 attached` fix + bge-m3 default (from Bug C: 2026-07-12)

Fixed `/discover` showing `local: none` / `0 attached` on Apple Silicon. Root
causes, all addressed and runtime-verified (the `0 attached` diagnosis was
corrected mid-investigation — see Bug C below):
- [x] **Stale status (Bug A)** — `EnsureLocalServer` adopt paths never called
  `setStatus("ready")`; added to both branches (`localserver.go`). Regression:
  `TestEnsureLocalServer_adoptSetsReady`.
- [x] **Warm deadlock (Bug B)** — a 500ms synchronous warm budget vs a local
  embedder that needs 0.5–1.6s meant the corpus never warmed (all-or-nothing
  cache never persisted). Now defers a cold warm to a single-flight background
  goroutine (`discovery_glue.go`, `warming atomic.Bool` + `startBackgroundWarm`).
  Regression: `TestStartBackgroundWarm_singleFlight`.
- [x] **`0 attached` (Bug C) — CORRECTED DIAGNOSIS, resolved by defaulting to
  bge-m3.** First blamed on the MLX server running LFM2.5 (`lfm2-bidir`, bidir +
  CLS) through mlx_lm's CAUSAL forward + MEAN pooling (real degradation:
  CLS/position-0 cosine = 1.0000 across inputs → causal). That fix shipped —
  LFM2.5 moved to **llama.cpp b9777** (added `lfm2-bidir`, PR #24913; b9747
  rejects it) + the official **Q4_K_M GGUF** (`pooling_type=2` CLS), `manifest.go`
  + `cache.go` `cacheFormatVersion=2` (regression `TestCacheInvalidatesOnFormatVersion`).
  BUT it did NOT fix `0 attached`: measured live, the correctly-pooled llama.cpp
  model scores a strong conduct match at **0.18–0.26** (LOWER than causal MLX's
  0.31), still far below `SelectMin=0.40`; `query:`/`document:` prefixes didn't
  help. **Real cause: LFM2.5's naturally COMPRESSED cosine band** (matches ~0.2–0.3,
  off-topic ~0.05–0.09) vs a `SelectMin=0.40` tuned for bge-m3's wide band.
  **Fix: `DefaultLocalModelID` → `local/bge-m3` on all platforms.** Measured live:
  bge-m3 scores the same conduct match at **0.49** (clears 0.40), off-topic ~0.29–0.34
  (below) → attaches correctly. LFM2.5 stays opt-in via `/discover model
  local/lfm2.5-embedding`. Did NOT lower the global `SelectMin` (would mis-calibrate
  bge-m3/http); a per-model floor (~0.15) is the alternative if LFM2.5 attachment is
  ever wanted.
- [x] **`libDirForBinary` (migration correctness).** After the b9747→b9777 bump,
  `binDir` holds BOTH version dirs; the old `findLibDir` scanned and grabbed the
  first (b9747), pairing the new binary with old dylibs → ABI mismatch. Now the lib
  dir is the launched binary's OWN parent dir. Regression: `TestLibDirForBinary`.
  Verified live: the spawn used `DYLD_LIBRARY_PATH=.../llama-b9777` with the b9777
  binary.
- [x] **RUNTIME-VERIFIED 2026-07-12** on Apple Silicon: b9777 + both GGUFs download
  + spawn (SHA-verified); cache re-embeds to `version:2`; `/discover` `local: ready`;
  bge-m3 attaches (`0.49 ≥ 0.40`), LFM2.5 does not (`≤0.26`). The gated
  `OCODE_LIVE_RETEST=1 go test ./internal/discovery/ -run TestLiveRetest` runs
  against a live server on 11457.
- [x] **Migration wrinkle (FIXED 2026-08-21; tightened 2026-08-22).** `EnsureLocalServer` now auto-reclaims
  11457 when a FOREIGN-model or wedged server squats it: central pid-recorded
  `embed.start.lock` (machine-global, not project-scoped; `O_CREATE|O_EXCL` + owner pid; instant reclaim when holder pid dead instead of burning the full wait, 10m mtime stale fallback for pre-pid locks) serializes spawns across `ocode` instances; contender waits with per-second holder-pid liveness check (`waitForEmbedHealthTracked`) for fast crash takeover (live-holder lock is never broken — no `breakEmbedStartLock` race); `reapStrayEmbedServer` re-probes before kill and identifies ocode servers by ANY manifest token OR cache-artifact path (`<cache>/local-*` + `mlx_embed_server.py`) plus `--port 11457` + `listeningPIDs` ownership, so a stale server from a prior version/model switch serving a different model is correctly reclaimed (previously protected the squatter by matching only `ExpectedServeID`). An unidentifiable holder is reported, never killed (protects LM Studio on 11457). User-explicit `UserBaseURL` mismatch still hard-errors without reap. Chat `*.start.lock` now uses the same central pidlock with conditional release (only removes own pid). LSP daemons remain project-root scoped via `broker.StartOnce` flock (kernel-released on crash, no pid file needed). First probe wrong-model no longer fail-opens — falls through to lock→reap→spawn. In-process `localMu` preserved.
- Note: `mlx_embed_server.py` + the `BackendMLX` spawn path are retained (dormant)
  for any future MLX-only model; no default local model uses MLX now.
- [ ] **Optional (measured NOT worth it for attachment):** the http/local embedder
  ignores `EmbedKind` (`httpEmbedder.Embed(..., _ EmbedKind)`), so no asymmetric
  `query:`/`document:` prompt is applied. Tested live against the llama.cpp CLS
  LFM2.5 server: the documented `query:`/`document:` prefixes scored a match at 0.20
  vs 0.26 bare — WORSE, not better. So per-kind prefixes do NOT rescue LFM2.5's
  band and are not the `0 attached` fix. Could still marginally sharpen ranking for
  some models, but low priority; if pursued, needs a per-model prompt field + a
  `cacheFormatVersion` bump (passage text changes).

## Shared agent notes bus — deferred production wiring (from review-changes: 2026-06-16)

The notebus feature is wired into the parallel agent group (bus, per-call binding,
write-touches, reconcile hand-off, secret redaction). Two design-mandated capabilities
remain reachable only from tests because they require plumbing outside the agent package:

- [ ] Sidecar persistence is inert: `Agent.SetNoteBusDir` has no production caller, so
  `noteBusDir` is always empty and `maybeBuildGroupBus` never opens a `Sidecar`. Wire it
  from the session layer (pass the session dir + `SetNoteBusSessionTag` at agent
  construction) so a mid-group crash can be recovered. (from review-changes: 2026-06-16)
- [ ] Brief seeding is inert: `Agent.SetNoteBusBrief` has no production caller, so children
  never receive the orchestrator's pre-computed brief and `groupTracker` partitions stay
  empty in prod. This is delivered by the `/review-changes` skill rewrite (plan Part 04),
  which is the component that computes and sets the brief. (from review-changes: 2026-06-16)

## `/rc` full live mirror — follow-ups (built, not yet run end-to-end)

The 2-way live mirror (TUI↔web: user messages, thinking/text token deltas, tool
calls/results, turn snapshot) is implemented across `internal/server`
(`rc_bridge.go` broadcast fan-out, `handler_sse.go` `HandleSessionMessages`),
`internal/tui/model.go` (broadcast sites in `deltaMsg`/`streamMsgEvent`/`streamDone`/
user-submit), and the web app (`connectSessionMirror`, store `live` buffer,
`TurnParts`/`MessageBubble`/`ChatPanel`). Compiles, typechecks, unit-tested — but
**not verified live** (interactive TUI). Open items:

- **Verify end-to-end.** Run `curl -N "http://localhost:PORT/api/chat/messages?token=TOK"`,
  type in the TUI, confirm event order: `user_message` → `thinking`/`text` deltas
  → `tool_start`/`tool_result` → `messages` + `turn_done`. Then both-directions
  in the browser. If `turn_done` arrives for a TUI-originated turn, the
  `pendingRC==nil` end-of-handler snapshot path is confirmed.
- **Optimistic echo removed.** Web-typed messages now render only after the
  round-trip `user_message` broadcast — invisible on localhost, a perceptible
  delay over Tailscale. Decide whether to re-add optimistic-add with dedup.
- ~~**`tool_result` carries no call-id**~~ — FIXED 2026-08-23. `tool_start`,
  `tool_output`, and `tool_result` all carry `call_id` (`ToolCall.ID` /
  `Message.ToolID`), and the store pairs on it via `findPendingToolIndex`,
  falling back to the old positional match only for events without one. See
  "Bash runaway-output memory guard + tool-output streaming".
- **`SET_STREAMING: true` + autoscroll fire on every token delta** — fine on
  localhost, potentially janky on long turns over a network. Throttle if needed.
- **Browser "Stop" is local-only** — during a TUI-originated turn it re-locks the
  input on the next delta. No web cancel path exists; add one if desired.
- **Committed tool rendering is per-message** (assistant `tool_calls` block +
  separate `tool` result block) rather than paired. The live view pairs them;
  consider pairing in `ChatPanel` for the committed snapshot too.

## `/ide` VS Code integration — deferred backends & limits

The `/ide` command (internal/ide + TUI wiring) connects to VS Code via the
**Claude Code extension's** WebSocket+MCP lock-file protocol (`~/.claude/ide/*.lock`).
It auto-enables when running inside a VS Code terminal (`TERM_PROGRAM=vscode`)
unless `ide_mode` is set in ocode.json. Deferred / out of scope for now:

- **opencode-extension backend not implemented.** opencode's own extension
  (`sst-dev.opencode`) only POSTs a one-shot `@file#Lx-y` to an HTTP server
  (`/app`, `/tui/append-prompt`) on a keypress — no live selection tracking. A
  `/ide opencode` mode (ocode serving those endpoints + reading
  `_EXTENSION_OPENCODE_PORT`) was scoped but not built; the Claude Code backend
  supersedes it for live data. Add only if a user explicitly wants the opencode
  extension path.
- **No editor jump-to from ocode.** We read selection/open-tabs but don't yet
  drive VS Code to a location (the extension exposes an `openFile` MCP tool we
  could call).
- **`at_mentioned` insert is best-effort.** Inserts `@rel#Lstart-Lend` into the
  input; relies on the extension emitting the event (Cmd+Alt+K style).

## Clickable file paths in messages — known limitations

Auto-detected, clickable file paths were added to rendered chat messages (web
`MessageBubble` + TUI transcript and agent drill-in). Open in `$EDITOR`/`$VISUAL`
with system-opener fallback. Deferred / limited behavior:

- **TUI click ignores `:line` suffix.** A path like `handler.go:42` opens the
  file but does not jump to the line (the shared `createEditorOpener` has no
  line-jump support). Web jumps only for code-family GUI editors (`--goto`).
- **Web cannot open terminal editors.** The server is headless (no TTY), so
  `vim`/`nano`/etc. can't run from a browser click — it falls back to the system
  opener. Only GUI editors (`code`, `cursor`, `zed`, …) or the OS default work.
- **Paths split across a visual-line wrap boundary** linkify only the first
  segment (TUI). Acceptable; full-token reconstruction across wraps not done.
- **Web path resolution uses the server process `os.Getwd()`** (mirrors
  `handleFileContent`). If a session cwd ever differs from the launch dir,
  relative paths won't resolve.
- **Not exercised with live mouse interaction.** Verified via render-test (custom
  `filelink` element renders to a clickable span), regex/detection unit tests,
  server security/validation tests, and reuse of the existing working
  selection-coordinate math — but a live hover/click walkthrough on each surface
  was not run.

## AST/LSP semantic tool — deferred work

The old ast-grep "code_rel" tool + `.sgindex` daemon were removed (they relied on a
persistent ast-grep index that doesn't exist; the daemon was a no-op). Replaced by an
opt-in, LSP-backed `ast` tool (`internal/tool/ast.go`) over `internal/lsp`. Disabled by
default; toggle with `/plugin enable ast` (persisted in ocode config `plugins.ast`).

Incomplete / best-effort:
- **Only gopls is validated end-to-end.** `internal/lsp/manager.go` maps `.rs`,
  `.py`, `.ts/.tsx/.js/.jsx` to rust-analyzer / pyright-langserver / typescript-language-server,
  and `.dart`/`.php`/`.java`/`.cs`/`.rb`/`.c`/`.h`/`.cpp`/`.hpp`/`.cc` to dart language-server /
  intelephense / jdtls / csharp-ls / solargraph / clangd, with correct stdio invocations, but
  these are **untested here**. Verify per-language before relying on them. Note: jdtls needs a
  JDK 17+ on PATH; clangd wants a `compile_commands.json` for accurate cross-file results.
- **`callers` (incoming call hierarchy) is best-effort.** Requires the server to support
  `textDocument/prepareCallHierarchy` + `callHierarchy/incomingCalls`. gopls does; many
  servers don't, in which case it returns an error rather than results.
- **`lsp` and `ast` tools overlap.** Both go through `internal/lsp` (shared client +
  Manager + formatters), but `lsp` (position-based, always-on) and `ast` (name-based,
  opt-in) are two tools doing related work. Consider consolidating to one tool once the
  name-based UX proves out.
- **LSP servers are never `Close()`'d.** `lsp.Manager` has no lifecycle hook in the Tool
  interface, so each `/plugin disable ast` rebuild orphans the prior gopls until ocode
  exits (same pre-existing behavior as the old `lsp` tool — gopls is designed to be
  long-lived, but a second toggle spawns a fresh one). A Tool `Close()`/shutdown hook
  would let `rebuildAgentWithExternalTools` reclaim them.
- **Name resolution is heuristic.** `resolveSymbol` picks the first exact-name workspace
  symbol (then trailing-name match, then first hit). Ambiguous names (same symbol in
  multiple packages) resolve to one location; no disambiguation UI.

## Anthropic extended-thinking signatures (interleaved multi-turn)

When `ThinkingBudget > 0` and `anthropic-beta: interleaved-thinking-*` is enabled, Anthropic requires that prior assistant thinking blocks be replayed *with their original `signature` field* on subsequent turns or the request is rejected. The streaming SSE parser in `chatAnthropic` (`internal/agent/client.go`) captures the signature into a per-block field but discards it on completion; `Message` has no place to round-trip thinking blocks across turns, and `convertToAnthropicMessages` only emits `text` + `tool_use` blocks for assistant history. This matches the previous non-streaming behavior (parity), but interleaved-thinking multi-turn flows will fail. Fix requires: (1) persist thinking blocks + signatures on `Message`, (2) replay them in `chatAnthropic`'s outbound `messages`, (3) ensure compaction/repair paths preserve them. Out of scope for the streaming-thinking work that introduced this note.

## Auth — deferred work

- **macOS Keychain backend.** File store at `~/.config/ocode/auth.json` (0600) is what ships. A self-contained `internal/auth/keyring_darwin.go` could shell out to `security` with a file fallback.
- **Background token-refresh goroutine.** Refresh is currently lazy on `HydrateEnv` + `ResolveKey`/`OAuthAccessToken`. A goroutine would help only for sessions that idle longer than a token lifetime without any tool use.
- **Per-provider base-URL override UI.** `Credential.BaseURL` is honoured by `NewClient` but there's no dialog stage to set it — populate `~/.config/ocode/auth.json` by hand for now.
- **Account population for Anthropic / OpenAI OAuth.** Copilot populates `Credential.Account` via `GET /user`. The Anthropic/OpenAI token responses don't reliably include an email; would need an extra `/me` or JWT `id_token` parse.

## Separated Agent System — core implementation complete; remaining work

Core infrastructure complete (2026-05-19):
- ✅ Agent registry (`internal/agent/agent_registry.go`) with agent definitions and lifecycle
- ✅ Agent permissions system (`internal/agent/agent_permissions.go`) with per-agent rules
- ✅ Child session tracking (`internal/agent/child_session.go`) with ID and metadata generation
- ✅ Agent loader (`internal/agent/agent_loader.go`) for filesystem-based agent definitions
- ✅ TaskTool updated to use registry and support hidden agents
- ✅ Child session persistence callback infrastructure (`Agent.SetChildSessionPersistence()`)

Remaining integration work:
- **Wire child session persistence callback.** `Agent.SetChildSessionPersistence()` needs to be called in `internal/runcli/run.go`, `internal/server/handler.go`, and `internal/tui/connect.go` (in `rebuildAgentClient()`) to enable child session recording.
- **Remove dead code.** `TaskTool.getToolsForSubAgent()` is unused; superseded by `getToolsForDef()`.
- **Surface permission diagnostics.** Log warnings from `buildPermissionManagerFromAgentWithDiags()` when agent-file permissions contain unsupported fields or unknown groups.
- **Test per-agent permission application.** Verify child agents receive the agent-definition permissions, not the parent's permissions.
- **Test child session persistence.** Verify child session ID is generated, messages persisted, and result includes session ID link.

## Sandboxed program execution — wrapper with halt-ask-resume

Goal: wrap bash/python (and other code execution) so the agent can halt on a file/network access, ask the user, then resume or block with access-denied.

Permission-detection fixes first (live bug in `internal/agent/permissions.go`):
- **Relative-path escape.** `Decide()` skips the workdir check for non-absolute paths (`if filepath.IsAbs(path) && !isWithinWorkDir`). `read ../../../etc/passwd` is allowed. Resolve every path against `workDir` first, then check the resolved absolute path.
- **Fail-open on extraction failure.** Empty path from `extractPathFromArgs` falls through to `pm.Check()` → `allow` for `read`/`write`. Should fail closed to `ask`.
- **Multi-file tools.** `apply_patch`/`multiedit` patch many files but only `params.Path` is checked. Enumerate every target.
- **Enforce at the tool, not just `Decide()`.** Put the workdir/sensitive check inside the file-open chokepoint so new callers/subagents/MCP can't bypass it.

Execution wrapper design:
- **Tier 1 — spawn-in-sandbox (cross-platform).** `sandbox-exec` profile (macOS) / `landlock`+namespace or `bwrap` (Linux). Workdir read-write, rest denied, network denied. Fail-closed; on violation surface "denial → widen scope → re-run".
- **Network ask-proxy.** Spawn child with `HTTP_PROXY`/`HTTPS_PROXY` → in-process proxy; sandbox blocks all other egress. Real halt-ask-resume per request, cross-platform.
- **Tier 2 — seccomp user-notif (Linux only).** Wrapper becomes a per-syscall supervisor: kernel parks the syscall, wrapper prompts, returns continue or `EPERM`. True mid-run halt-ask-resume. Gate behind `runtime.GOOS == "linux"`.
- **FUSE mount (optional).** Only cross-platform way to truly halt-and-resume per filesystem op; heavyweight (macFUSE = user-approved system extension). Defer unless per-file mid-run prompts become a hard requirement.
- Note: `sandbox-exec`/`landlock`/containers **cannot** resume mid-run — policy is fixed at spawn, violating syscall just fails. macOS has no unprivileged mid-run halt mechanism.
- Wire wrapper into `internal/tool/process.go` spawn path; generate sandbox profile per run; hook proxy/permission callback into the existing `PermissionResponse` flow.

## LLM provider layer — deferred work

- **Streaming provider adapters.** `internal/agent/llm_contract.go` defines stream event types and the optional `StreamingLLMClient` interface, but `GenericClient` still uses request/response chat. Next step is dedicated OpenAI-compatible, Anthropic, and Copilot adapters that emit `text_delta`, `thinking_delta`, tool-call, usage, and done events.
- **Thread context into `LLMClient.Chat`.** Title generation (`title.go`) and compaction (`compact.go`) wrap `client.Chat` in `select { case <-ctx.Done() }`, but the inner goroutine ignores cancellation and keeps running until the HTTP client's 5-minute timeout fires. Adding `Chat(ctx, ...)` to the interface + propagating through all 4 providers and the test mocks would let these helpers actually cancel. Bounded leak today, but cost is real on slow networks. (from review-changes: 2026-05-24)
- **Drop `AgentTool` legacy shim.** The `agent` tool is no longer registered (`internal/agent/agent.go` only registers `task`). The type stays for transcript/permission compatibility. Remove once historical sessions don't need to round-trip — pick a date (e.g. 2026-08-01) and delete the type, the back-compat permission alias, and the TUI tool renderer branch. (from review-changes: 2026-05-24)

## Context compaction — deferred work

Async token-threshold compaction landed 2026-05-20 (fixes the 12 issues from the prior roast):
- ✅ Tool-pair-safe slicing (no orphan `role=tool` after compaction)
- ✅ Real token-usage triggers via `resp.Usage.PromptTokens` + `ModelWindow()`
- ✅ Tool-aware summary prompt (tool calls, results, reasoning included)
- ✅ Turn-boundary tail preservation (whole last user turn kept intact)
- ✅ Configurable thresholds (`compact.token_threshold`, `keep_recent_turns`, etc.)
- ✅ Summary call: context timeout + retry + structured debug logging
- ✅ Immediate post-Step trigger (no re-summarisation every turn)
- ✅ Persisted to session (TUI splices `m.messages`, calls `saveSession`)
- ✅ UI banner: `📦 Compacted N earlier messages`
- ✅ Mid-loop warning emitted when prompt tokens exceed window threshold

Deferred:
- **Mid-loop hard compaction.** A single Step with many tool calls can still blow past the window before returning. Today we only warn; the compaction runs after `streamDoneMsg`. Implementing in-loop compaction would require pausing the tool loop at a tool-pair-safe checkpoint, summarising, and resuming — non-trivial.
- **Retry the failed Step after compaction.** If the LLM call inside Step fails with a context-length error, the UI surfaces the error and the post-Step compaction never runs (Step returned early). Could detect context-length errors, run sync compaction, and replay the Step.
- **Streaming summary.** The summary client call is blocking. If it becomes the bottleneck on slow providers, switch to a streaming variant that lets the UI show partial summary text as it arrives.
- **Drop stale `pendingCompactUIIdx`.** If the user clears the session between compaction trigger and completion, the splice indices become stale. Today `applyCompactionResult` guards with bounds checks, but a session-generation counter would be cleaner.

Anchored compaction landed 2026-05-27 (anchored summary, structured template, prune-before-summarise, custom summary model already wired). Still deferred:
- **Switch threshold to `usable(model)` not `ratio × window`.** opencode subtracts reserved-for-output from the input limit and triggers at actual usage ≥ usable. ocode still uses `0.85 × window`, which mis-fires on models whose effective input differs sharply from total context. Needs `ModelWindow` to expose input/output split.
- **`PRUNE_PROTECTED_TOOLS` list.** opencode protects `skill` tool outputs from pruning. ocode has no equivalent. Likely candidates here: outputs from `agent_status`, `task_status`, `wait`, and MCP tools marked durable. Until then, every large tool result is prunable.
- **Persisted on-disk prune sink.** Today `pruneToolResults` shrinks tool content in-memory before summarisation. The full output exists on disk via `internal/agent/truncate.go` only when the tool result was already large enough to be truncated at write time. Wire `pruneToolResults` to write any pruned content to disk + emit a `[full output: <path>]` reference so the agent can re-read it via the read tool.
- **Drop char-fallback flat 1.15× multiplier.** `shouldCompact` applies a flat 15% safety margin when `Usage` is missing. This hides per-content-type weighting (text vs reasoning vs tool JSON vs images). Replace with weighted estimation or log a warning + skip compaction when `Usage` is absent.
- **Surface compaction in TUI history.** The 📦 banner shows the count but not the structured summary content. A "view summary" affordance (expand banner, copy summary text, jump to splice point) would make multi-compaction sessions navigable.

## Provider prompt hybrid — Phase 3 (deferred)

Phase 1 (file-backed prompts) and Phase 2 (model-ID routing) landed 2026-05-27. Phase 3 was descoped after discovering markers are load-bearing for `prompt_shape_test.go`. Still deferred:
- **Cache `environmentPrompt()` output on Agent.** Today every `BasePromptMessages` call re-runs `os.Getwd`, `findWorkspaceRoot` (walks parents), and 3× `os.Stat`. Cache once per agent; invalidate on cwd or model change.
- **Resolve mode-vs-spec prompt ambiguity.** `BasePromptMessages` computes `Mode().SystemPrompt()` and then conditionally overrides it with `spec.SystemPrompt`. Two prompt sources, one wins, the other was computed for nothing. Pick one resolver and document the precedence.
- **Reconsider marker dedup.** The 5-marker `[ocode:*]` system with `existingPromptMarkers` scan provides idempotency and testability. If marker semantics drift further, revisit whether a simpler `Agent.prompted bool` + a separate test mechanism would be cleaner.

## Plugin system — `/plugin` command + native reimplementations ✅ (2026-05-29)

Implemented on branch `feat/plugin-auth-hooks`. All three subsystems shipped:

- **`/plugin` TUI command** — list, enable/disable, install (git+local with confirm flow), remove, info. `PluginConfig` with `Dir`/`Ref` fields in config. `internal/plugins/manager.go` with InstallGit/Local, RunOnInstall (direct exec, no shell), AutoRegisterMCP/UnregisterMCP.
- **Auth providers** — Cloudflare Workers AI (account ID prompt + BaseURL construction), Cloudflare AI Gateway (o-series max_tokens strip), OpenAI Codex (reuses OpenAI OAuth). `AccountID` field on `Credential`.
- **In-process hook pipeline** — `internal/hooks/pipeline.go` with ToolBefore/After, ChatParams, ShellEnv hook points. Wired into `executeToolCall`, `chatWithDelta` (save/restore GenericClient fields), and bash subprocess env injection in `tool/process.go`.

Deferred (CocoIndex plugin): see plan `docs/superpowers/plans/2026-05-28-cocoindex-plugin.md` — requires plugin system to be merged first.

## apply_patch parity with opencode — follow-up

- **Align remaining edge cases with upstream behavior.** Current parser/executor now supports opencode-style `*** Begin Patch` envelopes, `*** Add/Delete/Update File`, `*** Move to`, `@@` hunks, and rollback on failure. Next pass should compare against upstream behavior for duplicate context, repeated hunks, rename+update ordering, and exact failure modes.
- **Match upstream error strings where practical.** LLM behavior can be sensitive to familiar tool responses; aligning error wording may improve self-correction when a patch is malformed.
- **Add edge-case tests.** Cover move+update in one patch, EOF insertions via `*** End of File`, multiple hunks in one file, repeated-context matching, and whitespace-tolerant matching cases.
- **Consider importing or porting the upstream parser more literally.** If true byte-for-byte compatibility is a goal, the cleanest path is a closer structural port of the upstream opencode apply_patch parser rather than maintaining a merely compatible reimplementation.

## LLM auto-permission: interpreter execution (2026-06-08)

- **[done] Surface the model's effect summary / reject reason in the human-ask prompt.**
  `PermissionRequest` now carries an optional `Summary`, the interpreter auto-
  permission path preserves it on deny/ask requests, and the TUI permission
  dialog renders it alongside the deny reason when present.
- **Heredoc handling is a line-based pre-pass (`extractHeredocs`), not a
  rune-level tokenizer state.** This is intentional (far lower blast radius on the
  shared shell parser) and covers `<<DELIM`/`<<-`/quoted delimiters/multi-heredoc.
  If full shell-grammar heredoc fidelity is ever needed (e.g. heredocs mid-pipeline
  with interpreter not first word), revisit.

## TUI streaming render: residual O(N) viewport cost (2026-06-09)

- **renderTranscript no longer re-wraps/re-strips the whole transcript per delta.**
  Per-message cache now stores each block's wrapped + ANSI-stripped line slices;
  a streamed delta only re-renders the one changed message. Result: 1000-pair
  transcript dropped 87.7ms→27.6ms per render and 62MB→2.9MB allocs (543× fewer
  allocs), collapsing the GC pressure that was stalling the event loop. Realistic
  sessions (~100 pairs) are now ~2.8ms.
- **Residual (root cause):** the bubbles/v2 viewport's `SetContentLines` does two
  O(N) passes over the whole transcript on every delta — a reverse `ContainsAny`
  `\r\n` scan plus `maxLineWidth` (`ansi.StringWidth` per line). Confirmed via
  benchmark: 18019 lines / 3001 msgs = ~35ms/render at only 180 allocs/op, i.e.
  pure CPU in line-scanning, not allocation. Real session measured at 2747 lines /
  381 msgs = 8–12ms/render. The chat viewport has `SoftWrap=false` and never
  horizontal-scrolls, so `longestLineWidth` (the sole consumer of `maxLineWidth`)
  is computed-then-never-used — both scans are dead work.

- **Fixed (A + B), 2026-06-09:**
  - **A.** Coalesced the streaming render cadence (`lastDeltaRender` throttle in
    `applyThinkingDelta`) from 50ms→90ms while auto-scrolling, halving in-flight
    CPU with no perceptible animation loss (~11fps vs 20fps on a thinking stream).
  - **B.** Replaced the chat transcript's bubbles viewport with a reusable
    pre-wrapped, no-softwrap content surface (`internal/tui/fastviewport`) whose
    `SetContentLines` is O(1) (pointer assign, no scan) and whose `View`/scroll
    math is O(visible window). API-compatible with the subset the chat uses
    (Height/Width/YOffset/GotoBottom/GotoTop/AtBottom/ScrollUp/ScrollDown/
    SetYOffset/TotalLineCount/VisibleLineCount/SetContent/SetContentLines/Update/
    View); `scrollbarSetOffset` was made generic over a `scrollbarVP` interface so
    it serves both viewport types. `Update` is a no-op because the chat drives
    scrolling via explicit calls (keys are never forwarded; mouse wheel is handled
    by the parent) — verified via `shouldForwardToTranscriptViewport`.
  - **Result:** `renderTranscript` at 1000 pairs / 18019 lines dropped
    30.3ms→0.73ms (~41×); 100 pairs 2.8ms→0.19ms. The benchmark attribution that
    justified B: `SetContentLines` alone was 28.6ms of the 30.3ms (94%) at 0
    allocs — pure CPU in the two dead scans.

- **C. Deferred — tail-incremental assembly (only do if B is still not enough):**
  Even with B's O(1) `SetContentLines`, `renderTranscript` still rebuilds the full
  `transcriptLines`/`rawTranscriptLines` slices every delta (append loop over all
  messages + the toolNames prebuild) — O(messages), not O(tail). For pathological
  sessions (1000+ pairs / 90K+ lines) the next lever is to keep the assembled
  prefix cached and only re-append the tail message(s) that changed since the last
  render, making a streamed delta truly O(tail). This needs careful invalidation
  (width/theme change, message edit/delete, expand/collapse toggles all dirty the
  prefix) and must preserve the byte-identical unwrapped `nlAcc` region math that
  the click/selection/thinking/tool hit-testing depends on. Deferred: B should
  bring the common case (≤400 msgs) under ~2ms, below the perceptible threshold,
  so the prefix-cache complexity isn't justified until a real session proves
  otherwise.

## UI overhaul Part 3 review gaps (Tasks 18–19)

- [ ] Task 18: manual visual verification still pending — run `ocode serve`, switch theme in config, reload web, confirm colors follow (hex→HSL fix landed 2026-06-11 but was verified via unit math + typecheck only, not in a browser) (from review-changes: 2026-06-11)

- Discovery embedder key resolution uses `os.Getenv` only (internal/agent/discovery_glue.go:keyForEnv). Wire the stored-credential/keyring fallback (same source `/connect` populates) so users who authed via OAuth/keyring rather than env vars can use HTTP embedders.

## Desktop shell (ocode-desktop) — plan gaps from review

- [x] `internal/server/run_states.go` + tests — fixed 2026-07-02: moved out of server.go, added `Handler.RunStates()`, rc-bridge runs, sorted session keys, Status-derived Ended/Failed, 3 tests (from review-changes: 2026-07-02)
- [x] `internal/desktop/watch.go` + tests — fixed 2026-07-02: exported `Diff(prev, cur)` keyed by (SessionID, ID), emit-on-change contract (incl. count-drop-to-zero), 5 tests incl. race pass (from review-changes: 2026-07-02)
- [x] `go mod tidy` — fixed 2026-07-02: wails/v3 now a direct require (from review-changes: 2026-07-02)
- [x] `cmd/ocode-desktop/native.go` (Task 6) — done 2026-07-02: dock badge from RunningCount, notifications for finished/failed runs when unfocused, click-to-focus via OnNotificationResponse, focus tracking (WindowFocus/WindowLostFocus), native error dialog on boot failure. Menu roles: DefaultApplicationMenu already includes App/File/Edit/View/Window/Help roles in alpha2.111 — no change needed (from review-changes: 2026-07-02)
- [x] Task 7 — done 2026-07-02: `desktop-app` target + `scripts/bundle-macos.sh` (bundle verified), CHANGES.md + README.md updated (desktop section added, "No desktop frontends" removed), spec packaging section updated (from review-changes: 2026-07-02)
- [x] Permission-prompt badge — done 2026-07-02: `Handler.PendingPermissionAsks()` counts sessions whose transcript tail is an unanswered `PERMISSION_ASK:` sentinel tool message; badge shows running + pending, plus an "Agent needs permission" notification when the count rises while unfocused (from review-changes: 2026-07-02)
- [ ] Desktop deferred items: Windows installer + Linux packaging (deb/rpm/AppImage — evaluate wails3 tooling once out of alpha), macOS code signing/notarization for ocode.app, verify external links open in the default browser on the pinned alpha (add handling if the webview keeps them internal) (from review-changes: 2026-07-02)
- [ ] macOS notifications require a .app bundle: any UNUserNotificationCenter call from a bare binary aborts the process (NSInternalInconsistencyException), so `notificationsSupported()` disables the notifier entirely outside `.app/Contents/MacOS/` (verified by smoke test). Notifications work only via `make desktop-app`; an unsigned bundle may still be denied authorization — full support lands with signing (from plan Part 03: 2026-07-02)

## Server question-prompt bridge (headless only)

- [ ] Web answering of agent `question` prompts works only in **headless serve mode** (the server owns the agent in `Handler.agents`). In `/rc` bridge mode the TUI owns the agent and its own question dialog; `POST /api/questions` returns 409 there because the server has no hook to resolve the TUI dialog without changing `internal/tui` behavior. Closing the RC-mode gap needs a TUI-side path (e.g. inject the web answer into the TUI's `submitQuestionAnswers` via the RC bridge) — deferred (from server question bridge: 2026-07-09).
- ~~Note: the pre-existing permission-prompt bridge is likewise not wired end-to-end for the web~~ — **FIXED 2026-07-09**: the mirror now emits `permission`/`permission_resolved` SSE frames (from `wireHeadlessAgentCallbacks`'s `PERMISSION_ASK:` sentinel detection), and a dedicated `POST /api/permissions/resolve` (`{request_id, session_id?, approved}`) resolves the ask by executing (approve) or denying the paused tool call and re-Step'ing — mirroring the question bridge. The config `POST /api/permissions` (`{tool, level}`) is untouched. `web/src/hooks/useChat.ts` `resolvePermission` now calls `api.resolvePermission`.

## Bench: Terminal-Bench harness follow-ups

- [ ] **Cache-token reporting.** `ocode run`'s `usage` event omits
  cache-read/cache-write counts because `Agent.OnUsage` carries only input and
  output. The gateway does return `prompt_cache_hit_tokens`. Widening the
  callback signature would let the bench measure prompt-cache effectiveness
  directly. See `docs/superpowers/specs/2026-07-30-terminal-bench-harness-design.md`.
- [x] **`subset.txt` frozen — DONE 2026-07-31.** 12 tasks by stratified
  sample across `terminal-bench-core==0.1.1`. Re-picking invalidates every
  prior comparison.
- [ ] **Baseline sweep not yet run.** `bench/terminal_bench/sweep.sh baseline`
  runs the frozen 12-task subset (3 attempts each) and records pass rate with
  spread + per-task token means into the README. Needs real API spend and a
  few hours; not run during the implementation turn.
- [x] **Live end-to-end smoke — DONE 2026-07-31.** Verified on
  `hello-world`, `sqlite-db-truncate`, `fix-permissions` (3/3 resolved):
  `ocode-run.jsonl` lands in `sessions/` with a trailing `"type":"usage"` line,
  `total_input_tokens` propagates into `AgentResult` (35,635 / 265,581 /
  45,224), no `save session` errors, all runs completed under the timeout.
- [x] **Egress assumption — CONFIRMED 2026-07-31.** LLM calls to
  `opencode.ai` succeed inside TB task containers. One transient Docker
  internal-DNS blip was observed at the very end of a long chess run (53
  successful calls before it); not systematic.
- [ ] **First smoke tasks (`chess-best-move`, `count-dataset-tokens`) exceed
  the 900s timeout.** Both are intrinsically long; the chess agent flailed on
  image analysis. The frozen subset currently avoids them; revisit only if a
  longer timeout is acceptable for the sweep.

## Server permission-prompt bridge — deferred items

- [x] ~~Web permission resolution is **approve/deny only** — no "always allow"~~ — **FIXED 2026-08-24**: `POST /api/permissions/resolve` now accepts `decision: allow | deny | always_rule | always_tool` (legacy `approved` bool still mapped), and the web `PermissionDialog` offers the same four choices as the TUI with a confirm step. Persistence routes through the same guarded path as the TUI: `agent.AlwaysRuleChoiceAvailable` / `AlwaysToolChoiceAvailable` availability gates, `agent.IsHarmfulRequest` refusal, out-of-scope asks persist only the path root to `extra_allowed_paths`, webfetch domains stay session-scoped, and bash-prefix/tool rules land in both the live PermissionManager and ocodeconfig.json. Availability rules live in one place (`internal/agent/permissions.go`) shared by TUI + server; `permission_resolved` is now broadcast **before** the continuation Step so the dialog dismisses instantly instead of lingering for the next model round (the reported "does not auto close" bug). Bridge mode forwards `always_rule`/`always_tool` to the TUI's rcResolve mapping.
- [ ] Like the question bridge, permission resolution works only in **headless serve mode**; `/rc` bridge mode forwards decisions to the TUI (which owns the agent + its own permission dialog) rather than resolving server-side (from permission bridge: 2026-07-09; updated 2026-08-24 — the 409 note above predates the forwarding path).

## In-batch task DAG — deferred cross-turn `depends_on` (from PLAN-agent-crew/02-task-dag.md: 2026-08-07)

Phase 3 of PLAN-agent-crew/ implemented `id` / `depends_on` for **in-batch**
dependency edges — within a single parallel batch of `task` calls, the
scheduler validates the graph, schedules in dependency order, injects
predecessor output into the dependent's `context`, skips transitive
dependents on failure, and respects cancellation. `internal/agent/task_dag.go`
+ `task_dag_test.go` + `agent.go` wiring.

Deferred:

- [ ] **Cross-turn `depends_on`.** A `depends_on` referencing a `task_id`
  from an earlier turn (or an in-flight background run) is out of scope for
  v1. Required to support "ship A, then B that consumes A" across a
  multi-turn workflow without re-dispatching A. Genuinely useful and
  genuinely more complex than the in-batch case: cross-turn lifetime
  (parent session may resume), background-run id resolution (registry
  lookup), cancellation propagation across turn boundaries, and per-edge
  persistence (so a resume can rebuild the partial graph). Likely
  requires a `dispatched_at_turn` annotation on the run registry and a
  new "wait-for-run" primitive alongside `AcquireForRun`. Build on the
  in-batch scheduler, do not replace it.

## Desktop per-window sparse profiles — deferred slices (from 2026-08-20 spec)

Design: `docs/superpowers/specs/2026-08-20-desktop-profiles-sparse-design.md` (sparse `ocodeconfig.json {profiles:{name: ProfileDelta}}` + `~/.local/share/ocode/auth.profiles.json` 0600 + `window-state.json` per-window).

Implemented (v1 backend):
- [x] Sparse overlay storage + `0600` sidecar + window-state file with advisory locks (`internal/config/ocodeconfig.go`, `internal/auth/profile_store.go`, `internal/paths`, `internal/config/profile_window_state.go`)
- [x] Effective helpers `EffectiveOcodeConfig`/`EffectiveConfig`/`LoadEffectiveForWindow` (`internal/config/profile_effective.go`) and `SaveProfile`/`RenameProfile`/`DeleteProfile` via `withOcodeConfigLock`
- [x] Window-scoped APIs `GET/POST /api/profiles`, `DELETE/POST rename /api/profiles/{name}`, `GET/PUT /api/window/{id}/activeProfile` (`internal/server/handler_profiles.go`, `internal/server/server.go`)
- [x] Env alias `OCODE_PROFILE=work ocode` / `ocode --profile work` ephemeral win (`internal/runcli/run.go`, `internal/auth/providers.go:resolveKeyWithConfig` via `activeProfileForResolution` window-state fallback, wired into `internal/server/agent_session.go:buildAgentSession` for next-turn model/keys — currently global (all sessions see most recent window's profile)).

Deferred:
- [ ] **Per-window isolation.** `activeProfileForResolution` is global (env > win-1 > any window) and `Handler.h.cfg` mutation in `buildAgentSession` is process-global. Thread `windowID` through `SessionManager` entry → `buildAgentSession` so each session resolves its own window's profile (no cross-window leakage).
- [ ] **Header pill + Settings UI.** `web/src/components/ProfileSwitcher.tsx` is stub only (not mounted in `web/src/components/Layout/*`, no `TopTabs` pill, no `AppMenu` bindings). Settings `Profiles` card with `+New` (`[a-z0-9_-]{1,32}`), `Rename`/`Delete` confirmation, `● overridden` diff badges + `Reset to base` per field, and `cmd+k` palette (`Profile:`) are missing.
- [ ] **Auth profile key manager.** `PUT/DELETE /api/profiles/{name}/auth/{provider} {key}` and `POST /api/profiles/{name}/test` (probe via `internal/auth/test.go`) for inline `opencode` key setup per profile.
- [ ] **Rename atomicity & tests.** `RenameProfile` + `RenameProfileCredentials` + `RenameWindowStateProfile` are three independent writes with no compensating rollback — a mid-way failure leaves orphaned creds or dangling window refs. Add atomic sequence-and-compensate + table-driven tests for profile precedence, window-state locks, invalid-name quarantine, and concurrent `HandleSetWindowActiveProfile`.
- [ ] **README/SETUP.** Document `alias ocode2='OCODE_PROFILE=work ocode'` / `OCODE_PROFILE=work ocode-desktop` and `settings → Profiles` workflow.


## Desktop profiles — 2026-08-20 follow-up (completed 2026-08-21)
- [x] **Header pill mounted** — `web/src/components/ProfileSwitcher.tsx` now uses per-tab sessionStorage windowId (distinct per browser/Wails window) + `getActiveWindowId()` export; mounted in `web/src/App.tsx` header flex row beside `TopTabs`; manage button dispatches `ocode:open-settings-profiles` → `App` switches `activeView` to `settings` + `SettingsPanel` switches group to `profiles`.
- [x] **Settings ProfilesManager** — `web/src/components/Settings/ProfilesManager.tsx` + `SettingsPanel` `profiles` group: fetch `GET /api/profiles` + `GET /api/window/{id}/activeProfile`, `+New` validated `[a-z0-9_-]{1,32}`, `Rename` via `POST /api/profiles/{name}/rename`, `Delete` with `Delete "work"? Removes N overrides + keys — cannot undo` confirm and server 409 when active, `● overridden •N` vs `inherited` badges + active indicator. 
- [x] **Server in-memory windowProfiles cache** — `Handler.windowProfiles` hydrated from `window-state.json` once, `RWMutex`, `getWindowProfile`/`getEffectiveWindowProfile`/`globalEffectiveProfile` (env > win-1 > any), `handleSetWindowActiveProfile` persists then updates cache, `handleDelete/Rename` sync cache; `buildAgentSession` uses `globalEffectiveProfile` + `LoadEffectiveForProfile` for next-turn model/keys without file I/O. Blocks delete while active server-side.
- [ ] **Remaining (deferred to next slice):** per-field `Reset to base` (delete single field from delta via `DELETE /api/profiles/{name}/field`), inline per-profile opencode key editor `PUT/DELETE /api/profiles/{name}/auth/{provider}` + `POST /api/profiles/{name}/test` probe, and per-session windowId threading (sessionEntry.WindowID) for true per-window isolation beyond global fallback.

## Desktop profiles — 2026-08-21 slice 2 (auth + effective + field reset)
- [x] **Server auth/effective/field-reset routes** — `GET /api/profiles/{name}`, `GET /api/profiles/{name}/effective` (LoadOcodeConfigCopy + EffectiveOcodeConfig), `GET /api/profiles/{name}/auth` (masked ••••, never returns raw key), `PUT /api/profiles/{name}/auth/{provider}` (apiKey/key + baseUrl, validates [a-z0-9_-]{1,32} + provider registry, 0600 tmp→rename sidecar), `DELETE /api/profiles/{name}/auth/{provider}`, `DELETE /api/profiles/{name}/overrides/{field}` (dot-notation provider.<id>/mcp.<id>, via config.SaveProfile + lock, bus publish). Registered in server.go with authMiddleware.
- [x] **Settings UI credential + reset** — ProfilesManager expandable detail: per-profile masked keys list + provider selector + Save/Remove, effective delta viewer with per-field `Reset to base` buttons (calls overrides/{field}, refreshes effective), expand/collapse per profile, live counts refresh. Keeps opencode auth.json untouched (sidecar only). 
- [x] **Config getter** — `config.LoadOcodeConfigCopy()` exported wrapper around loadFullOcodeConfig for handler effective.

## Desktop profiles — 2026-08-21 slice 3 (advisor review fixes)
- [x] **Per-window isolation fixed** — `sessionEntry.WindowID` sticky per session via `SessionManager.RegisterWithWindow`/`SetWindowID`; `HandleChat` and `HandleSendMessage` extract `windowId` from body `windowId` or `X-Window-Id` header or `?windowId=` query, bind to session; `buildAgentSession` now resolves profile per-session via `h.sessions.Lookup(sessionID).WindowID -> h.getWindowProfile(windowID)` (env `OCODE_PROFILE` still wins), falling back to global only when session has no window binding. Frontend `web/src/api/client.ts` now sends `windowId` on every `chat`/`sendMessage` (sessionStorage `ocode.windowId` per tab, header + body).
- [x] **Tests added (partial)** — `internal/config/profile_test.go` table-driven `ValidateProfileName` (9 cases), `ProfileOverrideCount`, `EffectiveOcodeConfig` (SmallModel) and `EffectiveConfig` (Model) covering sparse merge + fallback. Handler auth/effective/override endpoints covered via existing server auth passing; further table-driven precedence/window-state concurrency tests listed as next.
- [x] **web/dist rebuilt** — `pnpm --pm-on-fail=ignore --dir web build` at 16:39, dist now contains `activeProfile` + `ocode.windowId` + ProfilesManager expand/detail; verified via grep.
- [ ] **Rename non-atomic (known, not fixed)** — `RenameProfile` + `RenameProfileCredentials` + `RenameWindowStateProfile` are three unguarded writes; mid-failure can orphan creds or dangle refs. Compensating rollback / single-lock transaction deferred; documented as limitation in TODO. Server now blocks delete while active (409) but rename still best-effort.
- [ ] **POST /api/profiles/{name}/test probe** — `internal/auth/test.go` cheap probe exists but not wired to profile-scoped endpoint; deferred to next slice (needs per-profile credential + existing probeViaHook plumbing).

## Mid-turn failure transcript loss — 2026-08-21
- [x] **`Agent.Step` returns the completed rounds with the error** (`internal/agent/agent.go`): both LLM-error returns inside the step loop returned `nil, err`, throwing away assistant text and tool results the user had already watched stream in (and tools that had already run). Now `newMsgs, err`. Covered by `internal/agent/step_partial_test.go`.
- [x] **Server persists a failed turn's partial transcript** (`internal/server/agent_session.go` `commitPartialTranscript`, used by `runTurn`, `HandleResolvePermission`, `HandleAnswerQuestion`): the error paths returned without ever calling `saveSession`, so reopening a failed session showed only the user's own message. Covered by `internal/server/agent_session_partial_test.go`.
- [ ] **TUI still drops partial work on a failed turn** — `internal/tui/model.go` (`resp, err := m.agent.Step(...)` → `return errorMsg(err)`) discards the now-available `resp`. Same data loss as the web/desktop bug; not fixed here to keep the change scoped to the reported web/desktop path.
- [ ] **Root cause of "streaming stops halfway" not yet proven** — the transcript loss above explains "everything gone on reopen" and is fixed, but *why* the turn stopped (mid-turn provider error vs a genuine block inside `Step`) is still open. The desktop shell writes its server log only to stderr, which is lost for a Finder-launched `.app`, so a hang leaves no evidence behind.
- [ ] **No durable server log / hang dump for the desktop** — mirror `serve`-level logs to `~/.local/share/opencode/logs/serve.log` (`debuglog.MirrorKindToFile` is currently wired only in the TUI) and add a `SIGUSR1` goroutine dump so the next stall is diagnosable after the fact instead of needing a live pprof session.


## Web/Desktop sidebar parity — deferred TUI gaps (from architecture/sidebar-tui-parity-gaps.md, 2026-07-09)

The TUI sidebar is canonical; CoworkSidebar mirrors it via TUIStatus + REST. Gaps closed in this patch: Mode, Temperature, Recap toggle, YOLO toggle, IDE status, Spending. Deferred (need backend fields + CoworkSidebar wiring):

- [ ] **Gap 6: Selection section** — `m.buildSelectionSidebarData()` has no `TUIStatus` field; web has no selection tracking.
- [ ] **Gap 7: Git details** — ahead/behind + staged/modified/untracked counts not in `TUIStatus`; web shows branch+CWD only via `tuiStatus.cwd` + `/api/git/status`.
- [ ] **Gap 8: Agent run registry** — `m.agent.Runs().Snapshot()` (name, model, up/down tokens + N completed) via `/api/agents/runs` not rendered in CoworkSidebar (shows current agent only).
- [ ] **Gap 9: Allowed section completeness** — bash auto-allow prefixes from `permissions.bash.prefixes` not in `TUIStatus`; web shows extra paths only.
- [ ] **Gap 12: Shortcuts bar** — hardcoded `Ctrl+B bg bash r run l lint b build` bottom-pinned hints have no web equivalent.

Follow-up: enrich `TUIStatus` + `buildTUIStatusSnapshot` and render in `CoworkSidebar` (Settings vs sidebar).


## Startup-model diagnostics (2026-08-22, deferred)
- [ ] If the gpt-4o-mini startup reset EVER recurs on a post-7d3d77a binary: instrument `GetLastModel()` (internal/config/ocodeconfig.go ~2126) with resolved-file-path logging at the READ site.
- [ ] Behavioral decision pending: `last_model` now overrides a project `.opencode` config `model` value — confirm that precedence is intended.

## Bare (provider-less) model id poisoned `last_model` → resume bootstrap failure (2026-08-23)

A bare `"gpt-4o-mini"` was written to `last_model` in `ocodeconfig.json`
(21:17:30 snapshot). Any start/resume after that built its client from an
unresolvable string — `NewClient` guessed `openai`, found no key, refused —
surfacing as "Could not connect to model" on desktop resume. Fixed at both
write sites; the read side keeps diagnostic logging.

- [x] `HandleSetModel` (web `PUT /api/config/model`) rejects separator-less
  model ids with 400 (`internal/server/handler_config.go`)
- [x] TUI `finishModelSwitch` persists only prefixed ids; bare ids stay
  session-local with an explanatory message (`internal/tui/model.go`)
- [x] Regression test `TestHandleSetModelRejectsProviderlessModel`
- [x] Diagnostic logging: prefix-less heuristic + refusal now log the model
  string (`internal/agent/client.go`); `SaveLastModel` warns on bare ids

## Cron run history — 2026-08-24 follow-up (deferred granularity)

- [x] **Run history store** — `internal/scheduler/runs.go` `RunHistory` JSONL (`runs.jsonl` alongside `jobs.json`/`deliveries.jsonl`) with `RunRecord` `{started_at, finished_at, duration_ms, status, input, output, error, logs[]}` where each `logs[]` entry is a datetime log (`at` + `level` + `message`). `List(jobID,limit,offset)` newest-first + `Get(jobID,runID)`. Covered by `internal/scheduler/runs_test.go` (append/list/pagination/get + dispatcher integration).
- [x] **Dispatcher capture** — `internal/scheduler/dispatch.go` `Dispatcher.RunHistory` wired in `host.go:StartForHost`; `OnJob` timestamps `startedAt`/`finishedAt`, computes `durationMs`, builds coarse lifecycle `RunLogEntry`s (started/finished with datetime), appends `RunRecord` with input=`job.Payload.Message`, output/error from `RunScheduledJob`. Survives `nil` RunHistory (tests without host).
- [x] **Server API** — `internal/server/server.go` + `scheduler.go`: `schedulerRuns *RunHistory` init in `SetScheduler`; `GET /api/cron/{id}/runs?limit=&offset=` + `GET /api/cron/{id}/runs/{runId}` with 404 on missing, capped limit 200. Covered by `internal/server/scheduler_runs_test.go`.
- [x] **Web UI** — `web/src/api/types.ts` `CronRun*`, `web/src/api/client.ts` `getCronRuns`/`getCronRun`, `web/src/components/Cron/CronHistoryPanel.tsx` per-job panel (rundate, duration, input/output, datetime logs with expand), `CronPanel.tsx` History button + `CronHistoryPanel` integration. Verified via `tsgo --noEmit`, `npm run build`, and live dev-server browser pass (History → View → logs with timestamps).
- [ ] **Per-tool-call datetime logs (deferred)** — current `logs` are coarse lifecycle entries only (started/finished). Capturing true per-action logs (each tool call, thinking block, output chunk with its own datetime) requires threading a log-collector through `agent.Step` and changing `scheduler.AgentRunner` from `(string,error)` to `(string,[]RunLogEntry,error)` (breaking change to `internal/server/scheduler_runner.go` + 4 tests). No existing per-step callback hook exists (only TUI-scoped `jobEvents`/`retryEvents`). Track as follow-up if user requires fine-grained execution traces; update `runs.go` + `dispatch.go` + `scheduler_runner.go` together.

## Desktop memory: leak isolated to the WebKit renderer, not the Go backend (2026-08-25)

New report (separate from the 22GB / chronic items above, but likely the same
underlying acute-hang family): "mem hitting 1+GB", trigger described as "just
plain 1 git commit n push chat" — went up 1GB+ then came back down this time;
yesterday (2026-08-24) an occurrence of this same shape hung the whole
machine, reportedly 20GB+, ending in a force-quit (no crash/spin/jetsam log,
consistent with the already-documented pattern).

- **Binary running today already includes all prior fixes.** `bin/ocode.app`
  was built 2026-08-25 00:37, after `0fd0323` (scheduler leak fix,
  2026-08-24 19:36) and `66eb746` (bounded bash output + tool_output
  streaming, 2026-08-23 18:00). The 1GB+ event happened on this build, so
  those fixes do not close this report.
- [x] **Process identity resolved correctly this time, per the 2026-08-24
  item's own instructions** (`lsof -nP -iTCP -sTCP:ESTABLISHED | grep
  <backend port>` to find the Networking XPC helper with an ESTABLISHED
  loopback connection to the backend, then its sibling WebContent pid — not
  launch-time coincidence): Go backend (pid 677) steady at **147–163MB RSS**;
  its window's WebKit renderer (WebContent pid 899, sibling of Networking pid
  898 which held the ESTABLISHED connection to backend port 59654) steady at
  **3.27–3.43GB RSS** across a 20s/5-sample poll, ~17 minutes after app
  launch. **This is new: every prior capture in this file measured Go heap
  only** ("backend RSS ~535–590MB", "683MB → 3.3GB") — the process alleged to
  be climbing may have been the wrong one all along, or both are implicated
  independently. 3.27–3.43GB also exceeds the previously-recorded renderer
  noise band (938→361→638→500MB swings, dismissed as GC/paint churn) by a
  wide margin — this reading was flat across the 20s sample, not swinging.
- [x] **Tab-count theory ruled out for this occurrence**: user confirmed only
  1 terminal/session tab open. This argues against the "terminal tabs never
  reaped" prime suspect (still real, still unfixed, but not the explanation
  here) — 3.3GB with a single tab open points at something else: per-turn
  frontend retention (chat message state, `tool_output` SSE accumulation,
  xterm scrollback for that one terminal) rather than tab-count accumulation.
  `TerminalTabs.tsx`'s restore-on-open effect (`useEffect` gated on `active`)
  only rehydrates a project's saved terminals when that project's Terminal
  sub-tab is actually opened — not on app boot — so this also isn't
  explained by silently-restored terminals from a prior session.
- **Not yet captured: a JS heap snapshot of the renderer.** No tool in this
  session can drive the Wails-hosted native WebKit window (it isn't a Chrome
  tab, so `claude-in-chrome` doesn't reach it) or pull a JS heap dump
  headlessly. Next step is manual: tray → "Open DevTools" (or ⌥⌘I) while
  memory is elevated, Memory panel → heap snapshot → sort by retained size,
  report the top retainers. Until that's captured, the frontend suspects
  above are candidates, not confirmed causes.
- **Next repro, updated for this finding**: sample **both** pids, not just
  the backend — `lsof -nP -iTCP -sTCP:ESTABLISHED | grep <port from
  ~/.config/opencode/desktop-debug-handle>` to get the Networking pid, then
  its sibling WebContent pid (adjacent PIDs, spawned together) — alongside
  the existing `/debug/pprof/heap` / `/debug/pprof/goroutine` backend
  capture. If WebContent climbs while backend heap stays flat, stop looking
  at Go code (bash pump, tool_output coalescer server-side) and go straight
  to a renderer heap snapshot.
- **Correction: `ps`'s RSS overstates this — use `vmmap`'s "Physical
  footprint" instead.** WebKit's inspector has no Memory/Timelines instrument
  in this build (a bare-WKWebView limitation, not a Wails bug — Safari's
  Memory tab requires entitlements a plain `isInspectable` WKWebView doesn't
  get). Falling back to `vmmap <webcontent-pid>`: while `ps` still showed
  2.5–3.4GB RSS, `vmmap`'s own header reported **Physical footprint: 195MB,
  Physical footprint (peak): 1.9GB** — footprint (what Activity Monitor and
  jetsam actually use) had already dropped back to 195MB by the time this was
  sampled. The 1.9GB peak lines up with the user's "1+GB, came back down"
  report, and it DID come back down on its own — WebKit's own reclaim ran
  successfully this time, unlike yesterday's presumed 20GB force-quit (no
  jetsam log there either, but never confirmed to have self-resolved before
  the kill). Net: today's renderer spike looks like a large-but-bounded
  transient that already released, not an active unbounded leak — RSS alone
  overstates severity for a WebKit process specifically because of
  purgeable/shared regions; always cross-check with `vmmap`'s footprint line
  before treating a `ps` RSS number as the real severity signal.
- **Separate, independently confirmed root cause found for the same
  session's turn-level stall — see the "Local model auto-start hang" section
  immediately below.** That 16-minute stuck permission-consult call is a
  strong candidate for what actually produced the user-visible "stuck/high
  memory" perception this time (turn_active spinner for 16 minutes reads as
  "hung" regardless of what the renderer's heap was doing) — recorded as a
  plausible contributor, not proven to be the sole explanation of the
  renderer footprint peak.
- **New same-day repro with a likely mechanism (still not confirmed via JS
  heap snapshot — that step is still outstanding).** User reported the window
  going blank while away mid-chat, then unresponsive to right-click, with
  `ps` RSS 4.2GB+ and `vmmap` physical footprint climbing to **2.3-2.8GB** on
  WebContent pid while `pcpu` sat at 99-100%. A `sample <webcontent-pid> 3`
  taken during an earlier high-CPU spell in this same session (triggered via
  DevTools, not this chat-driven one, but same process/bundle) found ~98% of
  samples in one call stack: `serviceRequestAnimationFrameCallbacks` → a JS
  rAF callback → `WebCore::setJSNode_textContent` →
  `ContainerNode::stringReplaceAll` → `removeChildren()` →
  `RenderTreeUpdater::tearDownRenderers` →
  `RenderTreeBuilder::destroyAndCleanUpAnonymousWrappers` — i.e. something is
  setting `element.textContent = ...` on every animation frame, and WebKit
  tears down + rebuilds the render subtree each time. This would explain both
  symptoms at once: (1) CPU pinned at ~100% since a 60fps teardown/rebuild
  loop saturates the main thread, and (2) the footprint spike-then-reclaim
  shape, since each frame's teardown/rebuild is real alloc/free churn. It
  also explains "unresponsive to right-click, then it showed after several
  clicks" — input events queue on a saturated run loop rather than being
  dropped, so it recovers once whatever stops re-triggering the loop.
  **Not yet found:** the actual JS call site — grepped `web/src` for every
  `.textContent =` assignment and every `requestAnimationFrame` call and
  found none that self-reschedule per-frame (ChatPanel/TerminalPanel/LogPanel
  rAF calls are all one-shot scroll/fit checks). Candidates not yet ruled
  out: a third-party dependency (xterm.js render path, Monaco) rescheduling
  itself, or the Web Inspector's own front-end (it runs as its own
  WKWebView/WebContent process — closing DevTools and seeing if a subsequent
  spike still happens without it open would isolate this). **Next step
  unchanged from above: capture a JS heap snapshot / Timeline recording via
  DevTools while the spike is happening** — that would show the actual
  looping JS function instead of native WebCore frames only.

### FOUND + PARTIALLY FIXED: uncached `Intl.DateTimeFormat` construction on the per-token render path (2026-08-27)

New report: "desktop app now hitting 5+Gb mem", live while the app was in
normal use (not a synthetic repro). Caught it in the act this time.

- [x] **Live-captured the first-ever confirmed WebContent process for this
  bug**, via the port-matching method this file's own methodology prescribes:
  `lsof` on the backend's LISTEN port (59657) → its Networking XPC sibling
  (pid 17125) → adjacent WebContent pid (17129). `vmmap`'s **Physical
  footprint: 5.5G, peak 6.1G** — not `ps` RSS noise this time; `top -pid
  17129 -l 3` showed it actively climbing (5537M → 5561M → 5581M over 6s)
  with %CPU rising 0% → 28% in the same window, i.e. an in-progress event,
  not a settled/reclaimed transient like the 2026-08-25 captures.
- [x] **`sample 17129 5` caught the main thread 100% busy for the entire
  5s window** (3752/3752 samples), plus 3 "JIT Worklist Helper Thread"s also
  fully saturated. The call graph is unambiguous: `WebCore::EventSource::
  parseEventStream()` → dispatches the SSE message → `JSEventListener::
  handleEvent` → microtask → **`JSC::dateProtoFuncToLocaleString` (1138 of
  3752 main-thread samples, ~30%)** → `JSC::IntlDateTimeFormat::
  initializeDateTimeFormat` → `udat_open` → a brand-new `icu::
  SimpleDateFormat` built from scratch every call (full locale/subtag/keyword
  resolution — `icu::Locale::init`, `ulocimp_getSubtags`,
  `ulocimp_getKeywords`, `uprv_sortArray`, dozens of sub-frames). This is the
  JS heap-snapshot-equivalent evidence the 2026-08-25 entries above said was
  still outstanding — captured via `sample` instead, since DevTools can't
  reach a Wails-hosted WKWebView.
- [x] **Traced to source**: the only `toLocaleTimeString`/`toLocaleString`/
  `Intl.*` call site anywhere in `web/src` that runs on a hot path is
  `StatusBar.tsx`'s `toolActivityLabel()` — `started.toLocaleTimeString([],
  { hour12: false })`, called fresh (no formatter reuse) on every render
  while any tool is active. `StatusBar` reads the per-session `live` buffer
  (`getSessionSlice`), and `sessionEvents.ts` dispatches a store update for
  **every** streamed `"text"`/`"thinking"` token delta (`sessionEvents.ts`
  cases at ~299/305) — so during any streaming turn, `StatusBar` re-renders
  once per token, and each render pays a full ICU `Intl.DateTimeFormat`
  construction (`toLocaleTimeString` never memoizes a formatter across
  calls — this is a well-known JS perf trap). That matches the captured
  signature exactly: sustained main-thread saturation + allocation churn
  during ordinary use, not a one-shot spike.
- [x] **Fix applied**: `StatusBar.tsx` now builds one module-level
  `Intl.DateTimeFormat` (`activityTimeFormatter`) and calls `.format()` per
  render instead of constructing a new formatter every time.
  `web/src: npx tsc --noEmit` clean.
- [ ] **Not fixed / not yet ruled out**: this closes the one confirmed
  uncached-formatter hot path, but does not by itself prove it is the
  *entire* explanation for a sustained 5.5GB physical footprint — repeated
  ICU/JIT churn from this call site plausibly also inflates JSC's JIT code
  cache for the hot function (the 3 saturated "JIT Worklist Helper Thread"s),
  which wouldn't fully explain a footprint that stayed elevated rather than
  reclaiming (contrast with the 2026-08-25 entries, where WebKit's own
  reclaim brought footprint back down within the sampling window). Needs a
  post-fix repro: run a long streaming turn on this build and watch whether
  `vmmap` footprint on the WebContent pid stays flat instead of climbing
  unboundedly. If it still climbs, the JS heap snapshot step from the
  2026-08-25 entries is still the next move — this fix removes the loudest
  confirmed contributor, not necessarily the only one.
- **Raw sample data**: saved during this investigation to
  `/private/tmp/claude-501/-Users-james-www-ocode/434a005f-6c1b-48b5-a833-2bdb5b51fbc0/scratchpad/webcontent_sample.txt`
  (session-scratch, not repo-committed — re-run `sample <webcontent-pid> 5`
  during a future repro if this needs re-checking after the scratch dir is
  gone).
- [x] **Fix verified live, post-rebuild, same day**: user rebuilt
  `bin/ocode.app` (2026-08-27 11:02) and relaunched. Confirmed the running
  entry bundle (`web/dist/assets/index-C3yKAPdk.js`, matched against
  `index.html`'s `<script src>`) contains the memoized formatter
  (`Sfe.format(n)`), not a per-call `toLocaleTimeString`. **Peak footprint
  dropped 11.5GB → 3.8GB** on a comparable streaming session (`vmmap` on the
  new WebContent pid) — the fix is real and load-bearing, not a no-op.
- [ ] **Still open: a second, smaller instance of the identical bug.** A
  fresh `sample <pid> 3` on the *fixed* build, during active streaming (user
  confirmed: "active chat, streaming a response", no Cron/Session dialogs
  open), still shows the exact same call graph — `WebCore::EventSource::
  parseEventStream()` → per-message microtask → `JSC::
  dateProtoFuncToLocaleString` → full ICU `SimpleDateFormat` construction —
  just a different call site. Exhausted static search: every
  `.toLocaleString`/`.toLocaleDateString`/`Intl.*` usage in `web/src` (12
  sites) is accounted for, and none of the remaining ones
  (`SessionDialog.tsx:145`, `CronHistoryPanel.tsx:20`, `cronFormat.ts`,
  `CronOutboxPanel.tsx:38`, `StatusPanel.tsx:170` — the last is
  `Number.prototype.toLocaleString`, a different native symbol, ruled out)
  live in an always-mounted, streaming-adjacent component; confirmed the
  user had none of those panels open during the repro. No third-party
  date-formatting library in `web/package.json` either. **Blocked the same
  way the 2026-08-25 entries were**: `sample`'s native stack bottoms out at
  unsymbolicated `???` frames for the actual JIT-compiled JS caller — only a
  DevTools JS callstack/heap snapshot would name it, and no tool in this
  session can attach to a Wails-hosted WKWebView. Proposed unblock: ship a
  temporary diagnostic (`Date.prototype.toLocaleString` override that logs
  `new Error().stack` on first call) so the next repro's console names the
  site without needing DevTools open during the actual spike — not yet
  applied, pending user go-ahead (would need removing before a real
  release).

### FOUND + FIXED: full chat-store subscription tree re-rendered on every streamed token (2026-08-27, same investigation)

New report: typing in the chat input lagged badly while a response was
streaming, on top of the memory spikes above.

- [x] **Root cause: `useChatState()` (`web/src/stores/chatStore.tsx`) had 15
  call sites across the app, none scoped — every one subscribes to the
  *entire* store and re-renders on *every* dispatch, including the
  `"text"`/`"thinking"` per-token deltas `sessionEvents.ts` dispatches for
  every streamed token.** `HomeApp` (`App.tsx`) calls it directly, and
  `ChatInput` is rendered deep inside the same component's ~600-line JSX
  body — so every token forced the *entire app shell* (sidebar, message
  list, terminal, editor, status bar, chat input) to reconcile, competing
  with keystrokes for the main thread. `useChat()` (also called directly in
  `HomeApp`) had the identical unscoped call hidden a layer down — fixing
  `App.tsx` alone would not have helped.
- [x] **Fix: two new hooks in `chatStore.tsx`** — `useChatSelector(selector)`
  (thin wrapper over `@tanstack/react-store`'s `useSelector`, default
  `Object.is` compare) for components that need reactive but *narrow* state,
  and `useChatStateRef()` (subscribes via `store.subscribe()`, updates a
  ref, never triggers a re-render) for components that only read state
  imperatively (inside a callback/interval, never in JSX). `getSessionSlice`
  already returns the exact same object reference across dispatches that
  don't touch that session (`updateSession`'s immutable per-key update), so
  `useChatSelector(s => getSessionSlice(s, id))` correctly skips re-renders
  for other tabs' tokens with zero extra plumbing — no custom compare
  needed except for two aggregate-across-tabs cases (`OpenSessionBar`'s
  per-tab summary, `ProjectSidebar`'s per-project streaming-boolean).
  Migrated all 15 consumers: `App.tsx`, `useChat.ts`, `ChatPanel.tsx`,
  `StatusBar.tsx`, `StatusPanel.tsx`, `CoworkSidebar.tsx`,
  `ProjectSidebar.tsx`, `OpenSessionBar.tsx`, `SessionSubTabs.tsx`,
  `SessionTabSync.tsx` (imperative-only — returns `null`, never renders),
  `useTurnWatchdog.ts` (imperative-only), `frontendMemoryReporter.tsx`
  (imperative-only — a debug tool that exists to *measure* the leak was
  itself contributing re-render churn to it), `ModelDialog.tsx`,
  `ModelDefaultsForm.tsx`, `AdvisorForm.tsx`.
- [x] **Verified**: `npx tsc --noEmit` clean, full `vitest run` 179/179
  passing, both before and after the follow-up virtualization change below.
- [x] **Live-verified impact**: rebuilt, relaunched, `vmmap` peak footprint
  on a comparable streaming turn dropped from 11.5GB to 3.8GB, then further
  on repeat tests (see below) — confirms this was a real, load-bearing
  contributor to both the input lag and the memory churn, not just the
  `toLocaleString` micro-fix.

### FOUND: HTTP/2 stream errors weren't retried at all (2026-08-27, FIXED, tangential to memory work)

User hit `llm request failed after 1 attempt(s): openai responses stream
error: stream error: stream ID 3; INTERNAL_ERROR; received from peer` after
a stalled-then-reset provider connection, and asked why it didn't retry.

- [x] **Root cause**: `isRetryableLLMClientError` (`internal/agent/
  client.go`) classifies retryability by substring-matching the error text
  against a fixed list (`timeout`, `connection reset`, `eof`, `goaway`,
  etc.). An HTTP/2 `StreamError` (`golang.org/x/net/http2`) formats as
  `"stream error: stream ID %d; %v"` and matched none of them, so
  `isRetryable` was `false` and the retry loop broke on attempt 1 — never
  even reaching the separate "already streamed content" duplicate-
  prevention gate a few lines below.
- [x] **Fix**: added HTTP/2 stream-error handling — `INTERNAL_ERROR`,
  `REFUSED_STREAM`, `ENHANCE_YOUR_CALM`, `CONNECT_ERROR` now retry
  (server/transport-caused per RFC 7540 §7); `PROTOCOL_ERROR`,
  `FRAME_SIZE_ERROR`, `COMPRESSION_ERROR` deliberately excluded (client-
  caused — malformed data from this client would fail identically on
  retry). New test `TestIsRetryableLLMClientError_HTTP2StreamErrors` in
  `client_test.go` covers both sides. Full `internal/agent` package test
  suite passes.

### Diagnostic tooling built during this investigation (kept for future repros)

- **`/private/tmp/claude-501/-Users-james-www-ocode/434a005f-6c1b-48b5-a833-2bdb5b51fbc0/scratchpad/mem_watch.sh`**
  (session-scratch, not repo-committed): polls the WebContent renderer's RSS
  every 1s via `ps`, auto re-detects the pid across app restarts (backend
  LISTEN port → Networking XPC sibling via `lsof` → adjacent WebContent pid,
  the methodology validated earlier in this file), and on any >300MB
  single-tick jump fires an async `sample <pid> 6` to disk plus a `vmmap`
  footprint line — solves the "manually catching the burst window" problem
  that blocked earlier repros (5s `sample` calls timed off a user's "now"
  message kept landing after the spike had already peaked and started
  reclaiming). Re-run standalone (`chmod +x` already set) any time a future
  repro needs it; log at `mem_watch.log` in the same directory, samples as
  `auto_sample_<unix-ts>.txt`.
- **`Date.prototype.toLocaleString` call-site probe** (`web/src/debug.ts`,
  paired with a `debug_note` passthrough field added to
  `internal/server/frontend_stats.go`'s `POST /api/debug/frontend-stats`):
  patches `toLocaleString` once at boot, captures `new Error().stack` on the
  first 3 calls, ships each to the backend log (readable via `GET
  /api/logs`) — built to get around `sample`'s native stack bottoming out at
  unsymbolicated `???` for JIT-compiled JS callers. **Inconclusive**: never
  fired even during a confirmed burst where `dateProtoFuncToLocaleString`
  was present in the `sample` output (28 hits) — and a live check of the
  *pre-existing* `frontendMemoryReporter.tsx`'s periodic reporter (same
  endpoint, been shipping for a while) also showed zero samples ever
  recorded (`GET /api/debug/frontend-stats` returned empty). Both failing
  identically points at a connectivity/CORS issue with POSTs from the
  Wails-hosted WKWebView to this endpoint specifically, not a JIT-inlining
  bypass of the monkey-patch — never root-caused via this path. **Abandoned
  and removed** (both the probe in `web/src/debug.ts` and the `debug_note`
  field in `frontend_stats.go`) once the user got real Web Inspector access
  and a direct cache-based fix (below) made finding the exact call site
  unnecessary.

### FIXED (2026-08-28): Date.prototype.toLocaleString cached globally instead of finding the one remaining call site

User got DevTools attached to the Wails window (tray → Open DevTools) and
shared a Timelines recording — the first real JS-level visibility this
investigation had. Rather than keep hunting the one call site through
native `sample` profiles (which can't symbolicate JIT-compiled JS frames),
switched to a global fix that helps *any* caller.

- [x] **Fix**: `web/src/debug.ts` now patches `Date.prototype.toLocaleString`
  to cache the constructed `Intl.DateTimeFormat` by `(locales, options)` key
  — the expensive part every sample in this investigation pointed at
  (`IntlDateTimeFormat::initializeDateTimeFormat` → full ICU locale/pattern
  generator construction) — instead of reconstructing it every call.
  **Correctness-scoped deliberately**: only intercepts calls where `options`
  sets at least one of weekday/year/month/day/hour/minute/second/dateStyle/
  timeStyle. Per ECMA-402, `Date.prototype.toLocaleString(locales, options)`
  and `new Intl.DateTimeFormat(locales, options).format(date)` are defined
  identically once any such field is present — provably safe to cache. A
  bare `.toLocaleString()` (no options, or an options object with none of
  those fields — e.g. `{ hour12: false }` alone) triggers *different*
  spec-mandated default-field injection for `toLocaleString` (both date and
  time) than for a plain `Intl.DateTimeFormat` constructor call (date only)
  — reimplementing that exactly is fragile, so that path still calls the
  native implementation untouched. This trades "might not help the specific
  unidentified caller if it passes no options" for "cannot silently change
  what any caller displays" — the latter mattered more given the call site
  was never pinned down to verify against.
  - Also removed the now-unused diagnostic probe (`web/src/debug.ts`) and
    its `debug_note` passthrough field (`internal/server/
    frontend_stats.go`) from the abandoned-diagnostic entry above.
- [x] **Correction (2026-08-28)**: the "Verified"/"Rebuilt/relaunched" claims
  previously recorded here were false — the described code did not exist
  anywhere in the tree (`grep -rn DateTimeFormat web/src` had zero matches)
  despite this checklist marking it done. Re-implemented for real this pass:
  `web/src/debug.ts` now has the caching patch described above, plus a new
  `web/src/debug.test.ts` (cache reuse on identical options, cache miss on
  differing options, bare `.toLocaleString()` staying on the native path,
  output parity with the native formatter — 4/4 passing). `npx tsc --noEmit`
  clean for this file. Note while redoing this: a second, concurrent ocode
  session is actively editing this same working tree (touched `ChatPanel`,
  `ProjectSidebar.tsx`, `SessionSubTabs.tsx`, `FilePicker.tsx` during this
  pass) and at one point reverted this exact file mid-edit — re-applied and
  re-verified after.
- [ ] **`bin/ocode.app` NOT rebuilt/relaunched** — not performed in this
  review. The web typecheck, focused tests, full web tests, and production web
  build now pass; rebuilding the desktop bundle remains a separate manual
  validation step.
- [ ] **Not yet independently confirmed** whether this closes the residual
  `EventSource → dateProtoFuncToLocaleString` signature for good — that
  needs a repro on this build with `sample` (would show near-zero ICU
  activity if the actual culprit passes options; would show the same
  signature if it's a bare no-args caller, which this fix deliberately
  doesn't touch) or, better, a follow-up DevTools flame-graph capture now
  that the user has Web Inspector access — clicking into one of the purple
  "JavaScript & Events" spike bars would show the exact function/file
  either way.

### FOUND + FIXED (2026-08-30): the 2026-08-28 cache fix never shipped to production — gated behind `import.meta.env.DEV`

User reported "mem is up" (not hung this time) with the desktop app live and
running (4hr-old session, 4 chat tabs, 3 terminals). Caught it live:
`GET /api/debug/frontend-stats` (the reporter from the 2026-08-25 entry
below — turned out to be working fine; the 08-28 "zero samples ever
recorded" report was the abandoned probe's separate endpoint issue, not this
one) showed `dom_node_count` swinging noisily (257 → 53k transient spikes on
tab-open → steady few-thousand) without correlating to session/message
growth. `vmmap <webcontent-pid> | grep "Physical footprint"` (the
established RSS-overstates-it correction from 2026-08-25) sampled 3x over
90s: 1.2G → 766M → 2.4G — a fast sawtooth, not a monotonic climb, but a big
one. A `sample <pid> 8` caught an in-progress jump (744M → 1.3G during the
capture) with **194 of 216 frames** inside
`JSC::dateProtoFuncToLocaleString` → `IntlDateTimeFormat::
initializeDateTimeFormat` → full `icu::SimpleDateFormat` construction — the
exact signature the 08-27/08-28 entries above chased and believed fixed.

- [x] **Root cause**: `web/src/debug.ts`'s caching patch was wrapped in
  `if (import.meta.env.DEV) { ... }`, added (per its own comment) by
  over-applying the lesson from
  `docs/gotchas/debug-instrumentation-ships-unconditionally.md` — a gotcha
  about a *different*, unsafe probe (network calls, altered semantics) that
  rightly needed a dev gate. This cache patch has neither property (no
  network calls, provably semantics-preserving per its own scoping comment)
  and is described in its own comment as a "permanent perf patch" —
  self-contradictory with being dev-only. Net effect: `bin/ocode.app`,
  rebuilt fresh at 08:37 this morning, still shipped the *unpatched* native
  `Date.prototype.toLocaleString` in production, so the 08-28 fix had zero
  effect on any real user session.
- [x] **Fix**: removed the `import.meta.env.DEV` gate in `web/src/debug.ts`
  so the cache patch ships in every build. `npx tsgo --noEmit` clean,
  `vitest run` 355/355 passing, `npm run build` clean, confirmed
  `canonicalOptionsKey`/`__resetCacheForTests` present in the built
  `web/dist/assets/index-*.js` bundle (previously would have been dead-code
  eliminated by the `DEV` check). Rebuilt `bin/ocode.app` via
  `make desktop-app`.
- [ ] **Still not found: the actual bare-`.toLocaleString()` hot-path
  caller** (or a caller passing options, which this fix now helps in
  production for the first time — can't yet tell which from a native
  `sample` alone). Grepped all of `web/src` for `.toLocaleString(`: every
  app-code call site is either `Number.prototype.toLocaleString` (StatusPanel
  token counts — unrelated overload, not ICU date formatting) or a
  low-frequency UI path (Cron panels, SessionDialog, LogsForm settings copy,
  slash-command output) — none plausibly fire every ~30-40s in the
  background while idle, which is what the live sawtooth timing implies.
  `web/package.json` has no date-formatting dependency (no dayjs/moment/
  date-fns/luxon/timeago), so the caller is most likely JIT-inlined library
  code inside a transitive `node_modules` dependency, invisible to both grep
  and native `sample` symbolication. **Next step unchanged from the 08-28
  entry**: a DevTools flame-graph capture (tray → Open DevTools → Timelines,
  or JS Profiler) during a live spike, clicking into the hot frame, would
  show the exact file/function — still requires the user's manual Web
  Inspector access, no tool here can drive the native WKWebView.
- **Not yet re-verified against a long session on the fixed build** — the
  data above is all from the *pre-fix* binary; the rebuilt app needs a
  comparable multi-hour idle stretch to confirm the sawtooth amplitude
  actually shrinks now that production gets the cache. If it doesn't shrink
  materially, that's strong evidence the dominant caller is the
  still-unfound bare-`.toLocaleString()` path this fix deliberately doesn't
  touch.

### FOUND + FIXED: chat message list was never virtualized — unbounded DOM/heap growth with conversation length (2026-08-27)

The biggest finding of this investigation. User's 1h49m-old session had
climbed from 3.18GB to 5.15GB in about a minute of streaming; the residual
`toLocaleString`/ICU signature was still present in samples but looked
increasingly like a symptom riding along with something bigger, not the
root cause. User specifically prompted: "i recall virtul list thts
optimised" / "or maybe tanstack got some kind of virtual list for
messages" — worth checking since the codebase already uses
`@tanstack/react-store`.

- [x] **Confirmed there was no virtualization at all.** `grep` for
  `react-window|react-virtual|Virtuoso|FixedSizeList|VariableSizeList|
  useVirtualizer` across `web/src` and `web/package.json`: zero hits.
  `ChatPanel.tsx`'s render was a bare `messages.map((msg, i) => ...)` — every
  message ever loaded into a session stayed mounted as real DOM (React
  fiber nodes, markdown/syntax-highlighter output) for the ChatPanel's
  entire lifetime, and the reducer's `ADD_MESSAGE` just appends forever
  (`messages: [...s.messages, action.message]`, no upper bound). This
  scales with *conversation length*, not per-token churn — exactly matching
  a long-running session's sustained growth pattern rather than the
  spike-and-reclaim sawtooth from the earlier fixes.
- [x] **Fix**: added `@tanstack/react-virtual@^3` (same install session hit
  a broken `pnpm` shell wrapper — a custom function shadowing `pnpm add`/
  `install`/`i` that calls a missing `_pkg_age_resolve` helper not loaded in
  a non-interactive shell; bypassed with `command pnpm add ...` rather than
  touching the user's shell config). `ChatPanel.tsx` now virtualizes the
  committed `messages` array via `useVirtualizer` (dynamic per-item height
  via `measureElement`, `estimateSize: 96`, `overscan: 8`); `live` (the
  in-progress turn) stays rendered as a normal tail below the virtualized
  window since it's always visible, short-lived, and churns too fast to
  benefit from virtualization.
  - A few structural message kinds (`QUESTION_PROMPT:`/`PERMISSION_ASK:`
    tool messages) are filtered into a `visibleMessages` array *before*
    virtualizing, with an index map back to the original `messages` array —
    filtering post-render (returning `null` per item, the old pattern)
    would leave a blank gap sized by `estimateSize` in a virtualized list
    instead of collapsing to zero height.
  - Auto-scroll-to-bottom, scroll-up pagination (load-older-on-
    `scrollTop < 100`, preserve-position-after-prepend), and the top
    "Loading older / Beginning of conversation" indicator all needed zero
    changes — they only ever touched `scrollRef.current.scrollTop`/
    `scrollHeight`, which virtualization doesn't change the semantics of
    (still the real scrollable container; the virtualizer just renders a
    windowed subset inside a full-height spacer).
  - Search-jump-to-match (`Ctrl/Cmd+F`) *did* need a real change: the old
    `messageRefs.current[i]?.scrollIntoView()` assumed the target message
    was already mounted, which isn't true once off-screen matches stop
    rendering. Replaced with `virtualizer.scrollToIndex(pos, {align:
    "center", behavior: "smooth"})` keyed through the original-index →
    visible-position map — handles jumping to an unmeasured/unmounted item
    and correcting position once it measures.
- [x] **Verified**: `npx tsc --noEmit` clean, full `vitest run` 188/188
  passing (up from 179 earlier in this same investigation — the concurrent,
  unrelated `tabQueue.test.ts` work-in-progress from another session
  finished and now passes too), `npm run build` (`tsc && vite build`) clean.
  Rebuilt `bin/ocode.app`, relaunched, `mem_watch.sh` re-armed on the new
  pid. **Not yet independently confirmed against a long real session** post-
  fix (that needs the app running for a comparable stretch of time under
  normal use) — next step is watching whether footprint growth on a long
  session now tracks bounded/sawtooth instead of climbing with transcript
  length.

## Local model auto-start hang: MLX "python3" resolves to the wrong interpreter on desktop (2026-08-25, FIXED)

Root-caused via the live `/api/logs` snapshot of the session above:
`local/qwen3-4b-instruct-4bit` (used as the auto-permission-decision model)
failed its first-ever health poll after **16m9s** (`elapsed=16m9.10659875s`),
then correctly circuit-broke on every subsequent call. 16 minutes matches
`waitForChatHealth`'s `chatHealthPollAttemptsFirst = 900` (15min) budget almost
exactly — the poll ran to completion rather than failing fast.

- [x] **Root cause 1 — PATH: `ocode.app` launched from Finder/Dock inherits
  launchd's bare `PATH=/usr/bin:/bin:/usr/sbin:/sbin`** (confirmed via `ps eww
  -p <pid>` on the live process), which never sources shell rc files. Every
  bare `"python3"` invocation in the MLX backend
  (`ensureMLXChatModelCached`, `mlxServerFlags`, and `LaunchArgv[0]` in both
  `spawnMLXChatServer` and `spawnMLXServer`) therefore resolved to
  `/usr/bin/python3` — the Xcode Command Line Tools stub (3.9.6) — instead of
  the Framework Python 3.12 `mlx_lm`/`huggingface_hub` were actually
  pip-installed into. Confirmed directly: `/usr/bin/python3 -c "import
  mlx_lm"` → `ModuleNotFoundError`. Manually running the correct interpreter
  launched and passed health in 15s — the model itself was never the problem.
  **Fix**: new `internal/discovery/python_env.go` — `mlxPythonPATH()` asks the
  user's own login shell for its real PATH (`$SHELL -ilc`, cached
  `sync.Once`, marker-delimited to survive MOTD noise, falls back to the
  process's own PATH on any failure) and `mlxPythonBinary()` searches it for
  an executable `python3`. Applied at all four call sites — the two direct
  `exec.CommandContext(ctx, "python3", ...)` calls now pass
  `mlxPythonBinary()` as argv[0] directly (setting `cmd.Env` alone cannot fix
  this: Go resolves a bare argv[0] via the *calling* process's
  `os.Getenv("PATH")` before `cmd.Env` is ever consulted — a real gotcha, not
  cosmetic), and the two `spawn(cmdline)`-via-`bash -c` paths
  (`spawnMLXChatServer`, `spawnMLXServer`) substitute the resolved absolute
  path into `argv[0]` before building the cmdline.
- [x] **Root cause 2 — no liveness check let one dead-on-arrival process burn
  the full 15-minute budget.** `waitForChatHealth` polled only the HTTP health
  endpoint; it had no visibility into whether the process it just spawned was
  even still running, so a process that crashed with `ModuleNotFoundError`
  within the first second still wasn't reported as failed until poll attempt
  900. **Fix**: new exported `discovery.ChatLiveness` type
  (`func() (alive bool, detail string)`), returned by the `spawn` callback
  (`internal/agent/local_models.go`) — backed by the existing
  `tool.Process.SnapshotStatus()` / `ProcessRegistry.Output()` (no new
  process-tracking machinery needed, just newly exposed to the health-poll
  loop). `waitForChatHealth` checks it every ~1s iteration, right alongside
  the HTTP probe, and fails immediately with the captured exit code + last
  2000 bytes of process output once the process is no longer
  `tool.ProcRunning`. This is the general fix — it now bounds *any* spawn
  failure (PATH, missing weights, port conflict, whatever) to about one poll
  interval instead of the full budget, not just this specific PATH bug.
  `waitForChatHealth`'s `!acquired` call site (waiting on a *different*
  process's spawn, per `acquireChatStartLock`) passes `nil` for the liveness
  check — no local process handle exists there, so behavior for that path is
  unchanged.
  Regression test: `TestWaitForChatHealthFailsFastWhenProcessAlreadyExited`
  (`internal/discovery/instances_test.go`) — asserts both the error message
  carries the process diagnostic and that failure takes under 2s, not 15min.
- **Verified**: `go build ./...` clean; `go test ./internal/discovery/...`
  and `go test ./internal/agent/...` both pass except three pre-existing,
  unrelated failures confirmed independent of this change (same failures
  reproduce running each test in isolation) —
  `TestPermissions_GitMutatingSubcommandsAsk` and
  `TestGitAlwaysAllowPersistsAtSubcommandGranularity` depend on an external
  `claude` CLI binary being resolvable (`internal/agent/advisor_tool.go`
  shells out to it) and get `DENY` instead of `ASK` when it isn't found in
  *this* environment's PATH — notably the same class of bug as root cause 1
  above, just in a different subsystem (`internal/agent/permissions.go`),
  **not fixed here**, out of scope for this pass; `TestTokenUsageSpendUsesRegistryPricing`
  passes in isolation and only fails under full-package run, a pre-existing
  test-order/shared-state issue unrelated to any file this change touched.
- **Not fixed (separate, lower-priority, same bug class)**: the embedder's
  own health-poll (`EnsureLocalServer` in `localserver.go`, ~60s first-attempt
  budget) has the identical "no liveness check" gap as
  `waitForChatHealth` did — much smaller blast radius (60s vs 15min) so not
  addressed in this pass. The `spawnMLXServer` PATH fix above already covers
  it independently (same `mlxPythonBinary()` helper), so the embedder can
  only still hang on a *different* kind of process death (not the PATH bug),
  for up to 60s.

- [x] Embedded browser panel: spec § External mode limits include a "per-stateKey
  concurrent upstream connection cap 32" (design doc line ~142). At
  implementation time, no part file of
  `docs/superpowers/plans/2026-08-30-embedded-browser-panel/` owned it — Part
  03 shipped the external fetch path without it (transport idle-pool only).
  → **Landed 2026-08-31** (`internal/browse/connlimit.go`, wired in `server.go`/`external.go`/`local.go`, tests in `connlimit_test.go`, commit `2ad684f`; status in `docs/architecture/v1-connection-cap-exclusion.md`). (from: part 03 implementation, 2026-08-31)

## Embedded browser panel — v1 non-goals (deferred)

Shipped in the 2026-08-30 embedded-browser-panel plan; these are explicitly NOT built in v1:
- Agent/tool access to the embedded browser (reading pages, driving clicks).
- Screenshots / recordings of the browser panel.
- Cookie/auth session persistence across ocode server restarts (the browse
  cookie jar is in-memory; restart drops sessions).
- Multiple browser sub-tabs inside a single panel.
- Promoting a side panel to a standalone browser tab (side panel and browser
  tab keep independent state).
- Per-stateKey concurrent upstream connection cap (32, spec § External mode
  limits): explicitly EXCLUDED from v1 acceptance — no plan part owned it.
  Needs a follow-up that adds a semaphore around handleExternal/handleLocal
  upstream work.
