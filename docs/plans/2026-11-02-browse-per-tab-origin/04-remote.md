# 04 Remote projects
- The remote host's browse server must run in root mode itself (it produces the
  HTML rewrite, capture.js injection, redirects, cookie paths). Add a header
  alongside `BasePathHeader` carrying the binding; ignored safely by an older
  remote (older remotes are killed/replaced on connect); never guessed from path.
- Local per-tab listener for a remote project forwards to the host's browse
  listener over the workspace tunnel (`handler_remote_browse.go`), mounting the
  host's `/b/{stateKey}/{scheme}/{host}` prefix itself so the browser sees root.
- Drop the `Path=/b/` -> `Path={prefix}/b/` rewrite for root-mounted tabs.
- Keep: saved-host check, token stays server-side, 101 subprotocol restore,
  503 + Retry-After while connecting, no fallback to local browse.
