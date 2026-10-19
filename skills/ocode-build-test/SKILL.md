---
name: ocode-build-test
description: Build, test, and asset-generation workflow for ocode. Covers plain vs tagged builds, web-build/.gitkeep recovery, worktree embed assets, race-test gates, and common Makefile targets. Use when building, testing, setting up a worktree, or debugging build/test failures.
when_to_use: When the user asks to build, test, run make targets, set up a git worktree, regenerate embed assets, or debug build/test failures. Also triggered by: "make build", "make test", "go test", "build tags", "htr-assets", "models-snapshot", ".gitkeep", "worktree".
---

# ocode build & test

## Core rules
- Plain source build needs **no generated assets**: `go build ./...` works with only committed `.gitkeep` placeholders.
- Tagged builds (`-tags "htr models"`) need generated inputs and **fail loudly** if missing:
  - `internal/browse/cdp/htr-assets.zip` (from `make prepare-htr-assets`)
  - `internal/agent/models-snapshot.json` (from `make models-snapshot`)
- `make install`, `make desktop`, `make desktop-remote-binaries` pass `-tags "htr models"` and depend on both generators.
- `web/dist/.gitkeep` is **tracked and must stay**. Vite's `emptyOutDir` deletes it during `pnpm run build`/`vite build`; `make web-build` recreates it. A ` D web/dist/.gitkeep` in `git status` is never intentional — restore with `git checkout -- web/dist/.gitkeep`, never stage its deletion.

## Common commands
- `make build` — web-build + `go build` (no tags).
- `make test` — `go test ./...` (no tags).
- `make web-build` — install deps, build web, recreate `.gitkeep`.
- `make install` — tagged build + install.
- `make models-snapshot` — regenerate models snapshot.
- `make prepare-htr-assets` — regenerate HTR bundle (needs out-of-tree sources).

## Worktree setup
```bash
git worktree add .worktrees/<branch> <branch>
cp internal/browse/cdp/htr-assets.zip .worktrees/<branch>/internal/browse/cdp/  # if tagged builds needed
```
- Copy `models-snapshot.json` too if running tagged builds/tests.
- `go test -race ./...` is a real gate here, not a formality.

## Testing notes
- A mutation test only counts if the mutant **compiles**; build-only failures are `INVALID`, not `CAUGHT`.
- `TestTerminalWS*` / pty-dependent tests fail in sandboxed sessions (`/dev/ptmx` denied) at pristine HEAD too — not a regression.
- Full-suite flakes exist (`TestStreamedToolOutputLateDeltaNotAppendedAfterFinal`, etc.); compare fail sets, don't trust a single run.

## Update triggers
- Changes to `Makefile` targets or build tags.
- New/renamed embed inputs or generated assets.
- New test gates or flake discoveries.