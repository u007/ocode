# Cross-platform encryption design for connector JSON + cache
Status: DESIGN (awaiting confirmation before implementation)
Author: ocode agent
Date: 2026-10-08

Threat model (user confirmed): both local multi-user + agent access + cloud-sync exposure.
Key source (user confirmed): user master password, derived via Argon2id.
Target (user confirmed): `.ocode/storage-connectors.json` + download cache files.

Recommendation (cross-platform, Go + desktop):
- Derivation: Argon2id (memory=64MiB, iterations=3, parallelism=4, salt=32 bytes random) from user master password; produces 32-byte AES-256 key. Salt stored unencrypted alongside ciphertext (required for decryption). Key NEVER written to disk; derived on-demand in memory and released after session or when OS keychain session expires.
- Encryption: AES-256-GCM (authenticated encryption) for both JSON settings and each cached file. Nonce/IV: 12 bytes random, stored with ciphertext (e.g., salt || nonce || auth tag || ciphertext format).
- Storage format for settings file: base64-encoded envelope: `ENCv1:argon2:salt_b64:nonce_b64:ciphertext_b64:tag_b64`. This allows easy detection (starts with ENCv1) and version migration.
- Cache files: same envelope but file-level; one envelope per cached cloud file. Filename includes original cloud path hash (not the content) so listing works without decryption.
- Cross-platform key management: master password is entered once per desktop session via native dialog (`internal/desktop` Wails/Go) or web Settings form. The derived session key is cached in OS keychain (darwin Keychain, linux libsecret, windows DPAPI) only for the active session, NOT persisted across restarts. On restart: user re-enters master password (or unlocks via OS biometric/keychain if available) to derive the same key and decrypt files.
- Security rules (per repo rules + sandbox docs):
  - Credentials (OAuth tokens, S3 access keys) NEVER go into the encrypted JSON; only connector metadata (id, type, root/bucket, project link) is there. Real secrets remain in OS keychain.
  - `.env.example` updated with `CONNECTOR_MASTER_PASSWORD_PROMPT=1` reference (no actual secret).
  - Sandbox: any new bash commands reading `.ocode/storage-connectors.json` are added to the sandbox ask list; decryption is a Go-side operation (not bash) so sandboxed bash can't directly decrypt without the master key.
  - The context agent (`context`) does NOT write bundle pages for this; only the main agent updates design docs (this file) and `docs/cloud-connectors-design.md`. No `docs/index.md` mutation needed until Phase 1 code lands.
- Implementation libraries (Go): `golang.org/x/crypto/argon2`, standard library `crypto/aes`, `crypto/cipher` (GCM). No external encryption SDK required.
- Performance: Argon2id derivation takes ~100-500ms once per session; AES-GCM encryption/decryption is fast. Cache file encryption is done at download time; settings file encrypted on save.

NOTE (2026-10-08): Argon2id parameters (time=3, mem=64MiB, threads=4, key=32 bytes) are pinned constants. Changing them breaks decryption of all existing envelopes. If parameters must change, bump envelope version to ENCv2.
