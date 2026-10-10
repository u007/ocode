<!-- Keep this short. A reviewer should be able to approve in under a minute. -->

## What and why

<!-- One or two sentences. Link the issue if there is one: Fixes #123 -->

## How

<!-- The approach, and anything non-obvious a reviewer would otherwise have to reverse-engineer. -->

## Verification

<!-- How you know it works. Name the exact commands and their results. -->

- [ ] `go build ./...` and `go vet ./...` pass
- [ ] `go test ./...` passes (or the failures are pre-existing and shown as such)
- [ ] `cd web && pnpm run typecheck && pnpm run test` passes, if this touches `web/`

<!-- Required for behaviour changes, not for docs or comments: -->
- [ ] Regression test added that fails before the fix and passes after
- [ ] Mutation-verified, if the change touches a security-sensitive branch

## Risk and rollback

<!-- What could this break? How would a user notice? What is the revert path? -->

## Notes for the reviewer

<!-- Anything you want reviewed more closely, or a place you were unsure. -->

<!--
Notes for this repository:
- `go build ./...` works in a fresh clone with no extra setup; HTR is behind the
  `htr` build tag and is not needed for source builds.
- `make install` / `make desktop-app` add `-tags htr` and require
  `make prepare-htr-assets` with the out-of-tree HTR sources.
-->
