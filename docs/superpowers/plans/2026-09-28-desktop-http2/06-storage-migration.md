# 06 — One-time localStorage migration http → https

- Marker file next to `desktop-port` (e.g. `desktop-https-migrated`) under the
  desktop config dir. Absent → migration run.
- Shell: when marker absent, initial webview URL is the **http** origin with a
  `migrateTo=<https app URL>` param.
- SPA early boot (before React renders, `web/src/main.tsx`): if `migrateTo`
  present → collect every localStorage entry (the origin is ours alone), POST them to a new
  token-gated endpoint (`/api/desktop/storage-migration`, in-memory, one-shot),
  then `location.replace(migrateTo)`.
- On the https origin early boot: GET the endpoint; if a payload is pending,
  write keys that are absent, then continue rendering. Server clears the
  payload on read and the desktop writes the marker.
- Failure of any step logs and proceeds to the https URL (user was told state
  may reset only if migration fails).

**Verify:** Go test for endpoint one-shot semantics; web test for export/import
helpers; manual: tabs + a draft survive the upgrade launch.
