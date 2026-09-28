# 05 — Linux WebKitGTK allow

- New cgo file(s) in `cmd/ocode-desktop` for linux: build tag `gtk3`
  (shipped, webkit2gtk-4.1) calls
  `webkit_web_context_allow_tls_certificate_for_host(default_context, cert, "127.0.0.1")`;
  `!gtk3` (webkitgtk-6.0) calls
  `webkit_network_session_allow_tls_certificate_for_host(default_session, …)`.
  Wails uses the default context/session, so this applies to its webview.
- Build `GTlsCertificate` from PEM via `g_tls_certificate_new_from_pem`.
- Must run after GTK init and before the window navigates.

**Verify:** cross build via `Dockerfile.cross` compiles. Runtime on Linux:
build-verified only; TODO.md entry.
