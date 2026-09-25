# Plan: Makefile Version Bump Targets

## Approved design

Implement `docs/superpowers/specs/2026-09-24-make-version-bump-targets-design.md`.

User decisions:
- Add `make up-patch` and `make up-minor`.
- Each target must chain `make install` and then `make desktop-app` after the bump.
- Execute the real patch flow once, changing `0.8.110` to `0.8.111`.

## Constraints and working-tree safety

- The worktree contains substantial unrelated WIP, including `CHANGES.md` and generated documentation bookkeeping. Preserve all of it.
- Do not reset, clean, stash, or overwrite unrelated edits.
- `internal/version/version.go` is the canonical version source.
- A structural changelog failure must be loud; missing fixtures may not skip tests.
- `up-minor` must reset the patch component to zero.

## User Expectation Checklist

- [x] `make up-patch` increments the patch component once.
- [x] `make up-minor` increments the minor component and resets patch to zero.
- [x] Both targets update the canonical Go version and first `[Unreleased]` Version Bump line.
- [x] Malformed versions and missing changelog structures fail non-zero with actionable messages and no partial replacement.
- [x] Each target runs install before desktop packaging and stops on the first failure.
- [x] Downstream builds observe the newly bumped version.
- [x] Regression tests run only in temporary fixture copies and do not mutate the real workspace.
- [x] `CONTRIBUTING.md` documents the targets and their expensive chained behavior.
- [x] Focused tests and `go test ./internal/version` pass.
- [ ] Real `make up-patch` installs the CLI, builds `bin/ocode.app`, and produces version `0.8.111` in the app bundle and remote-binary directory. (Blocked by unrelated `web/src/lib/chatVerbosity.ts` TypeScript errors at lines 124, 126–128.)

## Implementation Steps

1. Add `scripts/test-version-bump.sh` first, covering helper arithmetic/validation/preservation and Make target order/failure/version propagation.
2. Run the new test and confirm it fails because the helper and targets do not yet exist.
3. Add `scripts/bump-version.sh` with strict semver validation, preflight changelog validation, temporary output generation, and actionable failures.
4. Add `up-patch` and `up-minor` to the Makefile `.PHONY` list and invoke the helper before sequential recursive `install` and `desktop-app` makes.
5. Document the commands in `CONTRIBUTING.md` and add the runtime version-bump line in `CHANGES.md` only when the real target executes.
6. Run the focused script, packaging regression test, and `go test ./internal/version`.
7. Execute `make up-patch` once. Do not execute real `up-minor`.
8. Verify installed CLI version, app Info.plist version, embedded remote binary directory/version, and final worktree diff without touching unrelated changes.
