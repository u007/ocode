# Part 04 — URL matching

## Files

- Create: `internal/vault/match.go`
- Test: `internal/vault/match_test.go`

## Interfaces

- **Consumes:** `Item`, `ItemMeta`, `ErrLocked` from Part 03.
- **Produces:** `(*Vault).MatchForURL(rawURL string) ([]ItemMeta, error)`.

## Test cases to write

- `TestMatchForURLHostAndPath` — an item whose URL path is a prefix of the
  requested path ranks first; a parent-domain host (`sub.example.com` vs
  `example.com`) is included.
- `TestMatchForURLNoMatch` — an unrelated host returns an empty slice, no error.
- `TestMatchForURLRequiresUnlocked` — a locked vault returns `ErrLocked`.

## Implementation notes

- Parse `rawURL` with `net/url`; an unparseable URL or empty host returns an
  empty slice (no error, logged at debug with the operation `match url`).
- Host match: exact (case-insensitive) or `strings.HasSuffix(wantHost, "."+itemHost)`.
  No public-suffix list (documented limitation in the spec).
- Score: `+2` when the item's path is a non-empty prefix of the requested
  path; `+1` for an exact host; sort by score desc then site asc (stable).
- `ErrLocked` when the vault is locked.

## Steps

- [ ] Write `match_test.go` with the cases above.
- [ ] Run `go test ./internal/vault/ -run TestMatch -v` — expect FAIL (`undefined: MatchForURL`).
- [ ] Write `match.go`.
- [ ] Run `go test ./internal/vault/ -v` — expect PASS.
- [ ] Commit.

## Verify

```bash
cd /Users/james/www/ocode
go test ./internal/vault/ -run TestMatch -count=1 -v
```

## Commit

```bash
git add internal/vault/match.go internal/vault/match_test.go
git commit -m "feat(vault): add URL-based credential matching"
```
