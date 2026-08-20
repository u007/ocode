# Cross-Process Shared LSP Broker

- **Status:** approved design, revised after review
- **Date:** 2026-08-20

## Goal

Reduce duplicate memory usage when TUI, desktop/web, and nested `ocode` processes use the same project. Multiple processes may reuse one language-server process when their LSP state is compatible; incompatible clients retain isolated local servers.

## Scope and safety model

The first implementation supports multiple simultaneous clients and is enabled automatically for `gopls`, with a configuration switch such as `lsp_shared: false` to force the current local stdio behavior. Python and TypeScript are later adapters. Unsupported servers and ambiguous broker failures use local fallback.

V1 is **compatibility-based sharing**, not arbitrary LSP-session merging. A broker is shared only when clients agree on the broker identity, project/workspace configuration, initialization-affecting options, and document state. A client that requests incompatible capabilities, workspace/configuration, or document contents is rejected by the broker and may start an isolated local server. The implementation must not silently create local fallback while a live broker has rejected the client without surfacing the reason. Transient connection errors are not compatibility failures: clients use bounded reconnect with jitter before any local fallback. The broker must not spawn a second LSP for a timeout, a temporarily unavailable port, or a lost TCP connection until the broker is confirmed dead or the user has explicitly disabled sharing.

For documents, the broker uses a reference-counted canonical state:

- the first client opening a URI establishes canonical text and version;
- a later client may attach only when its text/version is compatible;
- conflicting `didOpen` or `didChange` state is rejected for shared mode and causes that client to use a local server;
- a client close decrements ownership; the broker sends upstream `didClose` only after the final compatible client closes;
- shared document changes are serialized and the broker publishes the resulting canonical version to attached clients;
- clients must accept broker versions or leave shared mode; unsaved divergent buffers are never merged implicitly.

This keeps multiple clients possible while preventing the broker from pretending that incompatible editor buffers are one document.

## Architecture

Add a broker layer under `internal/lsp/broker/`. A broker owns one existing stdio-backed LSP server. It listens only on loopback TCP (`127.0.0.1:0`) so the transport is identical on macOS, Linux, and Windows.

The broker key includes:

- canonical project root;
- resolved executable path and executable fingerprint/version;
- exact server arguments;
- language ID;
- workspace folders;
- initialization options and other configuration that affects upstream state.

Discovery metadata is stored below `paths.GlobalDataDir()`:

```text
<global-data>/lsp/<root-hash>/<server-identity-hash>.json
```

Metadata contains protocol version, canonical root, server identity, port, broker PID, broker instance UUID/epoch, random authentication token, and creation time. It is written atomically after the broker is listening and the underlying server has initialized. A cross-platform startup lock serializes broker election.

The existing `Manager` and `Client` APIs remain stable. `Manager.ClientForExt` first attempts broker discovery and a compatibility handshake; if no compatible broker exists, it may become the broker or use the current local `exec.Command` stdio path. Broker rejection for incompatible state is distinct from broker absence and is surfaced in diagnostics/debug logs before local fallback.

## Protocol

Broker/client traffic uses length-prefixed JSON frames:

```text
[4-byte big-endian length][JSON envelope]
```

Every envelope contains a protocol version, frame kind, client ID, request ID where applicable, and payload. The protocol defines maximum frame size, malformed-frame behavior, unknown-field policy, queue limits, write serialization, and handshake/read/write deadlines.

The handshake validates protocol version, token, broker UUID, project root, server identity, workspace configuration, and client compatibility. Broker request IDs are distinct from client request IDs. Pending requests are tracked per client and upstream, with cancellation and deadline cleanup.

### Message ownership matrix

