---
type: Design
title: Embedded Browser Password Vault — Design
description: 'Approved-for-planning design for a Bitwarden-like password vault in ocode''s embedded browser (full browser tab + sidebar). Server-side Go crypto: Argon2id-derived KEK wrapping a random AES-256-GCM data key; per-item blobs sealed whole with the item id as AAD; file at <GlobalDataDir>/browse/vault.json (0600). Per-surface unlock (UX-only, not a security boundary). Autofill in local iframe mode via capture.js and in Chrome/CDP mode via vaultFill/vaultCollect + Page.addScriptToEvaluateOnNewDocument observer + Runtime.addBinding. New Settings > Passwords page. Phased: vault core+API+settings, then local autofill, then Chrome autofill.'
tags:
  - browser
  - vault
  - passwords
  - crypto
  - autofill
  - design
  - superpowers
timestamp: 2026-09-24T04:14:52Z
---
# Embedded Browser Password Vault — Design

- **Date:** 2026-09-24
- **Status:** Draft — pending user review
- **Author:** ocode (build session)
- **Related:** `internal/browse/`, `web/src/components/Browser/`, `web/src/components/Settings/`

## 1. Problem

The embedded browser (full browser tab and the sidebar browser — both render
`BrowserPanel.tsx`) has no password manager. Users must re-type credentials
into every proxied login page, and there is no way to save them.

We want a built-in, Bitwarden-like vault:

1. Remember logins used in the browser surfaces and offer to save them.
2. Autofill saved logins, triggered by the user (toolbar button + in-field
   icon), never automatically on page load.
3. Manage (add/edit/delete/reveal) site-based credentials from the Settings UI.
4. Encrypt secrets at rest with AES using a user-chosen master password.

## 2. Decisions (locked with the user)

| Decision | Choice |
|---|---|
| Crypto location | **Server-side (Go)**. Go derives the key and does AES-GCM. The master password travels only over the local authenticated API and is never persisted. |
| Unlock model | **Per browser surface / tab** (and a separate `settings` surface). |
| Idle auto-lock | **None.** A surface stays unlocked until the user explicitly locks it or the surface is closed/revoked. |
| Autofill trigger | **Toolbar key button + in-field icon**, both requiring an explicit user click. No auto-fill on load. |
| Mode scope | **Local (iframe) mode AND Chrome (external) mode.** |
| Chrome-mode save | **Auto-detect submit too** — inject a persistent observer script via `Page.addScriptToEvaluateOnNewDocument`, mirroring local mode. |
| Vault scope | **Single global vault** per ocode server / data dir, shared across projects, browsers, and settings. |

## 3. Architecture

```
SPA (BrowserPanel / AddressBar / VaultForm / CredentialPicker)
  │  authenticated /api/vault/*  (global; local server)
  ▼
internal/server/handler_vault.go ──▶ internal/vault (crypto + store)
                                          │
                                          └─ <GlobalDataDir>/browse/vault.json (0600, AES-GCM)

Local mode:  SPA ──postMessage──▶ capture.js in iframe (fill / collect / detect)
Chrome mode: SPA ──CDP ws──▶ cdpsocket.go ──▶ Target.Evaluate / injected observer
```

The browse page runs on a **separate origin** (random `127.0.0.1:<port>`) from
the SPA, so local-mode autofill is cross-origin and must use `postMessage`
(already the transport for telemetry and scroll restore). Chrome mode has no
DOM in the SPA and must use CDP.

## 4. `internal/vault` package (new)

### 4.1 File format

Path: `<GlobalDataDir>/browse/vault.json`, mode `0600`, written atomically
(tmp file + `os.Rename`, matching `internal/config`).

```jsonc
{
  "version": 1,
  "kdf": { "algo": "argon2id", "salt": "<b64 16B>",
           "time": 3, "memory_kib": 65536, "threads": 4 },
  "wrapped_key": "<b64 AES-GCM(DK) under KEK, AAD=\"ocode-vault-key\">",
  "items": [
    {
      "id": "<uuid>",
      "blob": "<b64 AES-GCM(item JSON) under DK, AAD=id>"
    }
  ]
}
```

