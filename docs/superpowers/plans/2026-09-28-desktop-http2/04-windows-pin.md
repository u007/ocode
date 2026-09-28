# 04 — Windows WebView2 pin

- `cmd/ocode-desktop/main.go`: set `application.Options.Windows.AdditionalBrowserArgs`
  to include `--ignore-certificate-errors-spki-list=<base64 SPKI sha256>`
  (Chromium's pin flag; only the pinned key is exempt).
- Cert must exist before `application.New`.

**Verify (macOS proxy):** Chrome with the same flag + `--user-data-dir=<tmp>`
loads the https SPA and terminals work over h2 Chromium.
**Runtime on Windows:** build-verified only; TODO.md entry.
