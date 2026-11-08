# Offline Read Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (default) or superpowers:subagent-driven-development (long plans of independent tasks) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable the ocode web SPA to boot and show cached local projects, sessions, terminals, and chat history when the local server is unavailable.

**Architecture:** A service worker caches the app shell and hashed assets for offline boot. An IndexedDB store provides read-only cached copies of API data, populated by write-through caching on every successful fetch. An offline status hook detects connectivity loss and gates write actions.

**Tech Stack:** React 18, TypeScript, Vite, Service Worker API, IndexedDB (via raw API), Zustand

**Spec:** docs/superpowers/specs/2026-11-08-offline-capable-web-client-design.md (Feature A only)

**Detail:** decisions, not code — the default; the implementer writes the implementation.

## Global Constraints

- Service worker caches app shell + hashed assets only. No `/api/*` responses in the SW cache (API data goes to IndexedDB).
- IndexedDB database name: `ocode-offline`, version 1. Origin-scoped (local server origin only).
- TTL: 7 days for sessions, 30 days for projects, 1 day for terminals.
- Quota: 50MB max. Evict oldest entries when exceeded. Warn at 80%.
- Terminal scrollback: cache last 1000 lines only. Clear on logout.
- Offline mode is read-only: all write actions (send message, terminal input, file edit, todo changes) are disabled with a visible indicator.
- The existing `no-cache` on `index.html` and `immutable` on hashed assets must be respected by the SW (stale-while-revalidate for index.html, cache-first for assets).

## Review Focus

- A cached session rendered offline must show a "Last synced: X ago" indicator, not silently present stale data as current.
- When the user logs out, IndexedDB and SW cache must be fully purged (no stale auth data survives).
- Terminal scrollback cached in IndexedDB may contain secrets; it must be cleared on logout and limited to 1000 lines.
- The service worker must not cache `/api/*` responses (would create a second copy of authenticated data with no purge path).
- When `navigator.onLine` is true but the server is unreachable (network error), the app must still fall back to cached data, not show a generic error.

## Task 1: Service Worker for App Shell Caching

**Risk:** high — new browser API surface, cache invalidation logic, must not break normal online operation

**Files:**
- Create: `web/public/sw.js`
- Create: `web/src/lib/serviceWorker.ts`
- Modify: `web/src/main.tsx` (register SW on app boot)
- Test: `web/src/lib/__tests__/serviceWorker.test.ts`

**Interfaces:**
- Produces: `registerServiceWorker()` in `web/src/lib/serviceWorker.ts` — registers the SW, handles updates, notifies on new version available
- Produces: `web/public/sw.js` — the service worker script with cache strategies

- [ ] **Step 1: Write the failing test**

```typescript
// web/src/lib/__tests__/serviceWorker.test.ts
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { registerServiceWorker } from '../serviceWorker';

describe('registerServiceWorker', () => {
  beforeEach(() => {
    vi.stubGlobal('navigator', { serviceWorker: { register: vi.fn().mockResolvedValue({}) } });
  });

  it('registers sw.js at root scope', async () => {
    await registerServiceWorker();
    expect(navigator.serviceWorker.register).toHaveBeenCalledWith('/sw.js', { scope: '/' });
  });

  it('does nothing when serviceWorker is not supported', async () => {
    vi.stubGlobal('navigator', {});
    await expect(registerServiceWorker()).resolves.toBeUndefined();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/lib/__tests__/serviceWorker.test.ts`
Expected: FAIL with "Cannot find module '../serviceWorker'"

- [ ] **Step 3: Implement serviceWorker.ts**

Create `web/src/lib/serviceWorker.ts` with `registerServiceWorker()`. It must:
- Check `'serviceWorker' in navigator` before registering
- Register `/sw.js` at scope `/`
- Handle the `updatefound` event to notify when a new version is available
- Export a function (not auto-register on import)

- [ ] **Step 4: Create sw.js**

