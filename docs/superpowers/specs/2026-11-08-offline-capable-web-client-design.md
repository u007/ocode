# Offline-Capable Web Client Design

**Date:** 2026-11-08
**Status:** Draft — revised per advisor review
**Scope:** Two independent features (can ship separately)

## 1. Problem Statement

When the local/desktop ocode server is unavailable (crashed, restarting, or the user is on a different network), the browser cannot:
- Load the SPA (assets served by the local server; `no-cache` on `index.html`)
- Access any project, session, or terminal data (all server-side)
- Reach remote SSH servers (tunnel owned by the local server)

The user wants:
1. **Offline read access** to cached local projects, sessions, terminals, and chat history (when everything is down)
2. **Direct remote access** to remote SSH servers when the local server is down, with full read-write capability on remote projects/sessions

These are independent features. Feature 1 needs no server changes. Feature 2 needs server changes but no service worker.

## 2. Key Insight: Remote Server Already Serves the SPA

`ocode serve --remote` already registers `s.mux.Handle("/", spaHandler(s.webFS))` (server.go:870). This means the remote server can serve the SPA at its own origin. A browser connecting directly to the remote server gets both the SPA and the API from the same origin — no CORS, no Private Network Access preflights, no cross-origin complexity.

This eliminates the need for:
- CORS configuration
- TLS certificate management (for same-machine/Tailscale scenarios)
- Cross-origin IndexedDB sharing
- Service worker for the remote origin

## 3. Feature A: Offline Read Cache (No Server Changes)

### 3.1 Service Worker (App Shell Only)

The service worker caches only the app shell and hashed assets. API responses are NOT cached by the SW (they go to IndexedDB).

| Asset Type | Strategy | Rationale |
|------------|----------|-----------|
| Hashed JS/CSS (`/assets/*`) | Cache-first, immutable | Content-hashed filenames; safe to cache forever |
| `index.html` | Stale-while-revalidate | Must revalidate but can serve stale while fetching |
| Other static (images, fonts) | Cache-first | Rarely change |
| `/api/*` | Network-only (no SW cache) | API data goes to IndexedDB, not SW cache |

**Rationale for no `/api/*` in SW:** Two caches (SW Cache API + IndexedDB) for the same data means two copies of authenticated chat data with no logout purge. Keep the SW to the app shell; put API data in IndexedDB.

### 3.2 IndexedDB Cache

```typescript
// Database: "ocode-offline" (origin: local server origin)
// Version: 1

interface OfflineDB {
  projects: {
    key: string;        // project path
    name: string;
    lastSynced: number; // timestamp
    data: ProjectMeta;
  };
  sessions: {
    key: string;        // session ID
    projectId: string;
    lastSynced: number;
    messages: Message[];  // clipped to last 200 messages
    metadata: SessionMeta;
  };
  terminals: {
    key: string;        // terminal ID
    projectId: string;
    lastSynced: number;
    scrollback: string; // last 1000 lines only (may contain secrets — see 3.4)
    tabs: TerminalTab[];
  };
}
```

### 3.3 Sync Strategy

- **Write-through:** After each successful fetch, write to IndexedDB
- **Offline read:** When fetch fails (network error or timeout), read from IndexedDB
- **TTL:** 7 days for sessions, 30 days for projects, 1 day for terminals
- **Quota:** Use `navigator.storage.estimate()`; evict oldest entries when > 50MB
- **Stale handling:** Show "Last synced: X ago" indicator; never show stale data without the indicator

### 3.4 Security Considerations

- Terminal scrollback may contain secrets (passwords, tokens typed in terminal). Limit to last 1000 lines. Clear on logout.
- IndexedDB is origin-scoped. Data is only accessible to the SPA at the local origin.
- No encryption at rest (IndexedDB is already origin-scoped; the SPA has full access).

### 3.5 Files to Add

- `web/public/sw.js` — service worker (app shell + assets only)
- `web/src/lib/serviceWorker.ts` — registration + update notification
- `web/src/lib/offlineDb.ts` — IndexedDB wrapper
- `web/src/lib/offlineSync.ts` — sync orchestration (write-through on fetch success)
- `web/src/hooks/useOfflineStatus.ts` — online/offline detection + cached data availability

### 3.6 Detection of "Local Down"

The existing connection logic already handles this:
- `fetchJSON`/`fetchEmpty` throw on network error
- `eventBus` reconnects with exponential backoff (1-30s)
- `useRemoteHostStatus` detects stall after 10s

