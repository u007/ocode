Plan: Connector framework Phase 1 — settings, encryption, cache UI
Level: default (decisions, not full code). Feature: bounded Phase 1.
Risk: high (security/auth, settings persistence, cross-file).

Scope (confirmed): global+project; 4 providers; encryption via user master password (Argon2id + AES-256-GCM); settings JSON (`.ocode/storage-connectors.json`) NOT `ocodeconfig.json`; separate virtual root per connector; max_cache_bytes + current size display + destructive clear button.

Task 1 — Settings model (standard)
- Create `internal/config/storage_connectors.go`: ConnectorConfig struct (global+project overrides), SettingsFile struct (encrypted JSON envelope).
- Load/save: read `.ocode/storage-connectors.json` from GlobalDataDir() (global) and project root `.ocode/` (per-project override); save atomically (write temp + rename).
- No code for full provider auth yet — only framework fields (provider_type, root_path, max_cache_bytes, encrypted flag).
- Tests: load, save, override merge, encrypted envelope parse.

Task 2 — Encryption wrapper (high — security)
- Go package: use `crypto/argon2`, `crypto/aes`, `crypto/cipher` (GCM).
- Envelope format: `ENCv1:` + base64(salt||nonce||ciphertext||tag).
- Derivation: Argon2id (memory=64MiB, iterations=3, parallelism=4) from user master password string input.
- Functions: `EncryptJSON(data []byte, password string) []byte`, `DecryptJSON(data []byte, password string) []byte`.
- No key persistence to disk; session key held in memory only.
- Tests: encrypt/decrypt roundtrip, wrong password fails, envelope format validation.

Task 3 — Cache size tracking + settings UI update (standard)
- Go: `CacheSize()` scans project `.ocode/storage-connectors.json` + `.ocode/cloud_cache/` (if exists) to compute bytes; returns int64.
- Web settings component (`internal/web/settings` or existing settings form): add `max_cache_bytes` input, display current size, destructive-clear button with confirm dialog.
- Clear action: Go endpoint `POST /api/storage/clear-cache` (or settings action) deletes `.ocode/cloud_cache/*`, updates settings, returns new size.
- Tests: size computation, clear deletes only cache (not settings), confirm dialog behavior.

Task 4 — Design doc updates + docs consistency (standard)
- Update `docs/cloud-connectors-design.md`: mark Phase 1 items (framework, Drive+S3 read-only, encryption, cache) as IN PROGRESS.
- Confirm `docs/index.md` bundle unchanged; verify `docs/cloud-connectors-encryption.md` is present (non-bundle).
- Add `.env.example` reference (`CONNECTOR_MASTER_PASSWORD_PROMPT=1`, no secrets).
- Add `TODO.md` updates (done in previous turn; verify present).

Verification: `go test ./internal/config/...`, `go vet`, `go build ./...`. Web: `vite build` clean for settings component change only.
Blocked dependency: 4 architecture answers (phased plan, keychain, separate root, optimistic concurrency, Monaco cache) from previous turn. Once user confirms, proceed with full Phase 1 build.