Create `web/public/sw.js` with:
- Precache list populated at build time (Vite injects the manifest)
- Install: precache hashed assets + index.html
- Activate: clean up old caches, claim clients
- Fetch handler with three strategies:
  - `/assets/*`: cache-first (immutable)
  - `/`: stale-while-revalidate (index.html)
  - Everything else (including `/api/*`): network-only (pass through)

- [ ] **Step 5: Register in main.tsx**

Call `registerServiceWorker()` in `web/src/main.tsx` after app mount. Only in production (check `import.meta.env.PROD`).

- [ ] **Step 6: Run tests**

Run: `cd web && npx vitest run src/lib/__tests__/serviceWorker.test.ts`
Expected: PASS

## Task 2: IndexedDB Wrapper and Schema

**Risk:** high — persistence layer, data integrity, quota management

**Files:**
- Create: `web/src/lib/offlineDb.ts`
- Test: `web/src/lib/__tests__/offlineDb.test.ts`

**Interfaces:**
- Consumes: none (foundation layer)
- Produces:
  - `OfflineDB` class with methods: `putProject`, `getProject`, `putSession`, `getSession`, `putTerminal`, `getTerminal`, `clearAll`, `getStorageEstimate`
  - Types: `CachedProject`, `CachedSession`, `CachedTerminal`

- [ ] **Step 1: Write the failing test**

```typescript
// web/src/lib/__tests__/offlineDb.test.ts
import { describe, it, expect, beforeEach } from 'vitest';
import { OfflineDB } from '../offlineDb';

describe('OfflineDB', () => {
  let db: OfflineDB;
  beforeEach(async () => {
    db = new OfflineDB();
    await db.clearAll();
  });

  it('stores and retrieves a project', async () => {
    await db.putProject({ id: '/path/to/proj', name: 'proj', lastSynced: Date.now(), data: {} });
    const result = await db.getProject('/path/to/proj');
    expect(result?.name).toBe('proj');
  });

  it('returns undefined for missing project', async () => {
    const result = await db.getProject('/nonexistent');
    expect(result).toBeUndefined();
  });

  it('clearAll removes all data', async () => {
    await db.putProject({ id: '/p', name: 'p', lastSynced: Date.now(), data: {} });
    await db.clearAll();
    const result = await db.getProject('/p');
    expect(result).toBeUndefined();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/lib/__tests__/offlineDb.test.ts`
Expected: FAIL with "Cannot find module '../offlineDb'"

- [ ] **Step 3: Implement offlineDb.ts**

Create `web/src/lib/offlineDb.ts` with:
- `OfflineDB` class wrapping IndexedDB
- Database name: `ocode-offline`, version 1
- Object stores: `projects` (keyPath: `id`), `sessions` (keyPath: `id`), `terminals` (keyPath: `id`)
- Methods: `putProject`, `getProject`, `putSession`, `getSession`, `putTerminal`, `getTerminal`, `clearAll`, `getStorageEstimate`
- TTL-aware: `getSession` returns undefined if `lastSynced` is older than 7 days
- Quota: check `navigator.storage.estimate()` before write; if > 50MB, evict oldest entries (by `lastSynced`) until under 40MB
- `clearAll` deletes all object stores' contents

- [ ] **Step 4: Run tests**

Run: `cd web && npx vitest run src/lib/__tests__/offlineDb.test.ts`
Expected: PASS

## Task 3: Offline Sync (Write-Through Cache)

**Risk:** standard — straightforward integration with existing fetch paths

**Files:**
- Create: `web/src/lib/offlineSync.ts`
- Modify: `web/src/api/client.ts` (hook sync into fetchJSON/fetchEmpty success paths)
- Test: `web/src/lib/__tests__/offlineSync.test.ts`

**Interfaces:**
- Consumes: `OfflineDB` from Task 2
- Produces: `syncProjectToCache(project)`, `syncSessionToCache(session)`, `syncTerminalToCache(terminal)`, `loadFromCache(type, key)`

- [ ] **Step 1: Write the failing test**

