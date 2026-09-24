---
type: Plan
timestamp: 2026-09-24T07:51:50Z
---
# Part 10 — Docs, TODO, and full gates

## Files

- Modify: `CHANGES.md`
- Modify: `skills/ocode-web/SKILL.md`
- Modify: `TODO.md`
- Update via `doc_write` (context agent): `docs/index.md`, `docs/log.md`
- Confirm: `.env.example` (expected unchanged)

## Interfaces

- None.

## Work items

- [ ] **CHANGES.md** — add a dated entry: new `internal/vault` (AES-256-GCM,
  Argon2id-wrapped random data key, item id as AAD, OS file lock + atomic
  writes, file `<GlobalDataDir>/browse/vault.json` mode `0600`); new
  `/api/vault/*` endpoints with per-surface grants; new Settings → Passwords
  (`VaultForm`); note that browser autofill lands in Phases 2–3.
- [ ] **`skills/ocode-web/SKILL.md`** — add `VaultForm.tsx` to the Settings
  component list in the file map, and add a numbered item: the vault is
  server-side (Go); the SPA calls `/api/vault/*` with a `surface` id;
  `VaultForm` uses surface `"settings"`; secrets must never reach logs.
- [ ] **`TODO.md`** — add bullets for the deferred work:
  - `(password-vault) Phase 2: local iframe autofill — capture.js detection +
    in-field icon + fill/capture messages; useBrowserMessages handlers;
    BrowserPanel save banner + CredentialPicker; AddressBar key button.`
  - `(password-vault) Phase 3: Chrome/CDP autofill — Target.Evaluate;
    vaultFill/vaultCollect messages; Page.addScriptToEvaluateOnNewDocument
    observer + Runtime.addBinding; executionContextId frame check.`
- [ ] **`docs/index.md` + `docs/log.md`** — update through the **context agent's
  `doc_write`** (these files are auto-managed; never hand-edit). Add/refresh the
  entry for the Phase 1 plan and confirm the spec entry is present.
- [ ] **`.env.example`** — confirm Phase 1 introduces **no new env var or
  config key** (the vault path is derived from `paths.GlobalDataDir()`). If any
  were introduced, add them here; otherwise state explicitly in the final
  report that none were added.

## Full gates

- [ ] Run the complete gate set:

```bash
cd /Users/james/www/ocode
go build ./... && go vet ./internal/vault/ ./internal/server/
gofmt -l internal/vault internal/server/handler_vault.go internal/server/handler.go internal/server/server.go
go test ./internal/vault/ ./internal/server/ -count=1
cd web && npm run typecheck && npm run build && npm test
```

- [ ] If `internal/server` has pre-existing failures from concurrent WIP,
  compare the FAIL set against a pristine baseline before attributing them to
  this work.
- [ ] Commit.

## Verify

The gate block above must be clean (gofmt prints nothing; Go tests pass; web
typecheck + build clean; web tests pass).

## Commit

```bash
git add CHANGES.md skills/ocode-web/SKILL.md TODO.md
git commit -m "docs: document browser password vault phase 1 and defer phases 2-3"
```
