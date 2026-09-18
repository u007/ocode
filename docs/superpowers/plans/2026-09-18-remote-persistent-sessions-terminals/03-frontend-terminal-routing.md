# Part 3: Frontend — Remote Terminals Through the Proxy

**Spec:** `docs/superpowers/specs/2026-09-18-remote-persistent-sessions-terminals-design.md`, Section 1.

**Context for this part.** The SPA builds the terminal websocket URL in `buildTerminalWsConnection` (`web/src/components/Terminal/TerminalPanel.tsx`). For a project with a `host` it appends `host=` and `port=` so the *local* server ptys an ssh/wsl.exe into the host. Terminal HTTP calls are `restoreTerminalHistory` (`terminalHistory.ts`, `GET /api/terminal/{id}/history`) and the DELETE in `closeTerminal` inside `web/src/stores/terminalStore.tsx` (around line 66). `remoteApiBase(host)` in `web/src/api/client.ts` returns `/api/remote/<encoded host>` or empty for local; `apiPath` and `apiWsPath` add the backend base. The local server's reverse proxy (`/api/remote/{host}/api/{rest...}`) registers the project on the host when the request carries an `X-Ocode-Project` header, and after Part 1 it rewrites the websocket subprotocol token. Terminal tabs are persisted per project in `terminalPersistence.ts` (`loadProjectTerminals`, `saveProjectTerminals`, keyed by bare project path) and held in the terminal store keyed by `byProject[projectPath]`.

After this part a remote project's shell is a child of the remote server: it survives laptop sleep, app restart, and reconnect, and reattaches by the same terminal id.

Constraints: local projects keep byte-identical URLs and keys; no `host=` or `port=` param on a proxied URL; every catch logs; TDD with vitest (`cd web && pnpm test -- <file>`); surgical diffs.

---

### Task 7: Route remote terminal traffic through `/api/remote/{host}`

**Files:**
- Modify: `web/src/components/Terminal/TerminalPanel.tsx` (`buildTerminalWsConnection`; the history and upload call sites that need the host)
- Modify: `web/src/components/Terminal/terminalHistory.ts` (`restoreTerminalHistory` gains `host` and `projectPath` in its options)
- Modify: `web/src/stores/terminalStore.tsx` (`closeTerminal` DELETE uses the host prefix and header; the store must know each project's host, so `openTerminal` and `closeTerminal` accept the host or the store reads it from the project list)
- Modify: `web/src/components/Terminal/TerminalTabs.tsx` (pass `host` down to the store calls)
- Test: `web/src/components/Terminal/TerminalPanel.wsAuth.test.ts`, `web/src/components/Terminal/terminalHistory.test.ts`, `web/src/components/Terminal/TerminalTabs.test.tsx`

**Interfaces produced.**
- `buildTerminalWsConnection`: when `host` is set, the path is `${remoteApiBase(host)}/api/terminal/ws` with `project_path`, `terminal_id`, and `history_offset` only. No `host=`, no `port=`. Auth toward the local server is unchanged (`?token=` normally, `ocode.bearer.<localToken>` subprotocol when `isRemote`). The proxy replaces that subprotocol on the way to the host.
- `restoreTerminalHistory({ …, host, projectPath })`: fetches `${remoteApiBase(host)}/api/terminal/{id}/history` and sends `X-Ocode-Project: projectPath` when `host` is set.
- Terminal kill: `DELETE ${remoteApiBase(host)}/api/terminal/{id}` with the same header when `host` is set.
- Local calls (no host) produce exactly today's URLs and headers.

- [x] **Step 1: Write failing tests**:
  - `TerminalPanel.wsAuth.test.ts`: with `host: "user@box"` the URL starts with `/api/remote/user%40box/api/terminal/ws` and its query has no `host` and no `port`; with no host the URL is unchanged from the existing assertions.
  - `terminalHistory.test.ts`: with a host the fetch URL is prefixed and the `X-Ocode-Project` header is present; without a host neither.
  - `TerminalTabs.test.tsx`: closing a tab on a host project calls DELETE on the prefixed URL with the header (extend the existing DELETE assertion around line 198).
- [x] **Step 2: Run** the three test files. Expected: FAIL.
- [x] **Step 3: Implement** the URL and header changes, threading `host` from `TerminalTabs` props into the panel, the history call, and the store.
- [x] **Step 4: Run** `cd web && pnpm test -- Terminal`. Expected: PASS, including the other `TerminalPanel.*.test.tsx` files.
- [ ] **Step 5: Commit** `feat(web): remote project terminals run on the host via the remote proxy`.

---

### Task 8: Host-qualified terminal persistence key

**Files:**
- Modify: `web/src/components/Terminal/terminalPersistence.ts` (`loadProjectTerminals`, `saveProjectTerminals`, and the GC helper that iterates `file.projects`; add `projectTerminalsKey(path, host?)`)
- Modify: `web/src/stores/terminalStore.tsx` (`byProject` keyed by the same key; `openTerminal`, `closeTerminal`, `activate`, `setActiveId`, `renameTerminal`, `setOscTitle`, `markAlerted`, `clearAlert` receive or derive the host)
- Modify: `web/src/components/Terminal/TerminalTabs.tsx` (pass the host into every store call)
- Test: `web/src/components/Terminal/terminalPersistence.test.ts` (create if absent), `web/src/components/Terminal/TerminalTabs.test.tsx`

**Why.** A local project and a remote project at the same path share tabs and reattach ids today. `projectSessionKey(path, host)` in `web/src/stores/projectStore.tsx` already solves this for sessions with `host::path`; terminals get the same shape.

**Interfaces produced.**
- `projectTerminalsKey(path: string, host?: string): string` returns `host::path` when host is set, else `path` (identical to `projectSessionKey`; reuse it by import rather than duplicating).
- Existing bare-path entries in localStorage keep loading for local projects. No migration of remote entries: a remote project's old bare-path entry is simply not found, and the sidebar list from Part 4 offers reattach.

- [x] **Step 1: Write failing tests**: save under `(path, host)` then load with the same pair returns it and load with `(path)` alone returns null; a pre-existing bare-path entry loads for `(path)`; `TerminalTabs` for a host project persists under the qualified key.
- [x] **Step 2: Run** the two test files. Expected: FAIL.
- [x] **Step 3: Implement** the key helper and thread it through the store and tabs.
- [x] **Step 4: Run** `cd web && pnpm test -- Terminal`. Expected: PASS.
- [ ] **Step 5: Commit** `fix(web): key terminal tabs by host and path for remote projects`.