- **Key hierarchy (wrapped data key — no separate verifier):**
  - On `Init` generate a random 32-byte **data key (DK)**.
  - Derive a **KEK** from the master password + Argon2id salt; wrap DK with
    AES-256-GCM (fresh 12-byte nonce; AAD `"ocode-vault-key"`) → `wrapped_key`.
  - `Unlock` re-derives the KEK and unwraps DK. A wrong master password fails
    the GCM tag check ⇒ `ErrWrongMaster`. There is no verifier blob.
  - `ChangeMaster` only re-derives a KEK from the new password/salt and
    re-wraps the **same** DK — item blobs are never re-encrypted (atomic,
    32-byte rewrite).
- **Item sealing (authenticated as a whole):** the entire item
  (`site, url, title, username, password, notes, created, updated`) is
  JSON-encoded and sealed as **one** AES-256-GCM blob under DK with the item
  `id` as **AAD**. Binding the blob to `id` prevents an attacker with write
  access from moving a credential blob onto a different item, and sealing the
  item as a whole prevents swapping individual fields.
- **KDF:** Argon2id (`golang.org/x/crypto/argon2`, promote the indirect dep to
  direct). Defaults: time=3, memory=64 MiB, threads=4, keyLen=32. Salt 16
  random bytes, regenerated on `ChangeMaster`.
- **Cipher:** AES-256-GCM, `nonce || ciphertext || tag`, base64.
- **Zeroing is best-effort:** Go `string`/`[]byte` semantics mean the master
  password and decrypted values cannot be guaranteed wiped; `Lock` zeroes the
  DK byte slice and drops references, and the spec makes no stronger claim.
- While locked the file exposes only item `id`s (opaque UUIDs) — no site,
  username, or title.

### 4.2 API

```go
type Vault struct{ /* mu sync.Mutex; dk []byte; items []Item; path string */ }

func New(path string) *Vault
func (v *Vault) Exists() bool
func (v *Vault) Init(master string) error          // generate DK, wrap, create file, unlock
func (v *Vault) Unlock(master string) error        // unwrap DK; ErrWrongMaster on tag mismatch
func (v *Vault) Lock()                             // zero DK, drop decrypted buffers
func (v *Vault) Unlocked() bool
func (v *Vault) List(sort string, limit, offset int) []ItemMeta // no secrets; sorted + paginated
func (v *Vault) Reveal(id string) (Item, error)    // ErrLocked when locked
func (v *Vault) Create(Item) (Item, error)
func (v *Vault) Update(id string, Item) (Item, error)
func (v *Vault) Delete(id string) error
func (v *Vault) MatchForURL(rawURL string) []ItemMeta
func (v *Vault) ChangeMaster(old, new string) error // re-wraps DK only; blobs untouched
func GeneratePassword(opts GenOptions) string
```

- `Lock` zeroes the **data key** (DK) and clears decrypted buffers; the
  password-derived KEK is never retained.
- `List` returns metadata only, **sorted by `site` then `username`** (stable,
  case-insensitive) and paginated via `limit`/`offset`.
- `MatchForURL` compares registrable host (eTLD+1 is out of scope; use exact
  host, then parent-domain suffix match), preferring path-prefix matches.
- **Concurrency:** an in-process mutex plus an OS **file lock** on the vault
  path; every mutating op re-reads the file under the lock before writing, so
  two ocode processes cannot clobber each other (same hazard as the
  concurrent-config-write issue).
- `ErrLocked`, `ErrWrongMaster`, `ErrNoVault`, `ErrExists` sentinel errors.

## 5. Server endpoints

New `internal/server/handler_vault.go`, routes registered in
`registerRoutes()` under the auth middleware. All are **global** (no `host`
parameter) — the vault belongs to the local server the SPA talks to.