```typescript
// web/src/lib/__tests__/offlineSync.test.ts
import { describe, it, expect, vi } from 'vitest';
import { syncProjectToCache, loadFromCache } from '../offlineSync';

describe('offlineSync', () => {
  it('syncProjectToCache writes to IndexedDB', async () => {
    const putSpy = vi.fn();
    vi.mock('../offlineDb', () => ({
      OfflineDB: class { putProject = putSpy; },
    }));
    await syncProjectToCache({ id: '/p', name: 'p', data: {} } as any);
    expect(putSpy).toHaveBeenCalled();
  });

  it('loadFromCache returns undefined when offline DB is empty', async () => {
    const result = await loadFromCache('project', '/nonexistent');
    expect(result).toBeUndefined();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/lib/__tests__/offlineSync.test.ts`
Expected: FAIL with "Cannot find module '../offlineSync'"

- [ ] **Step 3: Implement offlineSync.ts**

Create `web/src/lib/offlineSync.ts` with:
- `syncProjectToCache(project)`: calls `db.putProject` with `lastSynced: Date.now()`
- `syncSessionToCache(session)`: calls `db.putSession` with messages clipped to last 200, `lastSynced: Date.now()`
- `syncTerminalToCache(terminal)`: calls `db.putTerminal` with scrollback clipped to 1000 lines, `lastSynced: Date.now()`
- `loadFromCache(type, key)`: calls appropriate `db.get*` method

- [ ] **Step 4: Hook into client.ts**

Modify `web/src/api/client.ts`:
- After successful `fetchJSON` for project list: call `syncProjectToCache` for each project
- After successful `fetchJSON` for session: call `syncSessionToCache`
- After successful terminal history fetch: call `syncTerminalToCache`
- Import and use lazily (dynamic import to avoid loading IndexedDB when not needed)

- [ ] **Step 5: Run tests**

Run: `cd web && npx vitest run src/lib/__tests__/offlineSync.test.ts`
Expected: PASS

## Task 4: Offline Status Hook

**Risk:** standard — straightforward React hook wrapping browser APIs

**Files:**
- Create: `web/src/hooks/useOfflineStatus.ts`
- Test: `web/src/hooks/__tests__/useOfflineStatus.test.ts`

**Interfaces:**
- Consumes: `OfflineDB` from Task 2, `loadFromCache` from Task 3
- Produces: `useOfflineStatus()` hook returning `{ isOffline: boolean, lastSynced: number | null, cachedDataAvailable: boolean }`

- [ ] **Step 1: Write the failing test**

```typescript
// web/src/hooks/__tests__/useOfflineStatus.test.ts
import { describe, it, expect, vi } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useOfflineStatus } from '../useOfflineStatus';

describe('useOfflineStatus', () => {
  it('returns isOffline false when navigator.onLine is true', () => {
    vi.stubGlobal('navigator', { onLine: true });
    const { result } = renderHook(() => useOfflineStatus());
    expect(result.current.isOffline).toBe(false);
  });

  it('returns isOffline true when navigator.onLine is false', () => {
    vi.stubGlobal('navigator', { onLine: false });
    const { result } = renderHook(() => useOfflineStatus());
    expect(result.current.isOffline).toBe(true);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/hooks/__tests__/useOfflineStatus.test.ts`
Expected: FAIL with "Cannot find module '../useOfflineStatus'"

- [ ] **Step 3: Implement useOfflineStatus.ts**

Create `web/src/hooks/useOfflineStatus.ts` with:
- Track `navigator.onLine` state
- Listen to `online`/`offline` events
- Track whether cached data is available (check IndexedDB has entries)
- Expose `isOffline`, `lastSynced` (most recent sync timestamp), `cachedDataAvailable`
- When offline, attempt to load from cache

- [ ] **Step 4: Run tests**

Run: `cd web && npx vitest run src/hooks/__tests__/useOfflineStatus.test.ts`
Expected: PASS

## Task 5: Offline Indicator UI and Write Gating

**Risk:** standard — UI component + conditional rendering

