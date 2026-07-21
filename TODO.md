# TODO

## `.ojsonl` session format — concurrent-writer safety not solved (from design: 2026-07-21)

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
- [ ] **Migration wrinkle (STILL OPEN).** `EnsureLocalServer` fail-opens when a
  FOREIGN-model server squats the shared port 11457 (served id ≠ `ExpectedServeID`)
  instead of replacing it — so switching local models (or the MLX→llama.cpp
  migration) needs a manual `pkill -f llama-server` + restart. Hit repeatedly this
  session. Consider: when a wrong-model server occupies OUR fixed port 11457, stop
  it and spawn the right one (guard against killing a user's own LM Studio on a
  different port — 11457 is ocode-owned, so a wrong model there is almost certainly
  a stale ocode spawn).
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
- **`tool_result` carries no call-id** (`Tool: "tool"`), so concurrent tool
  results can mis-pair in the *live* view; the `turn_done` snapshot heals it.
  Thread the tool name/call-id through `tool_result` for correct live pairing.
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

## Server permission-prompt bridge — deferred items

- [ ] Web permission resolution is **approve/deny only** — no "always allow". The web `PermissionDialog` offers two buttons, and rule persistence (the TUI's `a`/`t` choices) pulls in `agent.IsHarmfulRequest` guards, out-of-scope-path handling (`allowOutOfScopePath`), webfetch-domain rules, and `PermissionManager` writes that `HandleResolvePermission` deliberately does not replicate. If always-allow is wanted on the web, add a persist flag to the resolve payload + dialog and route it through the same guarded persist path the TUI uses (from permission bridge: 2026-07-09).
- [ ] Like the question bridge, permission resolution works only in **headless serve mode**; `/rc` bridge mode returns 409 (the TUI owns the agent + its own permission dialog) (from permission bridge: 2026-07-09).
