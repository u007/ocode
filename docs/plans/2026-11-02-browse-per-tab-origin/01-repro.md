# 01 Repro (failing first)
- FIRST and primary: Chrome-gated test against TODAY's prefix mode (compiles,
  fails on assertion). A Go test against a not-yet-existing handler is a build
  failure, not a repro. Later item: run the `local_test.go` suite in root mode,
  especially the service-worker block (stable ports would persist a root SW).
- Chrome-gated test (`repro` tag, `OCODE_CHROME_PATH`): fixture page that
  reports `location.pathname` to the test; assert it equals the upstream path
  under per-tab origin; keep a prefix-mode case asserting current behavior.
- Fixture mimics a TanStack SPA: serves index.html for any path, script reads
  `location.pathname` before rendering.
- Acceptance variant: same fixture reached through the remote-host tunnel.