**Files:**
- Create: `web/src/components/OfflineBanner.tsx`
- Modify: `web/src/App.tsx` (render OfflineBanner, pass isOffline to child components)
- Modify: `web/src/components/ChatInput.tsx` (disable send when offline)
- Modify: `web/src/components/Terminal/TerminalPanel.tsx` (disable input when offline)
- Test: `web/src/components/__tests__/OfflineBanner.test.tsx`

**Interfaces:**
- Consumes: `useOfflineStatus` from Task 4
- Produces: `OfflineBanner` component

- [ ] **Step 1: Write the failing test**

```typescript
// web/src/components/__tests__/OfflineBanner.test.tsx
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { OfflineBanner } from '../OfflineBanner';

describe('OfflineBanner', () => {
  it('renders nothing when online', () => {
    const { container } = render(<OfflineBanner isOffline={false} lastSynced={null} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('shows offline message when offline', () => {
    render(<OfflineBanner isOffline={true} lastSynced={Date.now() - 60000} />);
    expect(screen.getByText(/offline/i)).toBeInTheDocument();
    expect(screen.getByText(/last synced/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/components/__tests__/OfflineBanner.test.tsx`
Expected: FAIL with "Cannot find module '../OfflineBanner'"

- [ ] **Step 3: Implement OfflineBanner.tsx**

Create `web/src/components/OfflineBanner.tsx` with:
- Props: `isOffline: boolean`, `lastSynced: number | null`
- Returns null when `isOffline` is false
- When offline: renders a banner with "Offline — showing cached data" and "Last synced: X ago"
- Styled as a non-intrusive top banner (yellow/amber background)

- [ ] **Step 4: Wire into App.tsx**

Modify `web/src/App.tsx`:
- Call `useOfflineStatus()`
- Render `<OfflineBanner isOffline={isOffline} lastSynced={lastSynced} />` at the top of the app
- Pass `isOffline` to `ChatInput` and `TerminalPanel` via props

- [ ] **Step 5: Add write gating**

Modify `ChatInput.tsx`: disable send button when `isOffline` is true, show tooltip "Offline — cannot send"
Modify `TerminalPanel.tsx`: disable terminal input when `isOffline` is true

- [ ] **Step 6: Run tests**

Run: `cd web && npx vitest run src/components/__tests__/OfflineBanner.test.tsx`
Expected: PASS

## Self-Review Checklist

1. **Spec coverage:** All Feature A requirements covered? Service worker (Task 1), IndexedDB (Task 2), sync (Task 3), offline detection (Task 4), UI (Task 5). ✓
2. **Step scan:** Each step has one clear action. ✓
3. **Type consistency:** `OfflineDB`, `CachedProject`, `CachedSession`, `CachedTerminal` types defined in Task 2, consumed in Tasks 3-5. ✓
4. **Review Focus:** Each item addressed:
   - "Last synced" indicator → Task 5 (OfflineBanner shows it)
   - Logout purge → Task 2 (clearAll) + Task 1 (SW cache clear on logout)
   - Terminal scrollback secrets → Task 2 (1000 line limit) + Task 3 (clip to 1000)
   - No /api/* in SW → Task 1 (network-only for /api/*)
   - navigator.onLine true but server unreachable → Task 4 (fetch failure triggers offline)
5. **Proportion:** 5 tasks, each with 4-6 steps. Reasonable for the feature scope. ✓
6. **Detail level:** Decisions, not code. Test snippets included. ✓

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-11-08-offline-read-cache.md`. Please review the plan. Which execution approach would you prefer?

- **Subagent-driven** - A fresh subagent implements each task and a fresh reviewer checks it before the next one starts, then a whole-branch review at the end. Most thorough; costs a fresh context per task and per review.
- **Native** - I implement every task myself in this session, the way this harness runs work, then one fresh reviewer on the most capable model checks the whole branch. Cheapest and fastest; no independent review until the end. Runs well with a mid-tier session model, since the plan carries the design.

For this plan I recommend **Native**, because the tasks are sequential (each depends on the previous), there are only 5 tasks, and the plan carries enough detail for a single implementer to work through them without per-task review gates.

Does the plan capture what you want, and which approach should we use?
