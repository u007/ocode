---
type: Gotcha
title: Shared LSP Broker Server-State and Routing Gotchas
description: 'Updated with confirmed spec violations from 2026-08-21 review: Manager sharing enabled by default, docs map unbounded/didClose absent, conflicting didOpen/didChange overwrites, daemon inherits terminal fds'
resource: superpowers/specs/2026-08-20-shared-lsp-broker-design.md
tags:
  - lsp
  - broker
  - shared-server
  - diagnostics
  - document-state
  - protocol
  - compatibility
  - reconnect
  - split-brain
  - spec-violation
  - review-finding
timestamp: 2026-08-20T03:21:23Z
---
# Shared LSP Broker Server-State and Routing Gotchas

**Type:** Gotcha  
**Description:** Validated shared-LSP broker transport/RPC status and the still-gated Manager discovery, stdio lifecycle, fallback, and two-manager integration work.  
**Resource:** superpowers/specs/2026-08-20-shared-lsp-broker-design.md  
**Tags:** lsp, broker, shared-server, diagnostics, document-state, protocol, compatibility, reconnect, split-brain  

---

A shared LSP broker is **compatibility-based sharing**, not transparent multiplexing of arbitrary editor sessions. It may reuse one stdio-backed language server only when clients agree on the complete broker identity, workspace/initialization configuration, capabilities, and document state. Incompatible clients must be rejected from shared mode with a surfaced reason and may use an explicitly isolated local server. Never silently merge incompatible state or create an unobservable split-brain fallback. (Approved design: `docs/superpowers/specs/2026-08-20-shared-lsp-broker-design.md`.)

## Validated current implementation state

The broker transport and client RPC slice is complete and validated: formatting, focused and package tests, vet, race checks, and `git diff --check` pass. This covers broker identity, atomically published metadata, bounded length-prefixed framing, authenticated loopback transport, startup coordination, and authenticated client request/response and push routing.

Manager integration remains deliberately disabled. Manager discovery/election is not implemented; `NewBrokerClient` attaches only to caller-supplied metadata and does not discover or start a broker. Broker-owned adaptation of the upstream stdio server and its lifecycle handoff are also incomplete. `NewSharedManager` remains only an explicit policy gate and still launches isolated gopls processes through the ordinary stdio path. Shared-client fallback is therefore not automatic: until the integration contracts exist, attachments recorded through the manager are ignored and `ClientForExt` uses isolated stdio.

The remaining integration work is intentionally gated. It still requires canonical document synchronization and ownership, diagnostics routing by URI/version/broker generation, ownership and failure semantics for server-originated requests, broker discovery/election and lifecycle/fallback handling, and broker-owned adaptation of the upstream stdio server. Two-manager integration coverage proving that compatible managers share one fake upstream server is also missing. Do not enable Manager-level sharing until those contracts and their validation exist; otherwise duplicate server processes, conflicting document state, stale or misrouted diagnostics, and split-brain behavior remain possible.

## Broker identity and client compatibility are separate checks

The broker identity must include the canonical project root, resolved executable path and fingerprint/version, exact arguments, language ID, workspace folders, initialization options, and every other upstream-state-affecting configuration. Metadata also carries the protocol version, broker instance UUID/epoch, loopback endpoint, and authentication token.

A client handshake must validate the token, broker UUID/epoch, protocol, canonical root, server identity, workspace configuration, capabilities, and compatibility options. A matching server identity alone is insufficient: a client requesting incompatible initialization capabilities, workspace/configuration, or dynamic behavior is rejected rather than partially attached. Broker rejection is distinct from broker absence and must be visible in diagnostics/debug logs before any isolated local fallback.

## Canonical document state rejects conflicts

The upstream server sees one document per URI. The first compatible client establishes canonical text and version; subsequent clients may attach only when their text/version is compatible. Client-local LSP versions are stream-scoped and must be translated or validated against broker versions.

Conflicting `didOpen` or `didChange` content/version is rejected for shared mode; unsaved divergent buffers are never implicitly merged. Shared changes are serialized, assigned a monotonically increasing canonical broker version, and published to attached clients. A client that cannot accept broker versions leaves shared mode. Reference counting governs close behavior: the broker sends upstream `didClose` only after the final compatible client closes. Serialization without this canonical-state contract merely makes corruption deterministic.

## Diagnostics require URI, version, and generation routing

Diagnostics are notifications, not request responses. Store and route them by URI, canonical document version, and broker/server generation. Deliver them only to compatible subscribers permitted by the URI subscription/open-document policy; do not broadcast per-document diagnostics merely because clients share a server. Explicitly propagate diagnostic clears to every compatible subscriber for that URI.

A diagnostic from an older document version or broker epoch must be ignored. On close, replacement, disconnect, or reconnect, define whether state is cleared or replayed; never allow stale diagnostics to reappear after a new generation. Project-wide diagnostics and the broker's per-document diagnostic path must remain distinct.