| Method & path | Body / result |
|---|---|
| `GET /api/vault/status?surface=` | `{exists, unlocked}` — `unlocked` is the per-surface grant |
| `POST /api/vault/init` | `{master, surface}` → creates + unlocks; 409 if exists |
| `POST /api/vault/unlock` | `{master, surface}` → 401 `ErrWrongMaster`; grants surface |
| `POST /api/vault/lock` | `{surface}` → drops that grant; **omitted/empty ⇒ locks all surfaces** |
| `GET /api/vault/items?sort=&limit=&offset=` | metadata list (default sort `site`, limit 200 max 1000); 403 when surface not unlocked |
| `GET /api/vault/items/{id}/reveal` | full item; 403 when surface not unlocked |
| `POST /api/vault/items` | **create**; 403 when surface not unlocked |
| `PUT /api/vault/items/{id}` | **update**; 403 when surface not unlocked |
| `DELETE /api/vault/items/{id}` | delete; 403 when surface not unlocked |
| `POST /api/vault/match` | `{url, surface}` → matching metadata; 403 when surface not unlocked |
| `POST /api/vault/change-master` | `{old, new}` → requires **any** live surface grant (normally `settings`); 403 if none |
| `POST /api/vault/generate` | `{length, upper, digits, symbols}` → password (no grant needed) |

### 5.1 Per-surface unlock grants

The handler owns `map[surface]grant` where `surface` is a browser `stateKey`
or the literal `"settings"`. There is **no idle timeout** (per decision);
grants are dropped by:

- explicit `POST /api/vault/lock` for that surface,
- browser-surface close: `handleBrowseRevoke` (the existing
  `POST /api/browse/revoke` handler the SPA calls when a browser tab is
  closed) additionally calls `LockSurface(stateKey)`,
- process exit (the key is memory-only anyway).

Unlock attempts are serialized and lightly throttled (a fixed minimum
interval between failed attempts) on top of Argon2id's own cost, so a stuck
client cannot hammer the KDF. No attempt counters or lockout state are
persisted.

The vault's data key is held while **at least one** grant is live. The
handler reads the requesting `surface` from the JSON body / query. Missing or
unknown surface ⇒ 403 for secret-bearing calls.

**`surface` is client-supplied and is a UX affordance, not a security
boundary** — any authenticated SPA caller could claim any surface id. The real
boundary is the API auth token (untrusted proxied pages are cross-origin and
hold no token). The per-surface model exists so each browser tab prompts once
and closes locked; it does not defend against a compromised SPA.

## 6. Autofill — local (iframe) mode

Extend `internal/browse/capture.js` (injected first in `<head>` of every
local-mode document):

- **Detection:** a password field (`input[type=password]`, visible) plus the
  nearest username field (same `<form>`: first `text`/`email`/`tel` input, or
  the closest preceding text input). Heuristic, no framework coupling.
- **In-field icon:** an absolutely-positioned key button anchored to the
  focused password field. Clicking posts
  `{type:"ocode:browse:request-fill", stateKey}` to the SPA — the icon itself
  never receives secrets.
- **Fill:** on `{type:"ocode:browse:fill-credentials", username, password}`
  from the SPA origin, set both fields via the native value setter and dispatch
  bubbling `input` + `change` events (so React/Vue register the change). The
  SPA posts with `targetOrigin = browseOrigin`, and capture.js fills **only in
  the top frame** (`window.top === window`). The in-field icon is a *hint*
  only: the authoritative fill is always initiated from SPA chrome (toolbar
  button / picker), so a page cannot silently trigger a fill.
- **Capture:** on `submit` of a detected login form (capture phase), post
  `{type:"ocode:browse:credentials-captured", username, password}` to the SPA.
  Capture is **only** on submit (and on the `pagehide`/`beforeunload` that
  follows a successful submit) — never on a bare field `change`, which would
  prompt to save half-typed passwords. The payload intentionally omits any
  page-reported URL/site: the SPA matches and saves against the host the
  **proxy actually fetched** for this surface, never a page-supplied value.

`useBrowserMessages.ts` gains handlers for `request-fill` and
`credentials-captured`, gated by the existing origin + `stateKey` checks.
`BrowserPanel.tsx` then:

- `credentials-captured` → shows a **Save login?** banner (proxy host,
  username, masked password) → `api.vaultCreate`.
- `request-fill` → opens `CredentialPicker` filtered by `api.vaultMatch(url)`
  → on pick, `postMessage(fill-credentials)` to the iframe. If the vault is
  locked, the picker first shows the unlock prompt.

