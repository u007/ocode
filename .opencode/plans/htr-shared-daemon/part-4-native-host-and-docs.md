# Part 4 — Native host, socket injection, docs (Task 8)

Self-contained. You do not need to read the other parts.

**Spec:** `.opencode/plans/2026-10-01-htr-shared-daemon-spec.md`, section 4 and the
documentation list in section 6.

## Constraints that apply to this task

- **Never** write, rewrite or remove `com.htrcontrol.host`. `validateHTRNativeHostName` must keep rejecting it, and the existing test `TestNativeHostManifestDoesNotTouchStandaloneHost` must keep passing untouched.
- Keep the namespaced host `com.ocode.htrcontrol` and the extension rewrite. Only the **socket** stops being overridden.
- Never hand-edit `docs/index.md` or `docs/log.md` — they are auto-managed by the context agent.
- Re-derive every line anchor before citing it; the spec deliberately cites symbols, not line numbers.

## Symbols

**Existing** (verified 2026-10-01): `rewriteNativeHostName`, `extensionRuntimeDirName`, `pinExtensionKey` in `internal/browse/cdp/htr_assets.go`; `ensureNativeHostManifest`, `nativeHostAllowedOrigins`, `validateHTRNativeHostName`, `nativeMessagingDir` in `internal/browse/cdp/native_host.go`; `launchChromeWithOptions` (the env block that sets `HTR_SOCKET_PATH` / `HTR_NATIVE_HOST_NAME`) and `launch.go`'s `resolveExtensionDir`; `DefaultHTRPort` in `internal/browse/cdp/htr.go`.

**New in this part:** nothing — this task changes existing functions and comments only.

---

### Task 8: Point the preload at the shared socket, and correct the documentation

**Files:**
- Modify: `internal/browse/cdp/launch.go` (env block in `launchChromeWithOptions`)
- Modify: `internal/browse/cdp/manager.go` (`ManagerOptions` plumbing, if a mode flag is needed)
- Modify: `internal/browse/cdp/htr.go` (`DefaultHTRPort` comment)
- Test: `internal/browse/cdp/htr_shared_launch_test.go` (create)