## Server-originated requests need an owner and a defined failure

Client requests and responses are client-scoped. The broker rewrites request IDs in both directions and tracks pending requests per client and upstream, with cancellation, deadlines, and cleanup. Server notifications are broadcast or explicitly routed by method; unsupported traffic is surfaced, never silently dropped.

Server requests are routed to a designated compatible client when an owner exists. Workspace edits and user prompts are owner-routed. If no suitable owner exists, return the defined unsupported/error result rather than sending the request to an arbitrary client. Dynamic registration and capability changes are rejected unless an explicit V1 policy supports them.

## Split-brain prevention and lifecycle

Discovery metadata is written atomically only after the broker is listening and the child server is initialized. Startup uses a per-identity cross-process lock, re-reads metadata after acquiring the lock, and validates live records through the authenticated identity handshake. A stale or confirmed-dead record may be replaced; metadata is removed only if its UUID/epoch still matches the process removing it.

Failure classes must remain distinct:

- absent or confirmed-dead broker: elect a broker or use local mode;
- live broker with token, identity, protocol, capability, or configuration mismatch: surface incompatibility and use an explicit isolated identity, with no silent split-brain;
- ambiguous transport failure: quarantine/invalidate stale metadata rather than assuming the broker is dead;
- child-server exit: fail pending requests, invalidate the broker epoch, then restart or fall back.

A broker epoch/instance UUID prevents an old process or delayed frame from being mistaken for the current broker. Loopback binding, authenticated handshakes, and identity validation are mandatory; the broker port is never exposed through the web API or UI.

## Reconnect is a fresh session with bounded queues

Reconnect must be bounded and treated as a fresh broker session: perform `initialize`/`initialized`, validate compatibility again, replay compatible open documents in canonical order, and reset diagnostic generation. Pending requests from the old connection fail unless the caller explicitly retries them; old request IDs and frames must not cross epochs.

Length-prefixed frames require maximum frame and queue sizes, read/write/handshake deadlines, serialized writes, malformed-frame handling, and explicit backpressure. Queues must be bounded so a slow client cannot exhaust broker memory. On overflow, reject/drop only according to a documented message policy, surface the failure, and disconnect or resynchronize rather than growing without limit. Reconnect attempts and queued notifications reset the idle timer.

## Cleanup semantics

Subscriptions and document ownership are explicit, not inferred only from TCP lifetime. Cleanup must be idempotent across `didClose`, unsubscribe, reconnect, and disconnect. Stop accepting new clients during shutdown; drain or cancel pending requests; close the upstream LSP session gracefully; force-kill and wait for the child after a bounded timeout; close sockets, timers, and goroutines; and release the startup lock. After the last client disconnects, an idle grace period permits shutdown, but pending work, reconnect attempts, or queued notifications extend that period.

## Required validation

Integration tests should cover two compatible clients sharing opens/changes/closes, conflicting document contents and versions being rejected, independent initialization and capability mismatch, versioned diagnostic routing and clears, server-originated request ownership, request-ID rewriting and cancellation, bounded frame/queue behavior, stale metadata and startup-lock contention, broker epochs and split-brain prevention, reconnect replay and diagnostic reset, idle shutdown, child cleanup, and explicit local fallback. Cross-platform tests must verify canonical path handling, atomic metadata replacement, loopback-only binding, process cleanup, and concurrent startup. Add an integration test with two `Manager` instances and one fake upstream server before enabling Manager-level sharing; it must prove discovery/election, broker-owned stdio lifecycle, shared-client fallback behavior, and document/diagnostic/server-request routing.

## Confirmed spec violations from 2026-08-21 review

The following issues were confirmed by code review and represent durable knowledge for future development and troubleshooting. They are not theoretical risks — they are actual divergence between the approved design spec and the implementation.

### Manager sharing enabled by default despite spec gate

`NewManager()` in `internal/lsp/manager.go:126` calls `newManager(root, true)`, enabling the shared broker path unconditionally. This contradicts `broker/doc.go:8`, which states "Manager's shared gopls policy remains gated," and the approved design spec line 102, which requires the full multi-client protocol to be validated before enabling sharing. The gotchas doc's earlier section ("Manager integration remains deliberately disabled") describes the *intended* gating but not this code-level reality.

**Impact:** Sharing is live for every manager instance, not just explicit opt-ins via `NewSharedManager`. Any `ClientForExt` call will attempt daemon discovery and shared attachment when the daemon is reachable, even though the canonical document conflict handling, `didClose` semantics, and diagnostic routing are not yet implemented. This creates a window for silent document state corruption and split-brain diagnostics under normal usage.

**Fix:** Either gate `sharedBroker` behind the spec's readiness checklist (canonical state, `didClose`, diagnostic routing, reconnect/health-check), or rename the field to reflect its actual purpose (opportunistic daemon reuse without canonical-state guarantees).

### `docs` map unbounded; `didClose` absent

