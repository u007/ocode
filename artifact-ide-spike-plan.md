# Artifact IDE Spike Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (default) or superpowers:subagent-driven-development (long plans of independent tasks) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove that an artifact served from kakiit can read and write shared data through a postMessage bridge without giving the artifact page cookies, a bearer token, or the ocode app origin.

**Architecture:** kakiit serves a wrapper page that owns the viewer's access cookie and hosts the artifact in a sandboxed iframe. The wrapper sends a per-load nonce to the iframe by postMessage immediately after load; the nonce never appears in the URL or in artifact HTML. The artifact posts storage requests back; the wrapper validates `event.source`, the opaque `event.origin` (`null`) and the nonce, then calls a viewer-scoped storage endpoint. ocode's sidebar gets a minimal artifact tab to exercise the path end-to-end. This plan proves the bridge only; it does not build the full IDE UI.

**Tech Stack:** kakiit (Hono 4, Drizzle/Postgres, bun test), ocode web (React, Vitest), Go server unchanged.

**Spec:** `proposal-artifact-ide.html` (§8 blockers, §10 first slice and gate).

**Detail:** decisions, not code — the implementer writes the implementation.

**Plan location:** repo root, not `docs/superpowers/plans/`, because `docs/` is the OKF bundle and only `okf:context-writer` may write it.

## Global Constraints

- Public serving stays under `/p` only, built through `newArtifactTailscaleShare()`; a public funnel requires `authConfigured()`.
- Wrapper `frame-ancestors` allows `'self'`, http/https loopback on any port, and an `embedOrigin` query param validated as origin-only; artifact content keeps `frame-ancestors 'none'`. Loopback wildcard means any local process can frame the wrapper.
- Private embeds pass the existing 7-day signed access grant as `access` in the wrapper URL and storage requests; the server rechecks password fingerprint or allowlist row. Unlock/invite return the grant as JSON when `Accept: application/json`. Wrapper sends `Referrer-Policy: no-referrer` and `Cache-Control: no-store`.
- Artifact pages keep `sandbox allow-scripts` with no `allow-same-origin`. For this spike `connect-src` stays limited to the hub origin — no external hosts.
- The wrapper delivers a per-load nonce by postMessage right after load — never in the URL or artifact HTML. It accepts a later message only when `event.source === iframe.contentWindow`, `event.origin === "null"`, and the nonce matches.
- Owner routes use sync Bearer; public routes use the opaque capability token and existing signed, mode-bound access cookies. Unknown artifact/token answers a uniform 404.
- Spike scope is the **shared pool only**. Personal pool, anonymous viewer identity, quota, retention and abuse controls stay in §9.
- Drizzle migrations are append-only: generate a new migration, never delete a committed one.
- Do not write ocode `docs/` bundle pages from the main agent; do not commit the proposal or this plan unless asked.

## Review Focus

- A forged `postMessage` from another frame must be rejected, not proxied. Pin in Task 1.
- A signed-out viewer with only an artifact URL must not read stored records. Pin in Task 2.
- A draft preview write must never reach live shared records. Pin in Task 2.
- Generated artifact code must not reach an external host through `connect-src`. Pin in Task 1.
- An unpublished artifact ID must not be distinguishable through the storage API. Pin in Task 2.

---

### Task 1: Bridge wrapper and message protocol (kakiit)

**Risk:** high — auth boundary and untrusted-message handling.

**Files:**
- Create: `src/server/services/ocode-artifacts/bridge.ts`
- Create: `src/server/api/routes/ocode.artifacts.bridge.ts`
- Modify: `src/server/api/routes/ocode.artifacts.serve.ts` (entrypoint branch and CSP)
- Test: `src/server/api/routes/ocode.artifacts.bridge.test.ts`

**Interfaces:**
- Consumes: `resolveVisitorToken`, `getLatestVersion`, `verifyAccessCookie` from the existing ocode-artifacts service; `buildCsp` from the serve route.
- Produces: a wrapper HTML response for `/p/:artifactId/:token` that embeds the artifact iframe and a message handler; a pure `verifyBridgeMessage(event, iframe, nonce)` helper; a per-load nonce delivered only by postMessage.

- [x] **Step 1: Write the failing bridge tests**

Name each test after its assertion: a message from a different `event.source` is rejected; a message whose origin is not the opaque `null` origin is rejected; a stale nonce is rejected; the wrapper route answers 404 for an invalid token. No test may place the nonce in the URL or in artifact HTML.

