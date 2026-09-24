---
type: Plan
timestamp: 2026-09-24T07:51:31Z
---
# Part 08 — VaultForm settings UI

## Files

- Create: `web/src/components/Settings/VaultForm.tsx`
- Test: `web/src/components/Settings/VaultForm.test.tsx`

## Interfaces

- **Consumes:** `api.vault*` (Part 07); `Button`, `Input` from `../ui/`;
  `Dialog` primitives from `../ui/dialog` (Radix); icons from `lucide-react`.
- **Produces:** `export default function VaultForm()`.
- Uses the fixed surface id `"settings"` for every vault call.

## States

- **loading** — status probe in flight.
- **create** — no vault: master + confirm fields; Create button disabled until
  both are non-empty and equal.
- **locked** — vault exists but this surface is not unlocked: master field +
  Unlock.
- **unlocked** — item table (site, username, reveal/copy, edit, delete) plus
  Add and Lock. Add/Edit opens a **Radix `Dialog`** containing the credential
  fields (site, url, title, username, password, notes) and a Generate button.

## Test cases to write

- `creates a vault when none exists` — status `{exists:false}`; entering
  matching master/confirm and clicking Create calls `api.vaultInit("m","settings")`.
- `shows an unlock prompt when locked` — status `{exists:true,unlocked:false}`
  renders the master field and Unlock button.
- `lists items when unlocked` — status unlocked + a listed item renders its
  site and username.
- `add opens a dialog and saves a credential` — clicking Add opens the dialog;
  filling fields and Save calls `api.vaultCreate` with the fields and `"settings"`.
- `reveal shows the password` — clicking Reveal calls `api.vaultReveal` and
  renders the returned password.
- `delete removes the item` — clicking Delete calls `api.vaultDelete`.
- `generate fills the password field` — clicking Generate calls
  `api.vaultGenerate` and the returned password appears in the password field.
- `lock relocks the surface` — clicking Lock calls `api.vaultLock("settings")`
  and returns to the locked state.

## Implementation notes

- **Use the existing Radix/Shadcn `ui/dialog` primitives** for the add/edit
  form — do not hand-roll a modal. Annotate the primary Save button with
  `data-dialog-default-action` per the repo's dialog focus policy.
- Every `catch` **logs the operation** (`console.error("vault: <op>", e)`) and
  sets a visible error string; no empty catch.
- Never render or persist the master password anywhere except the two input
  fields; clear them after init/unlock.
- Reveal is per-row and transient (not stored in a global); Copy uses
  `navigator.clipboard?.writeText` guarded for jsdom.
- The vault list request passes an explicit `sort: "site"` (not omitted).

## Steps

- [ ] Write `VaultForm.test.tsx` with the cases above.
- [ ] Run `cd web && npx vitest run src/components/Settings/VaultForm.test.tsx` — expect FAIL (cannot resolve `./VaultForm`).
- [ ] Write `VaultForm.tsx`.
- [ ] Run `cd web && npx vitest run src/components/Settings/VaultForm.test.tsx && npx tsgo --noEmit` — expect PASS + clean.
- [ ] Commit.

## Verify

```bash
cd /Users/james/www/ocode/web
npx vitest run src/components/Settings/VaultForm.test.tsx
npx tsgo --noEmit
```

## Commit

```bash
git add web/src/components/Settings/VaultForm.tsx web/src/components/Settings/VaultForm.test.tsx
git commit -m "feat(web): add Passwords settings form"
```
