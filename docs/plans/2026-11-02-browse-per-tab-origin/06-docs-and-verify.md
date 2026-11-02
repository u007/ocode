# 06 Docs and verification
- Update `docs/concepts/browse-*.md`, `browser-agent-tools.md`, add a concept
  page for the per-tab origin; update `docs/index.md`, `docs/log.md`, CLAUDE.md
  if an invariant changes. Re-derive any cited `file.go:NN` anchors.
- Verify in the real desktop app (WKWebView) that an iframe on
  `https://127.0.0.1:<port>/` passes the cert pin; then verify the aimsai2 TanStack
  tab over the SSH remote, reload, restart-and-keep-storage.
