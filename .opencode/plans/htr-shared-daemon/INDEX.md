# Shared HTR Daemon Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ocode starts and adopts one shared `htrcli serve` daemon — the one the user's own browser extension already talks to — instead of a private daemon on port 3846 that no external extension can reach.

**Architecture:** `browser.htr_shared` (default true) selects between the shared daemon described by `~/.htrcli/config.json` and today's private daemon (the rollback path, `htr_shared: false`). Resolution produces a `SharedDaemon` descriptor (port, socket, token, binary, mode); `EnsureHTRServe` then resolves one of four states — own-alive, foreign-alive, start, absent — using a new relaxed probe for adoption. Ownership gains `started_by_pid` so ocode stops only what it spawned. The extension preload keeps ocode's namespaced native host and simply stops overriding the socket.

**Tech Stack:** Go 1.26 (`internal/browse/cdp`, `internal/server`, `internal/config`, `internal/tui`), React + TypeScript (`web/src/components/Settings`), vitest, Go `testing` with the existing function-variable seams.

**Spec:** `.opencode/plans/2026-10-01-htr-shared-daemon-spec.md` (NOT yet copied into the OKF bundle — see the deferred item in `TODO.md`). Read it before starting; this plan argues from it.

**Deviation from the skill's single-file layout:** this plan is split into `INDEX.md` (this file) plus four part files because it is 8 tasks and ~700 lines. Execute the parts in numeric order.

## Global Constraints

- Spawn **only** through `tool.StartSupervised` inside `withHTRStartLock`, with the existing `htr-serve` registration (`ProcessRegistration{ID: htrServeID, Kind: tool.ProcessKindHTR, RetainOnShutdown: true}`). No raw `exec.Command`, no shell.
- **Never** pass `HTR_BEARER_TOKEN` to the spawned daemon. htrcli resolves its own token (`HTR_BEARER_TOKEN` → `HTR_BEARER_TOKEN_FILE` → viper `token`). Passing it is the single most likely silent regression; there is a test pinning its absence.
- Do **not** set `HTR_MANAGED_ID` to a random value — set it to the shared token, so the existing strict probe keeps working for a daemon we started.
- Never write, rewrite or remove `com.htrcontrol.host`. `validateHTRNativeHostName` must keep rejecting it. Existing regression test `TestNativeHostManifestDoesNotTouchStandaloneHost` must keep passing.
- Never hold `Handler.mu` across a daemon spawn, health wait or process start. `htrBrowserConfig()` takes and releases the lock; everything after it is outside.
- Every goroutine in `internal/tui` and `internal/agent` goes through `crashguard.Go`. Never a bare `go func()`.
- Every probe and wait loop is bounded. Existing probe timeout is 2s (`htrHealthyForInstance`).
- Runtime state stays under `paths.GlobalDataDir()`; diagnostics go to `paths.LogsDir()` or the package logger. Never `fmt.Print*` to stdout/stderr.
- htrcli's config is parsed as **JSON only**. Unparseable / missing / tokenless → AdoptOnly, never a silent default.
- No new session-id-keyed map or journal, so `/reset-id` needs no change.
- These are `ocodeconfig.json` keys, not env vars — no `.env.example` entry.

## Review Focus

The five conditions most likely to bite a user, each pinned by a named test in the owning task.

> **Corrected 2026-10-02 after implementation.** Four of the five test names below
> were never written under those names, so this table could not be used as a
> checklist — a reader checking it would re-derive already-covered (or
> non-existent) defects. The real guards are named now, and where a claim turned
> out to be false it says so. Items 1 and 5 are **not** what the plan predicted.

