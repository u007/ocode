# Part 1: Core Redaction Engine (`internal/redact`)

Spec: `docs/superpowers/specs/2026-06-12-secret-redaction-design.md` §3, §4, §5, §2-tier-1. New standalone package, no dependencies on agent/tui — pure logic, fully unit-testable.

### Task 1.1: Token format

**Files:**
- Create: `internal/redact/token.go`
- Test: `internal/redact/token_test.go`

- [ ] Write failing tests: `FormatToken(nonce, idx)` produces `[[OCSEC:a3f9c2:1]]`; `TokenPattern` regex matches it, rejects wrong-length nonce/non-hex/other-session nonce when filtered via `TokensForNonce(text, nonce)`; `NewNonce()` returns 6 lowercase hex chars from `crypto/rand`.
- [ ] Implement `FormatToken`, compiled package-level `TokenPattern` (`\[\[OCSEC:[0-9a-f]{6}:\d+\]\]`), `TokensForNonce`, `NewNonce`.
- [ ] `go test ./internal/redact/ -run Token` → PASS. Commit `feat(redact): OCSEC token format`.

### Task 1.2: Registry

**Files:**
- Create: `internal/redact/registry.go`
- Test: `internal/redact/registry_test.go`

- [ ] Write failing tests for `Registry` (constructor takes nonce): `GetOrAssign(value, kind, source)` returns same index for same value (accumulative reuse); indexes monotonically increase, never reassigned; values trimmed of surrounding whitespace + symmetric quotes before registration (`"hunter2"` == `hunter2`); concurrent `GetOrAssign` from N goroutines yields no duplicate indexes (race test, run with `-race`); `Lookup(index)` returns value+meta; `All()` returns entries sorted by index ascending.
- [ ] Write failing tests for `Substitute(text)`: replaces all registered values longest-first over non-overlapping spans (`hunter2` inside `hunter2-prod` → only the B token, no partial leak); and `Resolve(text)` (token → value, ignoring foreign-nonce tokens).
- [ ] Implement: mutex-guarded map pair (`value→index`, `index→entry{Value, Kind, Source, FirstSeenAt}`), `Substitute` building a longest-value-first replacer, `Resolve` via `TokenPattern`.
- [ ] `go test -race ./internal/redact/` → PASS. Commit `feat(redact): session secret registry`.

### Task 1.3: Tier-1 detectors

**Files:**
- Create: `internal/redact/detect.go`
- Test: `internal/redact/detect_test.go`

- [ ] Write failing table-test corpus, two modes via `DetectOpts{FileContent bool}`:
  - Known formats (both modes): AWS `AKIA…`, GitHub `ghp_`/`gho_`/`github_pat_`, Slack `xox…`, Stripe `sk_live_`, JWT `eyJ….eyJ….sig`, OpenAI/Anthropic `sk-…`, PEM private-key blocks, `scheme://user:pass@host` URL credentials.
  - Keyword+entropy (chat mode only): high-entropy string adjacent to `password|passwd|secret|token|api_key|Authorization:|Bearer`.
  - Custom words (both modes): exact user-defined strings.
  - False-positive guards: 40-hex git SHA, lockfile `sha512-…` integrity hash, base64 image chunk — must NOT match in file mode; SHA/integrity must not match in chat mode either.
- [ ] Implement `Detect(text string, customWords []string, opts DetectOpts) []Span` where `Span{Start, End, Kind}`. Compile all regexes once at package init. Entropy helper: Shannon entropy threshold on candidate token.
- [ ] `go test ./internal/redact/ -run Detect` → PASS. Commit `feat(redact): tier-1 detectors with file/chat modes`.

### Task 1.4: Vault

**Files:**
- Create: `internal/redact/vault.go`
- Test: `internal/redact/vault_test.go`

- [ ] Write failing tests (use `t.TempDir()` as base, inject base dir — do not hardcode home): `VaultPath(base, slug, sessionID)` = `<base>/project/<slug>/secrets/<ses_id>.vault.json`; `SaveVault` creates dir `0700`, file `0600`, JSON `{nonce, secrets{...}}`; write is atomic (temp + fsync + rename — assert no partial file after simulated mid-write failure via injected rename hook or by checking temp cleanup); `LoadVault` round-trips registry contents incl. nonce; `DeleteVault` removes file.
- [ ] Implement with `os.CreateTemp` in target dir + `f.Sync()` + `os.Rename`. Production base dir resolves to the same home base used by `session.GetStorageDir()` (`internal/session/session.go:58-81`) — **always home, never project-local `.ocode/`**; add `DefaultVaultBase()` helper mirroring that logic (Windows branch included).
- [ ] `go test ./internal/redact/ -run Vault` → PASS. Commit `feat(redact): per-session vault with atomic 0600 writes`.

### Task 1.5: Redactor facade

**Files:**
- Create: `internal/redact/redactor.go`
- Test: `internal/redact/redactor_test.go`

- [ ] Write failing tests for `Redactor` (holds Registry + vault path + config snapshot): `RedactChat(text)` runs tier-1 chat-mode detect → registry assign → substitute, **persists vault before returning** (ordering invariant: a returned placeholder is always resolvable after crash — test by asserting vault file contains the entry immediately after call); `RedactFile(text)` same but file-mode detect; `Render(text)` resolves tokens to values; `Enabled()` reflects config; disabled Redactor returns input unchanged.
- [ ] Implement. Tier-2 hook is a `Scanner` interface field (`Scan(maskedText) ([]Span, error)`) left nil here — wired in Part 2.
- [ ] `go test -race ./internal/redact/` (whole package) → PASS. Commit `feat(redact): Redactor facade with vault-before-message ordering`.
