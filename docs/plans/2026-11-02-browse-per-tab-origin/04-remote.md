# 04 Remote projects
- The remote host's browse server must run in root mode itself (it produces the
  HTML rewrite, capture.js injection, redirects, cookie paths). The binding travels
  in the request header `X-Ocode-Browse-Root-Mode: scheme://host`, not in the path:
  a foreign upstream is forwarded on the same `/b/` path, so the path cannot tell the
  binding from a foreign upstream. The host replies with `X-Ocode-Browse-Root-Mode: 1`
  on every reply, and a reply without that marker is never relayed (502, restart the
  host). An older remote ignores the header and is killed/replaced on connect.
- Local per-tab listener for a remote project forwards to the host's browse
  listener over the workspace tunnel (`handler_remote_browse.go`), mounting the
  host's `/b/{stateKey}/{scheme}/{host}` prefix itself so the browser sees root.
- Drop the `Path=/b/` -> `Path={prefix}/b/` rewrite for root-mounted tabs.
- Keep: saved-host check, token stays server-side, 101 subprotocol restore,
  503 + Retry-After while connecting, no fallback to local browse.
