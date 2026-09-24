# Browser Password Vault — Phase 1 Implementation Plan

> **For agentic workers:** implement part-by-part, ticking the `- [ ]` steps.
> This plan is high-level: it names files and functions, not code. Spec:
> `docs/superpowers/specs/2026-09-24-browser-password-vault-design.md`.

**Goal:** Ship a server-side, AES-256-GCM-encrypted password vault with an
authenticated HTTP API and a Settings → Passwords management UI. No browser
autofill yet.

**Architecture:** A new `internal/vault` package owns the encrypted file at
`<GlobalDataDir>/browse/vault.json`. A random 32-byte data key (DK) is wrapped
by an Argon2id-derived KEK; each item is sealed whole with its `id` as
AES-GCM AAD. `internal/server/handler_vault.go` exposes CRUD over
`/api/vault/*`, gated by per-surface unlock grants. The SPA's `VaultForm`
drives it from a new "Passwords" settings group.

**Tech Stack:** Go (`golang.org/x/crypto/argon2` promoted to direct;
`golang.org/x/sys` for the file lock — both already in `go.mod`);
`internal/server`; React 18 + TypeScript (`web/src`); Vitest.

**Spec:** `docs/superpowers/specs/2026-09-24-browser-password-vault-design.md`.

## Phase scope

This plan is **Phase 1 only**. Phase 2 (local iframe autofill) and Phase 3
(Chrome/CDP autofill) are deferred to their own plans.

## Global Constraints

- **No code in this plan.** Names of files/functions and test cases only.
- **TDD + mutation-verify.** Each part starts with a failing test and ends
  passing; break the behaviour, confirm the test fails, restore.
- **`use-modern-go` before editing Go.** Run
  `sh "skills/use-modern-go/scripts/run-tool.sh" list --file-path <file.go>`
  and apply the guidelines. Then `gofmt` + `go vet` on touched packages.
- **Crypto invariants (verbatim from the spec):** AES-256-GCM; Argon2id
  `time=3, memory_kib=65536, threads=4, keyLen=32`, 16-byte salt; DK is 32
  random bytes wrapped under the KEK with AAD `"ocode-vault-key"`; each item
  is sealed **whole** with the item `id` as AAD; vault file mode `0600`;
  atomic write (tmp + rename) under an OS file lock.
- **No fallbacks / no silent coercion.** Parse and validate explicitly; reject
  malformed input with a 4xx. Never silently substitute a default for a value
  the caller supplied; never drop a supplied `sort`/`limit`/`offset`.
- **Log every caught error** with the operation attempted (unlock, decrypt,
  persist, file lock). No empty `catch`/`err` discards.
- **Vault file writes are load-modify-write under the OS lock** — never a save
  of a stale in-memory snapshot (same class as the concurrent-config-write bug).
- **No secrets in logs.** Vault request bodies never reach the debuglog,
  `/api/logs`, or access logs; never log a master password, DK, or decrypted
  field.
- **`surface` is client-supplied UX state, not a security boundary.** The auth
  token is the boundary.
- **UI primitives:** use the existing Radix/Shadcn `ui/` components for any
  dialog/popover; do not hand-roll modals.
- **New env vars / config keys** must be added to `.env.example`. Phase 1 is
  expected to add **none** (the vault path is derived); confirm and state this
  in Part 10.
- **Gates per part:** `go build ./...`, `go vet`, `gofmt -l` clean for touched
  packages; `cd web && npm run typecheck && npm run build && npm test` for web
  parts.

## Review Focus

The five failure modes most likely to bite a real user, each pinned to the
owning part's tests:

1. **Two ocode processes writing the vault at once** — a save in one must not
   erase the other's item. → Part 03 (`persistLocked` merge + file-lock test).
2. **A wrong master password** — clean failure, no partial open, no file
   mutation. → Part 03; Part 06 (401).
3. **A tampered/relocated vault file** — a blob moved onto another item must
   not open. → Part 03 (AAD binding).
4. **Malformed pagination/query input** — `limit`/`offset`/`sort` that are
   non-numeric, negative, out of range, or unknown must be 400, not coerced.
   → Part 06.
5. **A locked surface calling a secret endpoint** — 403; `lock` with no
   surface clears every grant. → Part 06.

## Parts

- [01 — Crypto primitives](01-crypto.md)
- [02 — File store + OS file lock](02-store-lock.md)
- [03 — Vault public API](03-vault-api.md)
- [04 — URL matching](04-url-match.md)
- [05 — Password generator](05-generator.md)
- [06 — Server handlers, routes, per-surface grants](06-handler-api.md)
- [07 — Web API client + types](07-web-api.md)
- [08 — VaultForm settings UI](08-vault-form.md)
- [09 — Settings group wiring](09-settings-group.md)
- [10 — Docs, TODO, and full gates](10-docs-gates.md)
