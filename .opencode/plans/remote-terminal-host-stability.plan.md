# Plan: Prevent remote terminal remount during project-host hydration

## Context

`HomeApp` derives terminal `host` values from `projectState.projects`, but the project list is loaded asynchronously. A restored terminal can therefore receive `host={undefined}` first and a remote host later. `TerminalPanel`'s xterm/history/WebSocket effect depends on `[projectPath, host, id]`; that transition runs cleanup, aborts restore, closes the socket, disposes xterm, and starts the lifecycle again.

Relevant source:

- `web/src/App.tsx:805-847` builds the union of project paths and forwards `projectHosts.get(pp)`.
- `web/src/components/Terminal/TerminalPanel.tsx:634-640, 744-785, 797-844` uses `host` for history/WebSocket setup and tears down the whole terminal lifecycle during effect cleanup.
- `web/src/components/Terminal/TerminalTabs.tsx:26-95` forwards `host` while keeping project terminal trees mounted across view/project switches.
- `web/src/stores/projectStore.tsx:19-63, 87-93, 380-405` loads project metadata asynchronously; `loading` is also reused for later refreshes.

The working tree contains broad unrelated changes, including terminal history work. Implementation must not reset, overwrite, or format unrelated files.

## User Expectation Checklist

- [ ] A remote terminal is initialized only after trusted project metadata for its path is available, so its first history/WebSocket request includes the SSH/WSL host.
- [ ] Initial host hydration does not dispose xterm, abort restore, close the socket, or replay history a second time.
- [ ] Initial project-load failure does not cause an unknown path to be treated as local; terminal startup remains withheld or explicitly unavailable until metadata is available.
- [ ] A later refresh failure retains the last successful metadata and leaves live terminals mounted.
- [ ] Later refreshes with unchanged metadata do not unmount or restart live terminals, even while `loading` is true.
- [ ] Missing/removed project metadata never silently changes a live remote terminal to a local terminal.
- [ ] Local projects, newly added projects, terminal tab switching, and deliberate host/terminal identity changes retain their intended behavior.
- [ ] Focused regression tests observe the relevant terminal lifecycle, not only the rendered prop.
- [ ] TypeScript/Vitest/build validation passes, with unrelated pre-existing failures recorded rather than modified around.

## Review Decision

The original sticky gate direction is retained, but its semantics are tightened:

1. A request being settled is not the same as metadata being ready. Initial failure must remain a non-ready state.
2. A global `projectsLoaded` flag is insufficient for paths preserved by `tabsByProject` or `activeProject` but absent from the latest project response. Rendering must have a defined unknown-path policy.
3. `host` remains in `TerminalPanel`'s effect dependencies. Removing it would risk connecting with the wrong host and would obscure intentional host identity changes.

## Recommended Design

### Project metadata lifecycle

Add explicit project metadata status to `ProjectState`, for example `projectsStatus: "loading" | "ready" | "error"`, plus the existing project list. Initial state is `loading` (or an equivalent sticky `projectsReady: false` state); the initial request is started by the existing provider effect.

- Successful `SET_PROJECTS` atomically replaces the list and marks metadata ready.
- Initial failure marks an error/non-ready state and does not render terminal trees for unknown paths.
- Once ready, a later refresh may set the existing `loading` flag but must not revert readiness.
- A later refresh failure leaves the last successful project list and host metadata intact.
- If multiple refreshes can overlap, use a request generation/serialization rule so an older response cannot overwrite a newer host map.

The exact action/reducer transition must be explicit because the current failure path only dispatches `SET_LOADING(false)` (`projectStore.tsx:380-388`). Do not persist this readiness field with session data; it is provider-lifetime fetch state and must be re-established after a process reload.

### Project-path/host policy

Build a metadata map from the latest successful project list, and include `activeProject` only when it is trusted and its path/host is known. For each path retained by the terminal-path union:

- known local project → mount with no host;
- known remote project → mount with its host;
- unknown/orphaned path → do not open a shell as local; render it unavailable or retain a trusted last-known host for a live terminal until explicit removal handling occurs.

Project removal must be defined explicitly: either close/remove its terminal state or keep it visibly unavailable. It must not drop the host prop while leaving a live remote terminal mounted.

New projects must become visible to the terminal renderer only after the successful response contains their complete metadata, including `host` for remote projects.

## Implementation Steps

1. Update `ProjectState` and reducer actions with explicit initial metadata readiness/error semantics. Preserve readiness and the last successful project list across later refresh failures. Handle initial failure without fail-open local terminal startup.
2. Add request-order protection if the existing refresh callers can overlap; stale results must not replace a newer project/host snapshot.
3. Update `HomeApp`'s terminal rendering to use the trusted metadata/path policy. Keep project-scoped terminal containers mounted across ordinary view switches and later refreshes; do not use mutable `loading` as the gate.
4. Retain `host` in the `TerminalPanel` lifecycle effect dependency list. Clarify the nearby comment so API/backend-origin changes, deliberate project-host changes, and missing metadata are distinct cases.
5. Add focused tests:
   - deferred successful initial load: no terminal tree before resolution; first remote tree receives the host;
   - initial rejection followed by successful retry: no unknown terminal startup; first startup uses the resolved host;
   - later refresh in progress and later refresh failure: existing terminal tree/lifecycle remains intact;
   - project omission/removal: no remote-to-local fallback;
   - local and remote fixtures, plus a new remote project;
   - deliberate post-ready host change and terminal-id change retain their documented behavior.
6. Where practical, exercise the real `TerminalPanel` test seam using the existing xterm/WebSocket mocks (`TerminalTabs.test.tsx`) or a focused `TerminalPanel` harness. Count history restore calls, WebSocket creations/closes, xterm `dispose()` calls, and effect cleanup. An App-level prop test alone is not sufficient.
7. Format only changed TypeScript files and review the targeted diff. Do not alter unrelated dirty files.

## Documentation Decision

No standalone user-facing documentation update is required: this is an internal lifecycle correction with no API, configuration, or workflow change. Add concise inline comments for the initial metadata readiness and unknown-path policy because both protect against future regressions.

## Validation Plan

| Check | Checklist items verified |
|---|---|
| Project-store tests for initial success, initial failure, later success/failure, readiness preservation, and stale-response handling | 1, 3, 4, 5 |
| App/terminal regression test with deferred project metadata and remote host | 1, 2, 5, 8 |
| Terminal lifecycle test asserting exactly one initial restore/socket/xterm lifecycle and no premature cleanup | 2, 8 |
| Unknown/removed path test asserting no silent remote-to-local connection | 3, 6 |
| `cd web && pnpm exec vitest run web/src/stores/projectStore.test.tsx web/src/App.browser.test.tsx web/src/components/Terminal/TerminalTabs.test.tsx` (adjust paths for execution from `web`) | 1-8 |
| `cd web && pnpm run build` | 1-7 |
| `git diff --check -- <changed implementation files>` | 2, 4, 7 |
| Targeted `git diff --stat`/diff review and working-tree check | 7 |
| Full `cd web && pnpm exec vitest run` when practical; record exact unrelated failures if blocked | 4-8 |

## Approval Gate

This revised plan is ready for implementation after the failure/unknown-path semantics are accepted. No production code has been changed in this review.