- **Client requests/responses:** client-scoped; broker rewrites IDs and routes responses.
- **Document notifications:** broker-owned canonical state; accepted only when compatible, serialized upstream, and reflected to attached clients.
- **Diagnostics:** broker-owned by URI, canonical document version, and broker/server generation; clears are propagated explicitly to every compatible subscriber for that URI.
- **Workspace notifications/configuration:** broker-wide; later clients must match immutable initialization/configuration or be rejected. Mutable configuration changes are serialized and broadcast only when the server supports safe sharing.
- **Server notifications:** broadcast or explicitly routed according to method; unsupported notifications are surfaced, never dropped silently.
- **Server requests:** broker routes to a designated compatible client when there is an owner; otherwise it responds with a defined unsupported/error result. Request IDs are rewritten in both directions, with cancellation and timeouts.
- **Dynamic registration and client capability changes:** unsupported in V1 unless an explicit broker policy exists; incompatible clients are rejected rather than partially attached.
- **Workspace edits and user prompts:** owner-routed; if no owner exists, the broker rejects the request instead of applying it to an arbitrary client.

The broker binds only to loopback and never exposes its port through the web API or UI.

## Lifecycle and failure handling

1. Resolve and canonicalize the project root using platform-specific absolute/symlink/case rules.
2. Resolve the executable and derive the full broker identity.
3. Validate existing metadata by connecting and completing the authenticated identity handshake.
4. If missing or confirmed dead, acquire the per-identity startup lock.
5. Re-read metadata after lock acquisition; connect if another process won the election.
6. Start the broker and underlying stdio LSP server.
7. Atomically publish metadata only after successful upstream initialization.
8. Other processes retry discovery with bounded backoff.
9. Track connected clients and stop accepting new clients during shutdown.
10. Drain or cancel pending requests, close the upstream LSP session gracefully, force-kill and wait for the child after a bounded timeout, then remove metadata only if its UUID still matches.
11. Release the startup lock and close all sockets/timers/goroutines.

The broker shuts down after the last client disconnects and an idle grace period expires. Pending work, reconnect attempts, or queued notifications reset the grace timer.

Failure classes are explicit:

- absent/confirmed-dead metadata: election or local fallback;
- live broker with token, identity, protocol, capability, or configuration mismatch: no silent local split-brain; surface incompatibility and use isolated local mode only with an explicit isolated identity;
- transport failure: bounded reconnect as a fresh session, then local fallback with a new isolated identity;
- child-server exit: fail pending requests, invalidate the broker epoch, and restart or fall back.

Reconnect is a fresh broker session: initialize, initialized, compatible document replay, and diagnostic generation reset. Pending requests from the old connection are failed unless the caller explicitly retries them.

## Configuration and rollout

The current V1 exposes sharing as an explicit `NewSharedManager` policy gate and provides authenticated broker transport/lifecycle primitives. The existing `NewManager` path remains isolated stdio. Manager auto-discovery, broker election from a Manager, and shared document/diagnostic routing are intentionally not enabled until the full multi-client protocol is validated; this prevents an authenticated but semantically incompatible stream from creating split-brain LSP state. The gate is construction-time and does not mutate an active manager.

Python and TypeScript adapters, automatic gopls election, and document-state sharing are follow-up work after the multi-client gopls protocol and lifecycle tests pass. All other servers retain local fallback.

## Testing

Unit tests cover metadata encoding, atomic writes/replacement, broker identities, canonical roots, startup-lock contention, stale records, token/root/server validation, request-ID rewriting in both directions, cancellation, frame limits, bounded queues, and lifecycle transitions.

Integration tests use a fake stdio LSP server and verify two compatible clients can initialize, issue requests, receive correctly routed responses and diagnostics, share compatible document opens/changes/closes, and survive independent disconnects. Tests cover conflicting document contents/versions, incompatible initialization capabilities, workspace configuration, server-originated requests, progress, dynamic registration rejection, workspace edits, stale metadata recovery, startup failure, reconnect replay, split-brain prevention, idle shutdown, and local fallback.

Cross-platform tests verify path handling, metadata permissions, loopback-only binding, atomic replacement, process cleanup, and concurrent startup on macOS, Linux, and Windows. At least one integration test uses a real supported `gopls` binary where available.

## Non-goals

V1 does not merge arbitrary unsaved editor buffers, expose a remote LSP service, or silently drop unsupported protocol traffic. It shares one expensive server only for compatible clients and preserves isolated local behavior otherwise.
