---
type: Gotcha
title: Web model picker must open from the cached model list, not a live refresh
description: 'Gotcha: web model picker slowness — cached open, batch scan, render cap, gzip, configured filter; 2026-11-19 dialog open state moved out of App, Radix menu only on removable rows, deferred row mount on reopen'
resource: ""
tags:
  - gotcha
  - web
  - model-picker
  - performance
  - api
  - refresh
  - batch-scan
  - render-cap
  - models-registry
  - gzip
  - configured
  - payload
  - re-render
  - radix
  - context-menu
  - deferred-value
  - host-component
timestamp: 2026-11-19T15:25:32Z
---
# Web model picker must open from the cached model list, not a live refresh

**Type:** Gotcha  
**Description:** Web model picker slow: live refresh on open + per-model dir scan + no render cap + large uncompressed payload + full registry shipped to every consumer; on 2026-11-19 also the dialog's open state in the root App re-rendering the whole app, a Radix menu on every row, and the full row list mounting inside the click  
**Resource:** web/src/components/Layout/ModelDialog.tsx; web/src/components/Layout/ModelDialogHost.tsx; web/src/App.tsx; internal/agent/models_registry.go; internal/server/handler.go; internal/agent/context.go; web/src/components/Layout/modelSelection.ts; internal/server/gzip.go; internal/server/handler_models.go  
**Tags:** gotcha, web, model-picker, performance, api, refresh, batch-scan, render-cap, models-registry, gzip, configured, payload, re-render, radix, context-menu, deferred-value, host-component  

---

# Web model picker must open from the cached model list, not a live refresh

Fixed 2026-09-19. Symptom: **"changing model on web ui is super slow, sometimes took 10s."**

Measured on the running server: `GET /api/config/model`, `/api/config/small-model`,
`/api/config/advisor`, `/api/tui-status` all sub-millisecond. Only the model list was slow:
`GET /api/models?refresh=true` = 2.1–5.9s (variable), `GET /api/models` cached = 1.3–1.5s;
payload 1.2–1.3 MB / ~8,207 models / 223 providers.

## KEY LESSON / GENERAL RULE

1. **Never put `refresh: true` (or any live provider fetch) on a UI open path.** Live model
   fetches are sequential HTTP calls (LM Studio, Requesty, Novita, OpenRouter) with 2–10s
   timeouts. Make refresh an explicit user action (the web dialog now has a Refresh button).
2. **Never call `LoadModelContextWithSourceAt` per model inside a list handler.** Each call does
   `os.ReadDir` on up to 3 directories; 8,000 models × 3 dirs = ~24,600 dir scans ≈ 1.4s even
   on the cached path. Use a batch scan API instead.
3. **Cap rendered rows in the model picker when the list is very large.** 8k rows mount slowly
   (~1.5s). Cap unfiltered provider groups and let search still filter the full list so nothing
   is unreachable. Never cap Recents or Favorites.
4. **Gzip large API payloads explicitly — Go's standard library has no transparent compression.**
   A 1.2 MB model list gzips to ~120 KB. Wire compression at the serving boundary, never on
   SSE/WebSocket upgrade paths.
5. **Ship only the consumer's configured providers, not the full registry.** A 19-credential
   account only needs ~19 providers (~1,650 models) of the 223-provider (8,207 model) registry.
   Filter server-side behind a `configured=true` opt-in so other consumers are unaffected.
6. **Keep a dialog's open state in a small host component, not in a large root component.** Opening or closing it re-renders everything below the root. Expose `open(purpose)` through a ref (§6).
7. **Do not wrap every list row in a Radix menu root when only a few rows have a menu.** Wrap only the rows that have menu content (§7).

## 1. Dialog opened with `refresh: true` — live provider fetch on every open

`web/src/components/Layout/ModelDialog.tsx` passed `{ refresh: true }` on every open. The server
mapped that to `agent.AllProviderModels()`, which does LIVE network fetches (sequential; 2–10s
HTTP timeouts) in `internal/agent/models_registry.go` (`allProviderModelsFromRegistry(refresh=true)`).
The dialog rendered nothing until it resolved.