**Interfaces:**
- Consumes: `HTROptions.Shared.Mode` (Task 1) threaded to the launcher as a new `ManagerOptions.HTRSharedMode string` (values `"shared"`, `"private"`, or `""` for today's behaviour).
- Produces: no new exported symbols beyond the field above.

- [ ] **Step 1: Write the failing test**

`internal/browse/cdp/htr_shared_launch_test.go`:

```go
package cdp

import (
	"os"
	"strings"
	"testing"
)

// In shared mode the preload's relay must fall back to htrcli's own socket
// (~/.htrcli/daemon.sock), which is the same socket the user's browser relay
// dials. Overriding it would re-create the two-daemon split.
func TestSharedModeDoesNotOverrideSocketEnv(t *testing.T) {
	shared := append(os.Environ(), "HTR_SOCKET_PATH="+"/Users/me/.htrcli/daemon.sock")
	if !strings.Contains(strings.Join(shared, "\n"), "HTR_SOCKET_PATH=") {
		t.Fatal("sanity: the env slice should carry the value before filtering")
	}
	got := filterSharedLaunchEnv(shared, "shared")
	if strings.Contains(strings.Join(got, "\n"), "HTR_SOCKET_PATH=") {
		t.Errorf("shared mode must not inject HTR_SOCKET_PATH:\n%s", strings.Join(got, "\n"))
	}
}

func TestPrivateModeStillInjectsSocketEnv(t *testing.T) {
	got := filterSharedLaunchEnv([]string{"HTR_SOCKET_PATH=/x.sock"}, "private")
	if len(got) != 1 {
		t.Errorf("private mode must keep injecting the socket, got %v", got)
	}
}

func TestSharedModeDropsInertNativeHostEnv(t *testing.T) {
	got := filterSharedLaunchEnv([]string{"HTR_NATIVE_HOST_NAME=com.ocode.htrcontrol"}, "shared")
	if len(got) != 0 {
		t.Errorf("HTR_NATIVE_HOST_NAME has no consumer in htrcli and must be dropped in shared mode, got %v", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/browse/cdp/ -run 'TestSharedMode|TestPrivateMode' -v`
Expected: FAIL to compile — `filterSharedLaunchEnv` undefined.

- [ ] **Step 3: Implement the filter**

In `internal/browse/cdp/launch.go`:

```go
// filterSharedLaunchEnv decides which HTR env vars reach the embedded Chromium.
//
// In shared mode the preload's relay must use htrcli's own default socket
// (~/.htrcli/daemon.sock) so it reaches the SAME daemon the user's browser
// extension attaches to; injecting ocode's socket would re-create the split.
// HTR_NATIVE_HOST_NAME is dropped because nothing in htrcli reads it and an
// extension cannot read process environment.
//
// Private (legacy) mode keeps today's behaviour exactly.
func filterSharedLaunchEnv(env []string, mode string) []string {
	if mode != "shared" {
		return env
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "HTR_SOCKET_PATH=") || strings.HasPrefix(kv, "HTR_NATIVE_HOST_NAME=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
```

Apply it at the env block in `launchChromeWithOptions`, which currently appends
`HTR_SOCKET_PATH` and `HTR_NATIVE_HOST_NAME` unconditionally. Thread the mode
from `ManagerOptions.HTRSharedMode` through `Manager.launch` (the existing call
site that already forwards `HTRSocketPath` / `HTRNativeHostName`) down to
`launchChromeWithOptions`.

- [ ] **Step 4: Correct the now-misleading comments**

These comments describe the old world and are exactly how the private daemon
gets re-added by a later reader. Update each:

- `DefaultHTRPort`'s comment in `internal/browse/cdp/htr.go` — it currently says
  the separate port "prevents ocode from attaching to or stopping a user's
  existing daemon". That is now only true of `htr_shared: false`.
- The file-header architecture comment in `internal/browse/cdp/htr.go` — update
  the default port and say which parts are shared-mode versus legacy.
- The "never reads, rewrites, or removes com.htrcontrol.host" comment in
  `internal/browse/cdp/native_host.go` — still true; leave the wording, but add
  that shared mode also relies on it.

- [ ] **Step 5: Run the full affected suites**

Run:

```bash
go build ./...
go vet ./internal/browse/cdp/ ./internal/server/ ./internal/tui/
go test -race ./internal/browse/cdp/ ./internal/server/ ./internal/config/ ./internal/tui/
```

Expected: clean build, clean vet, all tests pass. `TestNativeHostManifestDoesNotTouchStandaloneHost`
must be unmodified and still green. Note that pty-dependent tests
(`TestTerminalWS*`) can fail in a sandbox that denies `/dev/ptmx`; compare
against a pristine baseline before blaming this change.

- [ ] **Step 6: Update the documentation**

- `CHANGES.md` — new entry at the top describing the behaviour change: one
  shared daemon, `browser.htr_shared` defaulting to true, and that closing ocode
  stops the daemon when ocode started it (so the user's extension drops).
- The embedded-browser / HTR settings documentation — the two new keys, their
  defaults, and the AdoptOnly explanation.
- `skills/ocode-web/SKILL.md` — the Settings → Browser HTR field list.
- A concepts page for the shared daemon under `docs/concepts/`, **written by the
  context sub-agent** (`task` with `agent=context`), since `docs/` has a single
  automated writer. Pass the path *without* a `docs/` prefix, because
  `doc_write` prepends it. The agent also owns the `docs/index.md` and
  `docs/log.md` entries; do not hand-edit either.

- [ ] **Step 7: Verify the mutation guards**

Two mutations, each checked for compilation before being called caught
(`docs/gotchas/mutation-check-mutants-must-compile.md`):

1. Add `HTR_BEARER_TOKEN=` to `sharedServeEnv` → `TestSharedSpawnEnvOmitsBearerToken` must fail.
2. Replace `owner.StartedByPID != os.Getpid()` with `owner.OwnerPID != os.Getpid()` → `TestStopRuleUsesStartedByPIDNotOwnerPID` must fail.

A mutant that only breaks the build is `INVALID`, not `CAUGHT`.

- [ ] **Step 8: Commit**

```bash
git add internal/browse/cdp/launch.go internal/browse/cdp/manager.go \
        internal/browse/cdp/htr.go internal/browse/cdp/native_host.go \
        internal/browse/cdp/htr_shared_launch_test.go \
        CHANGES.md skills/ocode-web/SKILL.md
git commit -m "feat(htr): share one daemon with the user's browser extension"
```

---

## End-to-end verification (run after Task 8)

Live check on a real machine, with the user's own extension:

1. Stop any manually started daemon: `pkill -f "htrcli serve"` (note the pid).
2. Launch the desktop app or a TUI session.
3. `htrcli health` must report a server on the port from `~/.htrcli/config.json`.
4. `ls ~/.htrcli/daemon.sock` must exist — that is the socket ocode's preload and
   the user's extension now share.
5. Open the extension's side panel in Firefox and enable remote control;
   `htrcli tabs list` must then show the tab.
6. Quit ocode. The daemon must be gone **iff** ocode started it (step 2), and
   `htrcli health` must then fail to connect. If ocode merely adopted an
   already-running daemon, it must still be running afterwards.
7. Confirm no raw daemon output corrupted the TUI frame during any of the above.

If step 5 needs the side panel opened manually, that is expected and is recorded
in the spec's open risks — it is not a defect in this work.