For offline mode:
- If `navigator.onLine` is false → offline immediately
- If fetch fails with network error → mark offline
- If fetch succeeds → mark online, sync to IndexedDB
- The existing 503 + `Retry-After` from remote-connecting is treated as "transient, retry" not "offline"

## 4. Feature B: Direct Remote Access (Server Changes)

### 4.1 New CLI Flags

`ocode serve --remote` gains:

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--bind` | string | `127.0.0.1` | Address to bind on. Non-loopback requires auth. |
| `--share-token` | bool | false | Generate and persist a durable share-token. |

### 4.2 Security Rules

1. **Auth gate.** When `--bind` is non-loopback, `--share-token` is required. Refuse to start otherwise — same rule as `authConfigured()` for funnel.

2. **No self-signed TLS.** Browsers cannot accept self-signed certs via `fetch`/`EventSource`. Options:
   - **Tailscale:** Use `tailscale serve` to get a valid TLS cert. The remote server binds loopback; Tailscale proxies with TLS.
   - **Loopback only:** Keep `--bind 127.0.0.1` (default). For same-machine scenarios, the tunnel survives the desktop crash and the browser connects through the tunnel's local port. No TLS needed.
   - **User-provided cert:** `--tls-cert` + `--tls-key` for users with their own CA.

3. **Token generation.** 256-bit CSPRNG, hex-encoded (64 chars). Stored hashed (SHA-256) in `~/.ocode/remote/share-token` (mode 0600). Plaintext printed to stdout once.

4. **Token validation.** `Authorization: Bearer <token>` header. The per-launch token (in `serve.json`) is unaffected.

5. **Revocation.** `--revoke-token` deletes the hash file.

6. **Rate limiting.** Per-IP on auth endpoints when non-loopback (10 attempts/min).

### 4.3 Browser-Side Configuration

Settings → Remote Connections dialog:

```
┌─────────────────────────────────────────────┐
│ Remote Connection                            │
│                                              │
│ URL:   [https://host:port or http://...]     │
│ Token: [paste share-token]                   │
│                                              │
│ [Test Connection]  [Save]  [Remove]          │
└─────────────────────────────────────────────┘
```

Stored in `localStorage` (origin: wherever the SPA is loaded). When the browser navigates to the remote server's origin, it uses the saved token.

### 4.4 Connection Flow

```
1. User opens browser, navigates to remote server URL (or it's saved as a bookmark)
2. Remote server serves SPA (same origin)
3. SPA reads saved token from localStorage
4. SPA connects to /api/* with Bearer token
5. Full read-write access to remote projects/sessions
```

No fallback needed — the remote server IS the server. The "fallback" is: if the remote is also down, the user goes back to the local origin's offline cache.

### 4.5 Reusing Existing Auth

The per-launch token in `serve.json` could be reused instead of a separate share-token. But the per-launch token changes on every restart. For direct browser access, a durable token is better. The share-token is separate and persistent.

## 5. Implementation Phases

### Phase 1: Offline Read Cache (Feature A)
- Service worker for app shell caching
- IndexedDB wrapper + sync
- Offline status hook
- Offline indicator UI
- Write gating for offline mode
- **No server changes. No new Go code.**

### Phase 2: Direct Remote Access (Feature B)
- `--bind` and `--share-token` flags
- Share-token generation/storage/validation
- Rate limiting for non-loopback
- Settings UI for remote connections
- **No service worker changes.**

### Phase 3: Integration (Optional)
- Unified connection status (local vs remote vs offline)
- Seamless switching between origins
- **Depends on both Phase 1 and Phase 2.**

## 6. Open Questions

1. **Multi-device tokens:** Should one host support multiple share-tokens (one per device)? Recommendation: single token per host for simplicity. Revoke regenerates.

2. **Tailscale integration:** Should `--share-token` automatically set up Tailscale serve? Recommendation: no, keep them separate. Tailscale is a deployment concern.

3. **Offline write queue:** Should we queue writes made while offline? Recommendation: no (read-only offline). Queued writes require conflict resolution.

4. **IndexedDB size limit:** What's the practical limit? Recommendation: 50MB with LRU eviction. Show warning at 80%.

5. **Terminal scrollback secrets:** Should we skip caching terminal scrollback entirely? Recommendation: cache last 1000 lines but clear on logout and show warning.

## 7. Non-Goals

- Queued writes / offline mutations
- Multi-user / shared cached data
- End-to-end encryption of cached data
- Background sync API
- Push notifications
- Cross-origin data sharing (each origin is self-contained)
