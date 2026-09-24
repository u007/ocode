# Part 01 — Vault crypto primitives

## Files

- Create: `internal/vault/crypto.go`
- Test: `internal/vault/crypto_test.go`

## Interfaces

- **Produces:**
  - `deriveKEK(master string, salt []byte) []byte` — Argon2id stretch to 32 bytes.
  - `seal(key, aad, plaintext []byte) ([]byte, error)` — AES-256-GCM, returns `nonce || ciphertext+tag`.
  - `open(key, aad, blob []byte) ([]byte, error)` — inverse of `seal`.
  - `wrapKey(kek, dk []byte) (string, error)` — seal DK under KEK, base64.
  - `unwrapKey(kek []byte, wrapped string) ([]byte, error)` — inverse; wrong KEK ⇒ `ErrWrongMaster`.
  - Consts `kdfTime=3`, `kdfMemory=64*1024`, `kdfThreads=4`, `keyLen=32`, `saltLen=16`, `keyWrapAAD="ocode-vault-key"`.
  - Sentinel errors `ErrLocked`, `ErrWrongMaster`, `ErrNoVault`, `ErrExists`, `ErrNotFound`, `ErrCorrupt`.

## Test cases to write

- `TestSealOpenRoundTrip` — seal then open returns the original plaintext.
- `TestOpenWrongAADFails` — opening with a different AAD returns an error.
- `TestOpenTamperedFails` — flipping a ciphertext byte makes `open` fail.
- `TestWrapUnwrapKeyRoundTrip` — unwrap returns the exact DK bytes.
- `TestUnwrapWrongKEKFails` — a different KEK returns `ErrWrongMaster`.
- `TestDeriveKEKDeterministic` — same master+salt ⇒ same KEK; different master ⇒ different KEK.

## Implementation notes

- `deriveKEK` uses `argon2.IDKey` with the consts above.
- `seal`/`open` use `aes.NewCipher` + `cipher.NewGCM`; `seal` reads a fresh
  random 12-byte nonce from `crypto/rand`.
- `open` rejects a blob shorter than the GCM nonce size with `ErrCorrupt`.
- `unwrapKey` maps **any** base64/decode/tag failure to `ErrWrongMaster` (no
  oracle beyond success/failure) and rejects a DK whose length ≠ `keyLen`.
- Package doc comment states the master password is never stored.

## Steps

- [ ] Write `crypto_test.go` with the cases above.
- [ ] Run `go test ./internal/vault/ -run 'TestSeal|TestOpen|TestWrap|TestUnwrap|TestDerive' -v` — expect FAIL (`undefined: seal`).
- [ ] Write `crypto.go`.
- [ ] Run `go test ./internal/vault/ -v` — expect PASS.
- [ ] Run `go mod tidy`; confirm `golang.org/x/crypto` is now a direct requirement in `go.mod`.
- [ ] Commit.

## Verify

```bash
cd /Users/james/www/ocode
go test ./internal/vault/ -v
grep -n 'golang.org/x/crypto' go.mod   # must NOT say "// indirect"
```

## Commit

```bash
git add internal/vault/crypto.go internal/vault/crypto_test.go go.mod go.sum
git commit -m "feat(vault): add Argon2id/AES-GCM crypto primitives"
```