The toolbar **key button** in `AddressBar.tsx` triggers the same picker (and,
in local mode, the same fill message; in Chrome mode, the CDP fill below).

## 7. Autofill — Chrome (external) mode

Chrome renders through a CDP screencast (canvas), so the SPA has no DOM.

- Add `Target.Evaluate(ctx, expr string) (json.RawMessage, error)` wrapping
  `conn.Call(ctx, sessionID, "Runtime.evaluate", {expression, returnByValue:true, awaitPromise:true})`.
- Add two **dedicated** client messages to `cdpsocket.go` — `{t:"vaultFill",
  username, password}` and `{t:"vaultCollect"}` — handled server-side by
  building a fixed JS IIFE template with the values JSON-encoded. We do **not**
  expose a generic `Runtime.evaluate` passthrough from the SPA: the wire
  surface stays narrow and the SPA cannot execute arbitrary page JS.
- **Observer injection:** at target attach the server calls
  `Page.addScriptToEvaluateOnNewDocument` with a compact observer script
  (same detection/icon logic as `capture.js`). This mirrors the existing
  select-picker injection at `internal/browse/cdp/manager.go:1121`.
- **Capture channel:** the observer calls a bound function registered with
  `Runtime.addBinding("__ocodeVaultCapture")`; the server subscribes to
  `Runtime.bindingCalled` (via `conn.Subscribe`, the established event path)
  and forwards captures to the SPA as a `vaultCaptured` message. The existing
  console handler is not reused (keeps the DevConsole clean). A second binding,
  `__ocodeVaultRequestFill`, carries the in-field icon click (the observer
  cannot reach the SPA directly) so the SPA can open the picker and reply with
  `vaultFill`. **Bindings are page-callable and therefore forgeable:** the
  server must resolve the frame/URL from the event's `executionContextId`
  (via `Runtime.executionContextCreated` / `Page.frameNavigated`), never trust
  a URL in the payload, and match/save against the proxy-known host. Bindings
  are registered on **every auto-attached session** (`manager.go` auto-attach),
  and `Runtime.enable` must be active on that session for `bindingCalled` to
  arrive.
- **Current document:** `Page.addScriptToEvaluateOnNewDocument` only affects
  *future* navigations; at attach the server also performs one explicit
  `Runtime.evaluate` to install the observer into the already-loaded document.
- **Fill:** the SPA sends `vaultFill`; the server evaluates the fill IIFE.
- **Save:** the `vaultCaptured` event reaches the SPA, which shows the same
  **Save login?** banner as local mode.

## 8. Settings UI — `VaultForm.tsx`

New settings group `vault` labelled **"Passwords"** in `SettingsPanel.tsx`
(`OCODE_GROUPS` + `renderGroup` switch case). States:

- **No vault:** master password + confirm → `api.vaultInit`.
- **Locked:** master password prompt → `api.vaultUnlock("settings")` + a
  "Forget / Lock" button.
- **Unlocked:** item table (site, username, URL) with add/edit/delete,
  reveal/copy password, password generator (length/upper/digits/symbols), and
  change-master. `CredentialPicker` is reused by the browser surfaces.

## 9. Security properties & threat model

- **At rest:** AES-256-GCM; Argon2id (per-vault random salt) wrapping a random
  data key; one random nonce per item blob; file mode `0600`; atomic writes
  under an OS file lock.
- **Integrity:** each item blob is sealed with its `id` as AAD, so a
  write-access attacker cannot move a credential to another item; the item is
  sealed whole, so fields cannot be swapped.
- **Master password:** never written to disk, never logged, never sent into a
  page/iframe; only over the authenticated local API (or the authenticated
  remote proxy). Zeroing is best-effort (Go strings).
- **Data key:** memory only; zeroed on `Lock`; dropped when the last surface
  grant is released.
- **Untrusted page:** can observe its own filled input values (unavoidable —
  identical to typing) but never receives the master password, and receives
  credentials only after an explicit user click (no auto-fill on load).
