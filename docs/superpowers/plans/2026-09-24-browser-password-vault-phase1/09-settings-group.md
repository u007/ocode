# Part 09 — Settings group wiring

## Files

- Modify: `web/src/components/Settings/SettingsPanel.tsx`
- Test: `web/src/components/Settings/SettingsPanel.vault.test.tsx`

## Interfaces

- **Consumes:** `VaultForm` (Part 08).
- **Produces:** settings group id `"vault"`, label `"Passwords"`.

## Test cases to write

- `renders the Passwords group` — `SettingsPanel` renders a nav button labelled
  `Passwords`.
- `selecting Passwords renders VaultForm` — clicking the `Passwords` nav button
  renders the (mocked) `VaultForm`.

## Implementation notes

- Add `"vault"` to the `SettingsGroupId` union.
- Import `VaultForm` and add `{ id: "vault", label: "Passwords" }` to
  `OCODE_GROUPS` (place it after `browser`, since it is browser-adjacent).
- Add `case "vault": return <VaultForm />;` to `renderGroup`.
- Follow the existing group wiring exactly — no new pattern.

## Steps

- [ ] Write `SettingsPanel.vault.test.tsx` with the cases above.
- [ ] Run `cd web && npx vitest run src/components/Settings/SettingsPanel.vault.test.tsx` — expect FAIL.
- [ ] Add the union member, import, group entry, and switch case.
- [ ] Run `cd web && npx vitest run src/components/Settings/SettingsPanel.vault.test.tsx && npx tsgo --noEmit` — expect PASS + clean.
- [ ] Commit.

## Verify

```bash
cd /Users/james/www/ocode/web
npx vitest run src/components/Settings/SettingsPanel.vault.test.tsx
npx tsgo --noEmit
```

## Commit

```bash
git add web/src/components/Settings/SettingsPanel.tsx web/src/components/Settings/SettingsPanel.vault.test.tsx
git commit -m "feat(web): add Passwords settings group"
```