**TUI parity:** `internal/tui/picker.go openModelPicker` opens from
`AllProviderModelsCached()` and only live-fetches on ctrl+r (`refreshModelsCacheCmd`).

**Fix:** web dialog now opens from the cached list; a Refresh button triggers `refresh: true`.

## 2. `HandleListModels` scanned model context dirs per model

`HandleListModels` (`internal/server/handler.go`) called `h.annotateModelFlags`, which looped
over all models and called `agent.LoadModelContextWithSourceAt(root, name)` per model. That
function does `os.ReadDir` on up to 3 dirs (root, root/.opencode, ~/.config/opencode) per call →
~24,600 directory scans for 8,207 models ≈ 1.4s even on the cached path. The old comment
claiming it "reads only matching files" was false.

**Fix:** `scanModelContextDirs` (one scan) + `matchContextFile` + exported
`ModelContextKindsAt(root, ids)` batch API in `internal/agent/context.go`, preserving
exact/prefix-wildcard/exact-beats-wildcard-per-dir/first-dir-wins precedence.

**Benchmark (8,000 ids): 918ms → 0.7ms.**

## 3. Rendering ~8k rows was slow

Mounting ~8,000 DOM rows took ~1.5s even after the backend was fast.

**Fix:** `capProviderGroups` (`web/src/components/Layout/modelSelection.ts`,
`MAX_VISIBLE_PROVIDER_MODELS = 500`) caps unfiltered provider rows and shows
"N more models not shown — type to refine your search". Search still filters the FULL list so
nothing is unreachable. Recents and Favorites are never capped.

## 4. Large /api payloads must be gzipped — Go does not auto-compress

After fixing refresh, dir scans, and render cap, the model list was still slow: `GET /api/models`
shipped **~1.2 MB identity-encoded** (1,203,586 bytes; 8,207 models / 223 providers). The Go
standard library does not compress responses — no `Content-Encoding: gzip` is applied
automatically. gzip of the same body = **~120 KB** (~10x reduction).

**Fix:** `internal/server/gzip.go` — a gzip middleware wired at the serving boundary in
`serveHandler` as `gzipMiddleware(corsMiddleware(s.mux.ServeHTTP))`.

**Rules for any future middleware hand-rolling gzip:**

- Only compress an allowlist of content types; explicitly **exclude `text/event-stream`** (SSE)
  and never compress a WebSocket upgrade.
- The wrapper MUST implement `http.Flusher` and `http.Hijacker` by delegation (gorilla/websocket
  hijacks `/api/terminal/ws`; SSE handlers type-assert `http.Flusher`) and provide `Unwrap()`
  for `http.ResponseController`. Compile-time assertions pin this.
- Set `Vary: Accept-Encoding`; delete `Content-Length` on compression (chunked).
- Skip 1xx/204/304/206 and small (<1 KB) known-length bodies.

**Tests:** `internal/server/gzip_test.go`.

## 5. Filter the model list to configured providers server-side

After the cached-open fix and gzip, the remaining cost was payload size for the web consumer.
`HandleListModels?configured=true` (opt-in; default unchanged for API compatibility) drops
registry providers with no credential/config.

