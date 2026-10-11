# Design: Cloud Storage Connectors (Drive, OneDrive, GCS, S3)

Status: DRAFT (awaiting user confirmation before code)
Author: ocode agent
Date: 2026-10-08

## Scope (per user confirmation)
- Both global + per-project settings (global defaults, per-project overrides).
- Providers: Google Drive, OneDrive, Google Cloud Storage, Amazon S3.
- Full read/write/upload in files tab (browse, open, edit, upload, delete).
- Settings UI to manage connectors.
- Files tab integrates cloud trees alongside local trees.

## Recommendation (agent opinion)
Full 5-provider full-read-write is a very large change (OAuth flows, SDKs/imports, settings schema, file tab tree integration, conflict handling, security review for cloud credentials). Recommend phased:

Phase 1: Connector framework + settings model (global + per-project) + Google Drive only (read-only browse in files tab).
Phase 2: OneDrive + GCS + S3 (same framework).
Phase 3: Full write/upload/delete + conflict handling + security review.

But user selected "Full implementation" — proceed if confirmed.

## Key architecture questions for user
1. Where do cloud credentials live? (ocodeconfig.json per project? A new .ocode/storage.json? System keychain?)
2. File tab shows cloud trees as separate root nodes or merged with local? (Recommend separate root branch per connector to avoid name collisions.)
3. Conflict handling strategy for write? (Lock-file style? Last-write-wins? User-resolve dialog?)
4. Should cloud files be editable in Monaco editor directly (download-then-upload) or open read-only copy?

## Expanded design (all 4 providers, phased)

Providers: Google Drive (OAuth 2), OneDrive (Microsoft Graph OAuth), GCS (service account / HMAC key), S3 (access key + secret / IAM role).

Phase 1 — Connector framework + settings model + Drive + S3 (read-only browse/open):
- Go: connector interface (ListRoot, ListDir, ReadFile, DownloadToCache, GetMetadata).
- Go: settings loader/saver (new `.ocode/storage-connectors.json` per project; global defaults in `~/.config/opencode/storage-connectors.json`). NEVER in ocodeconfig.json.
- Go: OAuth token storage: OS keychain (darwin Keychain / linux secret-service / windows credential manager) for refresh/access tokens; non-secret metadata (provider id, root/folder, project link) in JSON settings file.
- Web: Settings → Connectors panel (add/edit/remove per provider, global vs project toggle).
- Web: Files tab shows cloud trees as separate virtual root nodes (`gdrive://...`, `s3://bucket/...`) — does NOT merge with local paths.
- Files tab: browse, open in editor (download to temp cache, open in Monaco as read-only initially; conditional upload on save for Phase 3).

Phase 2 — OneDrive + GCS: reuse framework, add provider-specific auth handlers.

Phase 3 — Write/upload/delete + conflict handling:
- Conditional writes using provider version tokens (Drive `md5Checksum`/`revision`; OneDrive `eTag`; GCS `generation`; S3 `If-Match` ETag).
- Conflict → user dialog (reload/overwrite/save-as), never auto-merge.
- Security: new connector-related bash commands added to sandbox ask list; `auth.json` pattern reused for token reference only (actual tokens in keychain); `.env.example` updated with optional `GDRIVE_CLIENT_ID`, `S3_BUCKET` references (not secrets).

## Cache management (user addition — 2026-10-08)
- Max caching size: configurable (default e.g. 500MB), stored in per-project `.ocode/storage-connectors.json` as `max_cache_bytes` and globally.
- Current cache size: computed at session start / on settings change by scanning download cache directory (`~/.local/share/opencode/project/<slug>/cloud_cache/` or similar); displayed in Settings → Connectors panel and optionally in files-tab status line.
- Clear cache button: settings UI button that deletes all cached files (not settings, not credentials); confirms with user dialog; updates displayed size to 0; safe even mid-session (downloads will re-fetch on next open).
