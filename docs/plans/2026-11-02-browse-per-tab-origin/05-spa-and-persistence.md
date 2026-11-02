# 05 SPA origin selection and persistence
- New server endpoint: allocate/reuse the listener for `(stateKey, scheme, host)`
  and mint the grant; flow is nav event -> SPA -> endpoint -> iframe load (same as
  the existing external handoff). `/api/browse/config` is per host so cannot
  return per-tab per-upstream origins.
- SPA `postMessage` origin checks (`restore-scroll`, nav/title listeners) validate
  against that tab's origin, not the single browse base (security).
- Persist keyed by `(stateKey, scheme, host)`. Desktop WKWebView data store: no
  non-persistent store found in Wails v3 beta.12 or ocode-desktop (grep), so
  default persistent; confirm at runtime before relying on stable ports.
- Shipped differently: `GET /api/browse/config` is unchanged and stays per host. The tab
  origin comes from a separate `POST /api/browse/tab-origin` (`handleBrowseTabOrigin`),
  called on every load. Non-local clients and remote-mode servers get 409
  `tab origin unavailable: ...`; the SPA (`openBrowseTabOrigin`) remembers that per host
  for the session and loads prefix mode. Any other error is a visible tab error, never a
  prefix fallback. A 503 with `Retry-After` means connecting, except on the remote POST
  path, where the header is not copied yet. `readBrowseOrigin` is unchanged;
  `BrowserPanel.loadInto` calls `openBrowseTabOrigin`. See
  `docs/concepts/browse-per-tab-origin.md`.
- `web/src/api/client.ts` `readBrowseOrigin` and `BrowserPanel.tsx` `loadInto`
  use the per-tab origin; iframe `src` is the upstream path.
- Persist `(stateKey,scheme,host) -> port` under `GlobalDataDir()/browse` (cross-process lock,
  merge not replace) so localStorage/IndexedDB survive restart.
- Persist the server-side site cookie jar (`cookiejar.go`) to disk the same way;
  drop expired and session-only cookies on load. NEW SCOPE: confirm with user.