- **No local-mode origin isolation:** every proxied site shares one browse
  origin, so page JS in that surface can read filled inputs and can forge
  `request-fill` / `credentials-captured` messages (including a fake URL). The
  design therefore (a) matches/saves against the proxy-known host, never the
  page-reported URL, (b) fills only the top frame, (c) posts with
  `targetOrigin = browseOrigin`, and (d) treats the in-field icon as a hint
  whose fill still requires an SPA-chrome click.
- **Cross-origin:** all `postMessage` traffic keeps the existing hard origin +
  `stateKey` gates.
- **Wrong password:** GCM tag failure ⇒ generic 401; no oracle beyond
  success/failure.
- **Per-surface grants are UX, not a boundary** (§5.1).
- **No leak to telemetry/logs:** vault request bodies are excluded from the
  debuglog, `/api/logs`, and access logs; captured passwords are never placed
  in browse console/network/POST-body telemetry (the agent can read that);
  no agent tool can reach `/api/vault/*`; reuse the redaction patterns in
  `internal/tui/redaction.go` where values could surface in logs.

### Known limitations (accepted)

- **Subdomain over-match:** `MatchForURL` uses exact-host then parent-domain
  suffix matching (no public-suffix list), so a credential for `example.com`
  is offered on `anything.example.com`. This matches normal browser password
  managers; a full eTLD+1 resolver is out of scope.
- **Chrome mode is local-only:** remote-workspace browsing has no CDP target
  (every host routes through the reverse proxy as local mode), so Chrome-mode
  autofill applies only when browsing locally with managed Chrome.

### Non-goals (v1)

OS keychain / biometric unlock, cross-machine sync (the vault is local to the
server the SPA talks to), breach checking, TOTP/2FA generation, browser
extension import/export, per-project vaults.

## 10. Testing

- **Go unit (`internal/vault`)**: init/unlock round-trip; wrong master
  (unwrap tag failure); tamper detection (flip a blob byte); **AAD binding**
  (moving an item's blob to another `id` fails to open); lock zeroes the DK
  and blocks `Reveal`; `ChangeMaster` **re-wraps the DK only** — item blobs
  are byte-identical before/after and the old password no longer unwraps;
  `List` sort/pagination; `MatchForURL` host/path precedence; file mode 0600;
  atomic write; **file lock** (two `Vault` instances serialize).
- **Go handler**: status/init/unlock/lock/items/reveal/create/update/delete/
  match/generate/change-master; 403 when surface not unlocked (items, reveal,
  create, update, delete, match); 401 wrong master; 409 double-init;
  `lock` with no surface clears all grants; `LockSurface` on
  `handleBrowseRevoke`; change-master needs a live grant.
- **Go CDP**: the `vaultFill`/`vaultCollect` templates via the stub chrome,
  including **JSON-escaping of hostile values** (quotes, backslashes,
  `</script>`, newlines) so the built expression cannot break out; binding
  events resolve the frame from `executionContextId`.
- **Web**: `capture.js` fill/collect/detect via the existing jsdom
  `(0,eval)(bundle)` harness (`browseCapture.smoke.test.ts`); capture fires
  only on submit, never on a bare field change; `VaultForm`
  (create/unlock/CRUD/change-master); `CredentialPicker`; `BrowserPanel`
  save-prompt + fill flow; `AddressBar` key button.
- **Gates**: `go build ./...`, `go vet`, `gofmt`, `go test` for touched
  packages; `cd web && npm run typecheck && npm run build && npm test`.

## 11. Phasing

Three independently shippable phases (each with its own tests + gates):

1. **Vault core + API + Settings UI** — `internal/vault`, `handler_vault.go`,
   routes, `VaultForm`, `CredentialPicker`. No browser integration; usable as
   a password list immediately.
2. **Local autofill + save** — `capture.js` detection/fill/capture,
   `useBrowserMessages` handlers, `BrowserPanel` save banner + picker,
   `AddressBar` key button (local mode).
3. **Chrome-mode autofill + save** — `Target.Evaluate`, `vaultFill`/
   `vaultCollect`, observer injection + bindings, toolbar/menu save.

## 12. Rollout

Desktop/headless app must be rebuilt to ship the new `capture.js` and
`web/dist`. Existing users see no vault until they create one; the feature is
inert otherwise.
