# Task 8: Full-suite verification and manual rollout check

**Files:**
- None modified — this task is verification-only.

**Interfaces:**
- Consumes: everything from Tasks 1–7.
- Produces: confidence that `internal/session`'s public API (unchanged
  signatures throughout) still behaves correctly for every existing
  caller — `internal/server`, `internal/tui`, `internal/runcli`,
  `internal/desktop` — with zero code changes required in those packages.

- [x] **Step 1: Full build and package test suite**

Run:
```bash
go build ./...
go vet ./...
go test ./internal/session/... -race -v
```
Expected: all green. `go build ./...` in particular confirms
`cmd/ocode-desktop` (which links against `internal/session` transitively
through `internal/server`) still builds — the Wails-based desktop binary
is the one this plan's originating investigation found using 2.6–3.2GB of
memory.

- [x] **Step 2: Cross-compile check**

Run:
```bash
GOOS=linux GOARCH=amd64 go build -o /dev/null ./...
GOOS=windows GOARCH=amd64 go build -o /dev/null ./...
```
Expected: both succeed with default `CGO_ENABLED` — confirms
`modernc.org/sqlite` (Task 1) doesn't break `make build-all`, which has no
C cross-toolchain configured for these targets.

- [x] **Step 3: Consumer package regression check**

Run:
```bash
go test ./internal/server/... -run 'Session|Spending' -v
go test ./internal/tui/... -run 'Session' -v
```
Expected: green. These packages only call `internal/session`'s public
functions (`Save`, `Load`, `List*`, `Delete`) — a real regression here
would surface as a behavior difference (wrong session content, wrong
listing), not a compile error, since no signatures changed.

- [x] **Step 4: Manual smoke test — new session**

```bash
make build   # or: go build -o bin/ocode .
./bin/ocode  # start a TUI session in a scratch directory
```
In the TUI: send one message to start a brand-new session. Then, outside
the TUI:
```bash
ls ~/.local/share/opencode/project/<slug>/sessions/ | grep <new-session-id>
```
Expected: a `<id>.sqlite` file exists; no `.json` or `.ojsonl` file for
that id.

- [x] **Step 5: Manual smoke test — migration on resume**

Pick an existing session in this repo's own project directory that is
still `.ojsonl` (there are thousands per this plan's originating
investigation — any of them):
```bash
ls ~/.local/share/opencode/project/<this-repo-slug>/sessions/*.ojsonl | head -1
```
Resume that session in the TUI (`/resume <id>` or the session picker) and
send one message. Then:
```bash
ls ~/.local/share/opencode/project/<this-repo-slug>/sessions/ | grep <that-id>
```
Expected: `<id>.sqlite` now exists; `<id>.ojsonl` is gone. Resume the same
session again and confirm the full prior transcript (not just the new
message) is still present in the chat.

- [x] **Step 6: Manual smoke test — sidebar/listing during the transition**

With the mix of migrated and unmigrated sessions from Steps 4–5 still on
disk, open the desktop app or web UI, select this project, and confirm
the session sidebar lists both the newly-migrated session and the
still-`.ojsonl` sessions, sorted by recency, with correct titles.

- [x] **Step 7: Note the rollout shape (no code change — confirms Global Constraints)**

This plan's INDEX.md Global Constraints already state: migration is
lazy-only, permanent (not a temporary shim), and the legacy `.json`/
`.ojsonl` read/list code paths must not be removed as a follow-up cleanup
without an explicit new decision from the user. No action needed here
beyond confirming Steps 1–6 passed — this step exists so a plan executor
doesn't mistake "all tests green" for "safe to also delete the legacy
code now."

- [x] **Step 8: Final commit (if anything was left uncommitted)**

```bash
git status
```
Expected: clean — every prior task already committed its own changes.
If anything is uncommitted (e.g. a fixup from this task's manual testing
touched a tracked file, which it shouldn't), stop and report it rather
than committing blind.
