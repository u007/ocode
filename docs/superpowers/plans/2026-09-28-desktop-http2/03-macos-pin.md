# 03 — macOS WKWebView pin

- `cmd/ocode-desktop/native_darwin.m`: at startup (before any window/delegate
  is created) add `webView:didReceiveAuthenticationChallenge:completionHandler:`
  to Wails' `WebviewWindowDelegate` class via `class_addMethod` (it does not
  implement it in beta.12). Implementation: for
  `NSURLAuthenticationMethodServerTrust` on host 127.0.0.1/localhost, compare
  the leaf certificate DER with the pinned DER; match → use credential for
  trust; anything else → `performDefaultHandling`.
- `native.go` / `native_darwin.go`: Go entry passes the DER bytes to C.
- Stub for non-darwin stays a no-op.

**Verify:** SPA loads over https in the .app; fetches, SSE, terminal `wss://`
all work; a different self-signed cert is still rejected.
