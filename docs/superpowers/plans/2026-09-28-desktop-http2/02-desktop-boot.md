# 02 — Desktop boot serves https

- `internal/desktop/boot.go` `StartServer` and `startRemoteServer`: generate the
  cert (or accept one generated earlier by `cmd/ocode-desktop` — Windows needs
  the SPKI hash before the Wails app is created), serve through the sniffing
  listener, and return `Handle.URL = https://127.0.0.1:PORT` plus the cert
  pin material on `Handle`.
- Keep the saved-port logic unchanged so the https origin is port-stable.
- Debug handle (`saveDebugHandle`) keeps writing the **http** URL — plain HTTP
  still works on the same port, and every CLI/tool consumer stays unchanged.
- Browse origin (`server.StartBrowse`, `SetSPAOrigin`): pass the https SPA
  origin. Verify the embedded browser panel (iframe + `ws://` CDP socket from
  an https page). If WebKit blocks it as mixed content, serve the browse
  listener through the same sniffing TLS listener and make `getBrowseBase`
  return https.
- `cmd/ocode-desktop/main.go`: create cert before `application.New` so
  platform options (Windows browser args) can carry it; pass to boot.

**Verify:** desktop launches, SPA loads over https, `lsof` shows ~1 webview
connection, `curl -k` reports HTTP/2, `curl` http still 200.
