---
type: Plan
timestamp: 2026-09-24T07:50:49Z
---
# Part 05 — Password generator

## Files

- Create: `internal/vault/generate.go`
- Test: `internal/vault/generate_test.go`

## Interfaces

- **Produces:**
  - `GenOptions{Length int; Upper bool; Digits bool; Symbols bool}`.
  - `GeneratePassword(opts GenOptions) string`.

## Test cases to write

- `TestGeneratePasswordRespectsOptions` — length is honoured and the result
  contains at least one uppercase, one digit, and one symbol when each is enabled.
- `TestGeneratePasswordDefaultsLength` — `GenOptions{}` yields length 20.
- `TestGeneratePasswordLowerOnlyHasNoSymbols` — with all classes disabled,
  every rune is `a`–`z`.

## Implementation notes

- Character pools: lower (always), upper, digits, symbols.
- Length is clamped: `<8` ⇒ 20; `>256` ⇒ 256 (explicit, documented).
- Guarantee one character from each enabled class, fill the remainder from the
  combined pool, then Fisher–Yates shuffle so guaranteed chars are not at
  fixed positions.
- Randomness comes from `crypto/rand` via a `randIndex(n int) int` helper
  (`rand.Int`); a rand failure returns 0 rather than panicking, and is logged
  with the operation `generate password`.

## Steps

- [ ] Write `generate_test.go` with the cases above.
- [ ] Run `go test ./internal/vault/ -run TestGenerate -v` — expect FAIL (`undefined: GeneratePassword`).
- [ ] Write `generate.go`.
- [ ] Run `go test ./internal/vault/ -v` — expect PASS.
- [ ] Commit.

## Verify

```bash
cd /Users/james/www/ocode
go test ./internal/vault/ -run TestGenerate -count=1 -v
```

## Commit

```bash
git add internal/vault/generate.go internal/vault/generate_test.go
git commit -m "feat(vault): add password generator"
```