1. **User edits their token in `~/.htrcli/config.json` while ocode is running.**
   **Not a defect.** The plan assumed a cached token; there is none.
   `ResolveSharedDaemon` re-reads the file on every call (`loadHTRcliConfig` →
   `os.ReadFile`) and every entry point resolves immediately before use — the
   TUI's `ensureSharedHTRDaemon` (`internal/tui/model.go:2981`, at-most-once per
   session) and the settings button's `startManagedHTR`
   (`internal/server/handler_config.go:2221`), which re-resolves per click. A
   `SharedDaemon` lives only in a local `HTROptions` inside one `EnsureHTRServe`
   call, so no edit can go unnoticed.
   Guards: `TestResolveSharedDaemonRereadsConfigAfterTokenEdit`,
   `TestResolveSharedDaemonRereadsConfigAfterPortEdit`,
   `TestResolveSharedDaemonHonoursOcodeTokenOverride` — mutation-verified against
   a compiling cache mutant.
2. **Two ocode instances (TUI + desktop) at once** — the second must adopt, not
   spawn, and must not kill the first's daemon on exit.
   Guards: `TestStopRuleDefersToLiveLease`, `TestStopHTRServeDefersToAnotherInstancesLease`,
   `TestFinalLeaseReleaseDoesNotKillAnAdoptedDaemon`.
3. **`htr_port: 3846` left in an existing config** — shared mode ignores it, and
   the Settings UI must show the *effective* port with provenance.
   Guards: `TestResolveSharedDaemon` (table case `LegacyPort: 3846` in shared
   mode), plus `BrowserForm`'s `htr-effective` provenance assertion in Task 7.
4. **User closes ocode while the extension is connected** — the daemon stops (by
   design) and the extension drops. Intended; keep it that way.
   Guards: `TestStopRuleStopsOwnDaemon`, `TestStopHTRServeStopsOwnDaemon`.
   (Also `TestStopRuleUsesStartedByPIDNotOwnerPID` — the plan called this
   `TestStopRuleUsesStartedByPID`.)
5. **A daemon is already listening on the shared port but rejects our token**
   (wrong token, or a foreign service). **The plan overstates what the code does.**
   ocode *cannot* distinguish "occupied by a service that rejects us" from "empty":
   the only two probes are the strict identity match and the foreign token probe,
   and a service rejecting both is indistinguishable from an open port. So ocode
   **does** attempt the spawn; the child fails to bind and dies within
   `sharedSpawnConfirmBudget`, and `confirmSharedSpawnAlive` surfaces that with
   the child's own output (including the bind error). The user sees a real error,
   but a process was wasted and the diagnosis is indirect.
   Guard for the mechanism: `TestReadinessFailsLoudlyWhenDaemonDiesDuringExec`.
   There is **no** test for the port-occupied scenario itself, and a clean
   pre-spawn refusal would need a third probe distinguishing "bound by something
   else" from "empty".

---

## Task index

| # | Task | Part | Delivers |
|---|---|---|---|
| 1 | Config fields + shared-daemon resolution | `part-1-resolution.md` | `browser.htr_shared`, `browser.htr_token`, `SharedDaemon`, `ResolveSharedDaemon` |
| 2 | Adopt-only fallback + notices | `part-1-resolution.md` | AdoptOnly is reachable and always explained |
| 3 | Foreign probe + four lifecycle states | `part-2-lifecycle.md` | `htrHealthyForeign`, adopt-without-spawn |
| 4 | Ownership + stop rule | `part-2-lifecycle.md` | `started_by_pid`, stop-only-what-we-started |
| 5 | Background readiness + mid-session death | `part-2-lifecycle.md` | non-blocking verify, fail-loudly policy |
| 6 | TUI eager ensure | `part-3-entrypoints.md` | plain TUI starts the daemon |
| 7 | HTTP API + Settings UI | `part-3-entrypoints.md` | provenance fields, adopt-only refusal, greyed legacy |
| 8 | Native host, socket injection, docs | `part-4-native-host-and-docs.md` | shared socket for the preload, comments, CHANGES |

## Deferred (in `TODO.md`, not in this plan)

- Bundle copy of the spec at `docs/superpowers/specs/2026-10-01-htr-shared-daemon-design.md` plus its `docs/index.md` / `docs/log.md` entries, via the context agent.
- Pruning orphaned `com.ocode.htrcontrol.json` files from browser families ocode no longer manages.
