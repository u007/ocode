# 03 Rewrite and capture root-mode
- `rewrite.go`: same-upstream URLs stay root-relative/unchanged; other-upstream
  subresource URLs map to `/__ocode/b/{scheme}/{host}/…` on the same origin;
  top-level cross-upstream navigation is NOT a 302: the new origin has no auth
  cookie (401) and skips the local-mode gate. It goes through the SPA (see 05).
- `capture.js`: explicit root-mode branch for `mountBase`/`browseBase`/
  `routeBase`/`originalURL`/`reroute`; mode is injected in `__OCODE_BROWSE__`,
  not guessed from the pathname. Prefix mode path unchanged.
- `rootrelative.go`: not needed in root mode; keep for prefix mode.
- Cookie `Set-Cookie` Path handling follows the root mount.