- [x] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/james/www/kakiit && bun test --serial --isolate --timeout 60000 --preload ./src/test-preload.ts src/server/api/routes/ocode.artifacts.bridge.test.ts`
Expected: FAIL with missing module/helper. The `test:file` wrapper failed with ECONNRESET in this environment, so use direct bun test.

- [x] **Step 3: Implement `verifyBridgeMessage` in `bridge.ts` and the wrapper route**

Decision pinned: the wrapper is served at `/p/:artifactId/:token/~bridge` from the existing serve route; the iframe loads the bare artifact root. The wrapper owns the access cookie; the iframe never receives it. Keep the existing uniform 404 for missing or invalid tokens. The wrapper's own CSP allows its hub-origin script only; the iframe keeps `buildCsp`'s sandbox policy.

- [x] **Step 4: Run the bridge tests again**

Run: the Step 2 command.
Expected: PASS. Also run the existing `ocode.artifacts.serve.test.ts` and `ocode.artifacts.serve.access.test.ts` and `bun check .` — all pass.

### Task 2: Viewer-scoped storage gateway with draft/live separation (kakiit)

**Risk:** high — persistence, authorisation and multi-user data isolation.

**Files:**
- Create: `src/server/db/schema.ts` addition: `ocode_artifact_record`
- Create: a new Drizzle migration under `drizzle/`
- Create: `src/server/services/ocode-artifacts/storage.ts`
- Create: `src/server/api/routes/ocode.artifacts.storage.ts`
- Modify: `src/server/api/index.ts` (mount the storage route under `/api/ocode/artifacts`)
- Test: `src/server/api/routes/ocode.artifacts.storage.test.ts`

**Interfaces:**
- Consumes: `requireSyncToken`, `resolveVisitorToken`, the Task 1 nonce contract.
- Produces: `GET`/`PUT` routes keyed by artifact id, scope (`draft` | `live`) and record key. `draft` accepts only a sync-token owner; `live` accepts the mode-bound viewer cookie. Every record stores the server-side writer.

- [x] **Step 1: Write the failing storage tests**

Name each test after its assertion: unknown artifact id returns 404 with no existence oracle; a signed-out viewer on `live` scope gets 404, not 401; a public capability token on `draft` scope is refused as owner-only; a draft write is invisible on the live scope.

- [x] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/james/www/kakiit && bun test --serial --isolate --timeout 60000 --preload ./src/test-preload.ts src/server/api/routes/ocode.artifacts.storage.test.ts`
Expected: FAIL with missing route/module.

- [x] **Step 3: Add the table, migration, service and routes**

Decisions pinned: migration `0272_stiff_demogoblin.sql` contains only `ocode_artifact_record`. `live` requires a valid artifact token **plus** a mode-bound access cookie, so signed-out public viewers cannot read or write records; `draft` requires sync Bearer ownership. The wrapper fetches `live` only.

The table keys on `(artifactId, scope, key)`. Cap a single record value in the spike and return a uniform 404 for every refusal path. List reads are sorted by key and paginated; pagination shape is a decision for this task. The writer is recorded server-side; the page never sees it.

- [x] **Step 4: Run storage tests and a fresh-DB migration check**

Run: the Step 2 command, then `cd /Users/james/www/kakiit && bun run db:migrate:test`
Expected: PASS on both. Storage tests pass and `bun check .` is clean. The fresh-DB migration check is **blocked**: Postgres connection resets with ECONNRESET in this environment before any migration runs. An environment limit, not a migration error; rerun with a reachable database.

### Task 3: Minimal artifact tab in the desktop preview (ocode web)

**Risk:** standard — new UI surface, no existing behaviour changes.

Before starting this task, dispatch `okf:context` for the MCP-apps widget and artifact-sharing pages. Reuse the existing `srcdoc`/`null`-origin checks rather than building a second implementation. Done: the digest confirmed the opaque-origin contract; the artifact iframe uses `sandbox` with no `allow-same-origin`.

**Files:**
- Modify: `web/src/components/Preview/PreviewHost.tsx` (`Surface` union, tab strip, pane)
- Modify: `web/src/components/Preview/sidebarPreviewState.ts` (restore the new surface)
- Test: `web/src/components/Preview/PreviewHost.artifact.test.tsx`

**Interfaces:**
- Consumes: the Task 1 wrapper URL for an artifact; existing `preview_open` / `ocode:open-preview` activation events.
- Produces: a third sidebar surface that renders the wrapper in a sandboxed iframe and exposes the bridge round-trip for manual verification.

- [x] **Step 1: Write the failing component test**

Name the test after its assertion: the artifact tab is present and the iframe carries `sandbox` without `allow-same-origin`.

- [x] **Step 2: Run the test to verify it fails**

Run: `cd /Users/james/www/ocode/web && pnpm vitest run src/components/Preview/PreviewHost.artifact.test.tsx`
Expected: FAIL — artifact tab absent.

- [x] **Step 3: Add the tab and iframe pane**

Decision pinned: the wrapper URL comes from the app query string `?artifact=` and is accepted only for absolute http(s) URLs. No artifact URL is persisted, and no token is synthesized in the SPA. `SidebarSurface` accepts `artifact`; the v2 storage key is unchanged. This is a proof surface, not the final author UI from §5.

- [x] **Step 4: Run the component test and the existing PreviewHost tests**

Run: `cd /Users/james/www/ocode/web && pnpm vitest run src/components/Preview/PreviewHost.artifact.test.tsx src/components/Preview/PreviewTabPage.activation.test.tsx`
Expected: PASS. Also ran `PreviewHost.test.tsx` (23 tests) and `pnpm typecheck` — all pass.

## Exit test for the spike

With a valid owner login: open an artifact in the new tab, write a record through the bridge, reopen in a second session with the same shared link, and read the record back. Then confirm a forged message and a signed-out storage request both fail, and that a draft write is invisible on the live scope. If any of these cannot be made to work, return to the user before building the rest of the proposal.