**Configured =** any auth store entry (`auth.List`, including provider ids not in the auth
registry such as `xiaomi-token-plan-sgp`), a set provider env var, a config `provider` block,
or `agent.KeyOptionalProvider(id)` (free/local: opencode, opencode-go, lmstudio, local — added
so free zen tiers aren't hidden). Recents and favorites are added **before** the filter and are
never dropped; the current model's provider is always kept.

**Do NOT resolve/refresh OAuth tokens in the list path** — no network in a list request. Use
`auth.List`/`auth.Get`, not `auth.ResolveKey`.

**Result for a 19-credential account:** 8,207 → ~1,650 models, 223 → ~19 providers. Web
`ModelDialog` sends `configured=true` on open and on Refresh; other/opencode-compatible consumers
still get the full registry.

**Tests:** `internal/server/handler_models_configured_test.go`.

## 6. Dialog open state in the root `App` re-rendered the whole app (2026-11-19)

Symptom (desktop app): every click on the model opened and closed the dialog with lag, and it got
slower as the session went on.

**Measurement setup for §6 and §8:** headless Chromium against vite dev, the configured list of 1,435
models (538 rendered rows after the 500-row cap, plus recents and favorites), 5 open/close cycles. Not
measured in the WKWebView production build. §7 is a jsdom benchmark.

`HomeApp` (`web/src/App.tsx`) owned the dialog's open state (`modelDialogOpen`, `modelDialogTab`), so each
open and close re-rendered the whole app. A CPU profile showed `FileTree` `TreeNode`, `ProjectSidebar`
rows, `GitPanel`, `TerminalTabs` and `CronPanel` rendering on every toggle. That cost grows with app state
(open sessions, tree size), which is why it felt slower over time.

**Fix:** `ModelDialogHost` (`web/src/components/Layout/ModelDialogHost.tsx`) owns the open state and the
purpose, and exposes `open(purpose)` through a ref (`ModelDialogHostHandle`). App calls
`modelDialogRef.current?.open(tab)` from `openModelDialog`. The host lives in its own file on purpose:
18 App test files mock `./components/Layout/ModelDialog` with only a default export, so a named
`ModelDialogHost` export placed in that module would be missing under those mocks.

Close-to-paint went from ~105 ms to ~60 ms. The profile shows no App-level component rendering on a toggle.

## 7. A Radix `ContextMenu` on every model row

Every row was wrapped in a Radix `ContextMenu`, but only removable "Recently Used" rows have menu content.
Now only `canRemove` rows (a recent whose id is `provider/model`) get the wrapper, in the row renderer of
`ModelDialog.tsx`. Other rows keep `onContextMenu` preventDefault, so the native menu stays suppressed as
before.

jsdom benchmark: close ~75 ms → ~36 ms; open ~145 ms → ~95 ms.

## 8. Reopen mounted ~538 rows inside the click

On a reopen, the cached `models` state mounted every row synchronously inside the click, which blocked the
dialog's first paint (~330 ms open-to-paint in dev). `listReady = useDeferredValue(open)` in
`ModelDialog.tsx` gates the list: the shell paints first (~35 ms) and the rows mount at deferred priority
(~250 ms later in dev mode). The navigation model is gated with the rows (`navIds = listReady ?
modelNavIds : NO_NAV_IDS`, an empty `readonly string[]`), so a keypress in that window cannot target a
row that is not mounted.

## Open: duplicate Radix dismissable-layer versions (not fixed)

`pnpm why @radix-ui/react-dismissable-layer` in `web/` shows two versions. The app's own Radix packages
resolve 1.1.19; 1.2.1 is pulled only through `@univerjs/design` (dropdown-menu and hover-card). Not changed.

## Regression tests

- `internal/agent/context_test.go` `TestModelContextKindsAt_AgreesWithPerModelLookup`
- `internal/agent/context_bench_test.go` (new)
- `web ModelDialog.test.tsx` ("opens from the cached list and never blocks on a live refresh",
  "Refresh fetches live provider lists…", provider render cap)
- `web modelSelection.test.ts` capProviderGroups
- `web ModelDialog.test.tsx` `ModelDialogHost` case: `open(purpose)` through the ref shows the dialog and
  the parent does not re-render
- `web dialogStreaming.test.tsx` host assertion updated
- `internal/server/gzip_test.go` (gzip middleware: compression, content-type allowlist, SSE/WebSocket
  exclusion, Flusher/Hijacker delegation, Unwrap, Vary header, small-body skip)
- `internal/server/handler_models_configured_test.go` (configured filter: credential-based
  inclusion, recents/favorites preservation, current-model provider kept, default full registry)
