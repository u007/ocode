# 02 Per-tab listener and binding
- New per-tab listener manager in `internal/server` next to the browse proxy
  (`server.go` ~1287): TLS via `NewSniffTLSListener(localTLS)`, bound to
  the SPA hostname like `proxyHost` (from `spaOrigin`), NOT hardcoded 127.0.0.1:
  a web client on `localhost:4096` framing `https://127.0.0.1:P` is cross-site and
  the per-tab cookie becomes third-party (blocked), one `(stateKey, scheme, host)` per listener, handler root-mounted.
- `internal/browse`: handler variant of `handleBrowse` that takes the target
  from the binding instead of `parseTarget`; keeps the `t.StateKey != stateKey`
  check, local-mode gate, SSRF dial guard.
- Cap listeners per tab; close all on tab close/revoke.
- Per-tab auth cookie name (encoded/hashed stateKey) (`auth.go`, `browseCookie` is a single constant today).
  Cookie Path is `/` (root mount), no prefix.
- Listener lifecycle through the existing shutdown path; closed on panel close/revoke.
- Stable port: request the persisted port for the stateKey first (see part 05);
  if taken, bind a new one and log it (storage for that tab resets).