`daemonUpstream.docs` (`internal/lsp/daemon.go:38`) is a `map[string]*docState` that grows on every `handleDidOpen` (line 197) and the defensive `applyChange` branch (line 239). There is no `didClose` handler anywhere in non-test LSP code — `Notify` in daemon.go only dispatches `didOpen` and `didChange`. Entries are never deleted.

**Impact:** Over a long-lived session, every distinct file URI opened by any broker client adds a permanent entry to the map. For a project with thousands of source files, this is a gradual memory leak. The `docState.refs` field (line 22) is incremented on every `handleDidOpen` (line 199) but never read or decremented — dead code indicating an incomplete ref-counted close design.

**Fix:** Implement `textDocument/didClose` handling: decrement `doc.refs`, delete the entry when it reaches zero, and forward the close to the real upstream client only after the last compatible client closes. Add an integration test that opens and closes files across two clients and verifies the `docs` map shrinks.

### Conflicting `didOpen`/`didChange` overwrites instead of rejecting

When a second client sends `didOpen` for a URI already held by another client, `handleDidOpen` (`daemon.go:202-208`) silently folds it into `applyChange`, overwriting the canonical text with the second client's snapshot. The spec requires rejection: "Conflicting `didOpen` or `didChange` content/version is rejected for shared mode; unsaved divergent buffers are never implicitly merged."

**Impact:** If two clients have divergent unsaved buffers for the same file, the last `didOpen` silently replaces the server's canonical text. The first client's subsequent queries (`references`, `definition`, etc.) now operate on the wrong content. This is the exact corruption scenario the spec designed against.

**Fix:** In `handleDidOpen`, when `exists` is true, compare the incoming text against `doc.text`. If they differ, either reject the `didOpen` with an error (strict mode) or log a warning and apply a merge strategy with an explicit diagnostic. Never silently overwrite.

### Daemon inherits terminal file descriptors

`spawnDaemonProcess` (`manager.go:396-398`) sets `cmd.Stdin = nil`, `cmd.Stdout = nil`, `cmd.Stderr = nil`. In Go's `exec` package, nil values cause the child to inherit the parent's file descriptors. The daemon runs `log.Printf` (e.g. in `broadcastDiagnostics`, line 144), which writes to stderr by default.

**Impact:** The daemon's log output paints directly over the TUI's alt-screen, causing the "hairwire" rendering corruption described in AGENTS.md's TUI Output Safety section. This is a recurring bug class: any process spawned while the TUI is live that inherits stderr will corrupt the terminal.

**Fix:** Set `cmd.Stdin = io.Discard`, `cmd.Stdout = io.Discard`, `cmd.Stderr = io.Discard` in `spawnDaemonProcess`, or redirect stderr to the agent's debug log. The daemon's `log.Printf` calls should use a `log.Logger` writing to `io.Discard` or a debug log file, not the inherited stderr fd.

### A `root` under the OS temp dir can never be shared — it must not spawn a daemon

`discoverOrSpawnDaemon` (`manager.go:340`) keys the daemon's broker identity on `(root, cmd, args, langID)` via `broker.NewIdentity`. Any caller that constructs a `Manager` against a fresh `t.TempDir()` (or any other one-shot temp directory) generates a `root` that is guaranteed unique to that single process run — no future daemon lookup can ever match that identity again, since nothing will ever reconstruct that exact path.

**Impact (observed 2026-08-20):** Before `NewManager` defaulted to `sharedBroker=false`, every call to `ClientForExt` — including from `internal/tool/ast_test.go`'s `TestAstTool_ReferencesByName`, which builds its `Manager` over `t.TempDir()` — went through `discoverOrSpawnDaemon` unconditionally. Each test run spawned a brand-new detached `lsp-daemon` process (by design, detached so it outlives the spawning process) that could never be reused by any other run. Repeated test invocations outpaced the daemon's 5-minute idle self-exit (`DefaultDaemonIdleTimeout`, `cmd_daemon.go:18`) and left over 1,000 orphaned `lsp-daemon`/`gopls`/`go list` processes running on the machine. The idle-timeout and connection-refcounting logic in `daemon.go`/`broker/rpc.go` were not at fault — they worked correctly; the daemons were simply structurally unshareable from the moment they were spawned.

**Fix:** `discoverOrSpawnDaemon` now calls `underTempDir(root)` first and refuses to spawn (returns `ok=false`, logs, falls back to the isolated stdio path) whenever `root` resolves inside `os.TempDir()` (`manager.go`, added 2026-08-20). This is a structural guard, not just the `NewManager` isolated-by-default change — it protects any future caller that explicitly opts into `NewSharedManager`/`NewManagerWithShared(root, true)` against a temp-dir root, not only the default path. Covered by `TestUnderTempDirRejectsTempDirRoots` and `TestDiscoverOrSpawnDaemonRefusesTempDirRoot` in `manager_test.go`. Tests that need a real LSP roundtrip should keep using the default (isolated) `NewManager`, which never touches the broker/daemon machinery at all.
