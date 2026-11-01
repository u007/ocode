# Resume guide: ocode Excel add-in

Written 2026-10-30 at the end of the first session. Read this first, then `README.md`. The design rationale and
the alternatives that were rejected are in `docs/plans/2026-10-30-excel-addin-sidepane.md`.

## 0. TL;DR

- **Built and tested without Excel.** Everything below the `ExcelPort` interface is exercised by tests; the one file that
  talks to real Excel, `taskpane/src/officePort.ts`, has **never run**. It type-checks against the real
  `@types/office-js`, which proves API names and signatures, not behaviour.
- **First thing to do on the desktop:** section 4 (setup), then section 5 (verification checklist), in order. Expect a
  handful of Office.js surprises; each checklist item says where the fix goes.
- Nothing is wired into the root ocode build, CI, or releases. `excel-addin/` has its own `go.mod` and `package.json`.
- Branch: `ccr-915ec104-s4wquj`. No PR was opened.

## 1. What exists

| piece | state |
|---|---|
| `bridge/` (Go, stdlib only) | done and tested: 31 Go tests, race detector on, mutation-checked on the security-relevant behaviours |
| `taskpane/src/tools/` (13 tools) | done and tested against an in-memory workbook; 4 safety rules mutation-checked |
| `taskpane/src/officePort.ts` | written, **unverified**; see section 5 |
| chat UI (`ui/App.tsx`) | works in headless Chromium (`make smoke`); not seen in Excel's task pane |
| manifest | generated + structurally checked (5 tests); **not validated by Microsoft's tool, never sideloaded** |
| tests | 153 TypeScript tests in 12 files + 31 Go tests + 1 browser smoke test |

Tools: `describe_workbook get_selection read_range profile_range check_errors write_range fill_formula format_range
create_table create_chart create_pivot checkpoint undo_step` (contract: `bridge/tools.json`, sorted).

### How a request flows
1. User types in the pane. `App.send` prepends a user-role `[ocode:excel]` block (workbook id, file, active sheet,
   selection) and calls `POST /api/chat` (new session, `async:true`, `project_path` = the workbook's workspace) or
   `POST /api/sessions/{id}/message`. The pane never talks to ocode directly: same-origin `/api/*` goes through the
   bridge, which swaps in the ocode token. The pane authenticates to the bridge with `X-Bridge-Key`.
2. Output arrives on `GET /api/events` (SSE, fetch-streamed because EventSource cannot send headers). `chatState.ts`
   reduces the envelopes into messages.
3. When the agent calls `excel_read_range` etc., ocode POSTs JSON-RPC `tools/call` to the bridge's `/mcp`. The bridge
   sends a `call` SSE event to the pane (`/bridge/stream`), the pane's `Runner` validates the arguments against
   `tools.json`, runs the tool through `ExcelPort`, and POSTs the result to `/bridge/result`. The bridge answers
   ocode. A call the pane does not finish within `--call-timeout` (90 s) becomes a tool error and the pane is told to
   cancel.
4. Writes record before/after images in a journal stored in the bridge (`/bridge/state/{wb}/journal`), so undo works
   after the pane is closed and reopened. Undo refuses (changing nothing) if the cells changed since.
5. The workbook id lives inside the workbook (`Office.context.document.settings`); the session list per workbook is a
   sidecar index in the bridge (`/bridge/state/{wb}/sessions`).

### File map
```
excel-addin/
  RESUME.md README.md Makefile .gitignore
  bridge/            hub.go (relay) mcp.go (JSON-RPC) server.go (routes, proxy, auth) state.go (state + workspace)
                     main.go (flags) tools.json (CONTRACT) *_test.go
  taskpane/src/      a1.ts schema.ts sse.ts port.ts fakePort.ts officePort.ts runner.ts
                     tools/{read,write,chartpivot,journal,util,index}.ts
                     bridgeClient.ts ocodeClient.ts bridgeStore.ts chatState.ts context.ts identity.ts format.ts boot.ts main.tsx
                     ui/App.tsx ui/app.css   *.test.ts   e2e.test.ts (builds and runs the real Go bridge)
  taskpane/scripts/smoke-ui.mjs   taskpane/public/icon-*.png
  manifest/manifest.template.xml  (manifest.xml is generated, gitignored: it embeds the pane key)
  scripts/gen-manifest.mjs
```

## 2. Decisions already made (and why), so you do not re-litigate them

| decision | reason |
|---|---|
| A local **bridge** sits between pane and `ocode serve` | ocode's CORS allowlist is `http://localhost` / `127.0.0.1` / the hub only; Office task panes must be HTTPS. The bridge also keeps the ocode token out of the webview and hosts the tool relay. |
| Tools reach ocode as a **remote MCP** server, **no ocode core change** | `internal/mcp` remote client is plain `POST` JSON-RPC returning JSON (no SSE, no `initialize`); it only calls `tools/list` and `tools/call`. |
| Server name must be **`excel`**; bridge tool names are **unprefixed** | ocode prefixes MCP tool names with the server name (`excel_read_range`). `CLAUDE.md` seeded in each workspace uses those names. |
| `ExcelPort` interface; tools are pure logic | everything but one file is testable without Excel. |
| `fillFormula` computes each cell's formula with `shiftFormula` | avoids depending on how Excel treats a formula assigned to a multi-cell range. Tested. |
| Journal lives in the bridge, not in workbook settings | settings size limits are unknown; the bridge survives pane reloads. 100 steps, 20 000 cells per step; a write too big to checkpoint is refused (fail closed). |
| Workspace per workbook = `<root>/<workbook id>` registered as an **ocode project** via `POST /api/projects` | `allowedProjectRoots()` is the server work dir + registered projects (not `extra_allowed_paths`, which was my first guess and is wrong). |
| Agent guidance via a seeded **`CLAUDE.md`** in the workspace | ocode loads it from the session project root; it is static, so the prompt cache stays stable. User edits are never overwritten. |
| Volatile context (selection, active sheet) is a **user-role** `[ocode:excel]` block on the message | system/tool changes bust the prompt cache (CLAUDE.md "Prompt Cache Stability"). The UI strips it from the displayed bubble. |
| `npx` of unpinned packages is not used by the agent | a run of `npx office-addin-manifest@latest` was refused by the permission classifier; the manifest is checked locally instead and Microsoft's validator is a desktop step. |

## 3. Things found that cost time (do not re-discover)

- Node >= 16: `req.on("close")` fires when the request body is consumed, not on disconnect. Use `res.on("close")` for
  SSE handlers (the smoke test's fake ocode got this wrong once).
- The bridge connects during `boot()`, before the UI mounts; `boot` replays the last status on subscribe. Without that
  the "bridge" dot stayed grey forever.
- Chromium logs every non-2xx fetch as a console error (the bridge answers 404 for never-written state, by design).
  The smoke test therefore fails on uncaught exceptions only.
- Go's `ReverseProxy` already flushes `text/event-stream`; `FlushInterval: -1` is belt and braces (a mutation of it is
  an equivalent mutant, so it is not test-covered).
- LibreOffice 24.2 (used by the xlsx benchmark) cannot calculate XLOOKUP; Excel 365 can. The tools do not care.

## 3b. Code-review round (2026-10-30): what changed after the first hand-off

A 10-finding review was fixed (by Haiku subagents, then re-verified and mutation-checked). Things that now behave
differently from what section 1-3 describe:

- **Writes are checkpointed before they happen.** The journal is loaded (and a step too large to persist is refused)
  before any cell changes; if recording fails after the write, the write is rolled back, and if the rollback fails the
  error says so and names the range. The persisted journal is bounded by bytes (4 MiB) as well as 100 steps.
- **Undo restores number formats** (captured in each step), formats first, then values. Steps recorded before this
  change have no formats and undo values only. Write results carry `undo_restores_formats`.
- **`fill_formula` shifts whole-column/row references** (`B:B`, `2:2`, `$B:D`), with `#REF!` on overflow.
- **One pane per workbook.** Each page load sends an instance token; a second pane for the same workbook evicts the
  first (it gets an `evicted` event and stops reconnecting, the UI shows a banner). A file whose URL differs from the one
  stored in the workbook (copy / Save As / move) gets a NEW workbook id, so copies do not share a journal, session list or
  workspace. Cost: renaming or moving a file starts a fresh history; a shared file opened from different local paths
  on two machines would look like a copy (**U15**, verify).
- **The pane's Undo button goes through the Runner**, the same serial queue as agent tool calls.
- **Chart overlap is checked by geometry** (`ExcelPort.findChartOverlap`: chart and range left/top/width/height),
  because Office.js does not expose a chart's anchor cell. `listCharts` still returns no data range or anchor, so
  `describe_workbook` omits `data` for charts. A second no-anchor chart on the same data is refused (overlap), not
  auto-placed.
- **`create_chart` total rows:** only a FINAL row labelled exactly Total / Totals / Subtotal / Grand total is an error;
  anywhere else it is a warning. `include_total_rows=true` overrides.
- **`format_range` borders** set inside borders only when the range has more than one row / column.
- **The bridge proxy is an allowlist** (`bridge/allowlist.go`): only the 20 (method, path) pairs the pane uses reach (18 before 2026-11-01)
  ocode; the rest is 403. `POST /api/chat` and `POST /api/projects` additionally must name a folder strictly inside the
  workspace root, `permission_mode` must be absent/`normal`/`locked` (never `yolo`; **yolo changed on 2026-11-01, see
  section 3c**), and remote hosts are refused. A leaked pane key therefore cannot start an unprompted agent elsewhere on
  the machine. Adding a pane route is a security decision.
- **Data location:** state and workspaces default to `<ocode data dir>/excel/{state,workspaces}` (mirrors
  `internal/paths.GlobalDataDir`; keep `bridge/datadir.go` in sync). The old `~/.ocode-excel` is not migrated.
  `--data-dir` / `--workspace-root` override.
- Unclean paths such as `/api/sessions/../terminal` get a 307 from Go's mux before the allowlist runs; nothing is
  forwarded either way.

New **unverified-in-Excel** members (add to checklist B): `setNumberFormats` (format-before-value order on undo; does
`"007"` survive?), `findChartOverlap` (are `Chart.left/top` and `Range.left/top` populated and in the same units?),
the borders change on a 1-cell, 1-row and 1-column range. Known remaining limits: undo itself is not atomic across
formats and values beyond a best-effort format restore; a call enqueued in the instant of an eviction can still run on
the old pane.

## 3c. Permission modes (2026-11-01)

The pane gets a permission picker: **Ask** (`normal`), **Auto** (the Jev judge auto-allows) and **YOLO** (no prompts).
The user chose this. The pane UI is not built yet; this section covers the bridge side only.

- **yolo is allowed for pane chats.** The bridge used to refuse it so a leaked pane key could not start an unprompted
  agent. Now `POST /api/chat` accepts `permission_mode` absent/`normal`/`locked`/`yolo`; `sandbox` is still refused.
  The pane must show a confirm before choosing YOLO.
- **`PUT /api/permissions/mode` is proxied.** Body `{"mode": "normal"|"locked"|"yolo", "session_id": "<id>"}`, both
  keys required, matched exactly. It sets one session's mode. ocode checks that the session exists, not that it belongs
  to a workbook, so a leaked pane key can change any session's mode. The same key can already resolve any permission
  ask, so the added risk is small, but it is real.
- **Auto is server-wide.** `GET /bridge/auto-permission` returns `{"enabled": bool}`. `PUT /bridge/auto-permission` takes
  `{"enabled": bool}` and returns `{"enabled": bool}`. These are bridge-owned routes, not proxied, so the `/api/`
  allowlist did not grow. The PUT reads ocode's `/api/config/ocode/permissions-auto`, changes only `enabled`, and writes
  the whole block back (ocode's PUT replaces the block). Turning Auto on affects every ocode session on the machine, not
  only Excel workbooks. ocode errors come back as 502 with the status and message. The read and write are not atomic
  against edits made in ocode's own UI.
- **Pre-existing bypass, closed in the same change.** Go's JSON decoder matches keys case-insensitively, but the body
  checks compared exact names. `{"project_path": "<ws>", "PROJECT_PATH": "/etc"}` passed while ocode started the chat in
  `/etc`, and `{"path": "<ws>", "PATH": "/home"}` registered `/home` as a project. A key that differs from a checked name
  only by case or Unicode folding (`bodyKeysExact`) is now refused. Tests: `bridge/allowlist_body_test.go`.
- **The pane controls (2026-11-01).** `ocodeClient.ts` calls the new routes. The status strip under the tabs holds the
  Ask | Auto | YOLO picker (`ui/controls.tsx`, mapping in `modes.ts`), the model picker (connected providers only,
  `models.ts`) and the context gauge (`GET /api/sessions/{id}/context`, `gauge.ts`). Selecting YOLO asks for a confirm
  each time. Recap and Compact sit in the ⋯ menu; Compact confirms first. The pane reads a chat's saved mode through the
  bridge's `GET /bridge/session-mode/{id}` (see 3d).
- **`GET /api/sessions/{id}/context` is allowlisted** (read-only). Its `report` (`internal/contextbudget`) lists token
  sizes, prompt text, MCP tool definitions and ambient file names; the builder has no credential field. Passed through unchanged.
- **`GET /api/auth/connect` is NOT allowlisted.** It returns `masked` (the last 4 characters of each stored key) and
  the OAuth account in `statusDetail`. The bridge reads it server-side and answers `GET /bridge/connected-providers`
  with `{"providers": [ids]}` only, where a provider counts as connected when ocode's status is `✓` (env, stored key,
  OAuth or config). Code: `bridge/providers.go`; tests: `bridge/providers_test.go`.
- The e2e test (`taskpane/src/e2e.test.ts`) now expects a yolo chat to be forwarded and a sandbox chat to be refused.

## 3d. ocode desktop sees workbooks and their chats (2026-11-01)

ocode (through the `excel` MCP server) can now see which workbooks have a connected pane, read their chats and message
them. `tools.json` is unchanged. The three tools below are bridge-local (`bridge/localtools.go`): the bridge answers them,
they never reach a pane, and they are merged into `tools/list` sorted with the contract tools.

**Pane info.** The pane posts `POST /bridge/pane-info?workbook=<id>` with the pane key (like `/bridge/result`). The body
has exactly these nine keys:

| key | type |
|---|---|
| `fileName`, `path`, `host`, `platform` | string (`path` is `""` if never saved) |
| `state` | `"new"`, `"saved"`, `"modified"` or `"unknown"`; any other value is 400 |
| `blank`, `readOnly`, `protectedView` | bool or `null` |
| `sheets` | integer, 0 or more |

Missing or unknown keys, a wrong type, or `null` in a non-nullable field: 400. Body over 16 KiB: 413. No connected pane
for the workbook: 404 (no record is created). Success: 204. The record lives on the pane: `connectedAt` (attach time) and
`lastSeen` (last post) are kept there, and both go when the pane detaches. A re-attached pane must post again.

**Workbook listing.** `GET /bridge/workbooks?limit=&offset=` (pane key, loopback only, like `/mcp`) returns
`{"total", "workbooks": [...]}`. Entries are sorted by `fileName`, then workbook id. `limit` is 1 to 200 (default 50),
`offset` is 0 or more (default 0); anything else is 400. Each entry has `workbook`, `fileName`, `path`, `host`,
`platform`, `state`, `blank`, `readOnly`, `protectedView`, `sheets`, `connectedAt`, `lastSeen`, `workspace` and
`sessions`. A pane that never posted shows `state: "unknown"`, empty strings and nulls. `workspace` is `""` until the
folder exists. `sessions` is the ids from the pane's `sessions` state key, newest first, at most 20.

**Local tools.** `workbook` is optional and resolves like the `excel_*` tools: one connected pane is used, several or
none is an error. So the chat tools only reach workbooks with a connected pane.

| tool | risk | arguments | result |
|---|---|---|---|
| `list_workbooks` | read | `limit` 1..200 (50), `offset` >= 0 (0) | the listing above |
| `read_workbook_chat` | read | `workbook?`, `session?` (default: newest in the index; must be in the index), `limit` 1..100 (20) | `{session, title, total, messages: [{role, content, tool_calls}]}`, oldest first, system messages dropped |
| `send_workbook_chat` | write | `workbook?`, `message` 1..8000 characters, `session?` (must be in the index) | `{session, started}` |

- Message `content` is cut to 2000 characters with `…[truncated N chars]`. `tool_calls` holds tool names only. A tool
  message has empty content, so tool result bodies never reach the model.
- `send_workbook_chat` with `session` calls `POST /api/sessions/{id}/message`. Without it, it calls `POST /api/chat`
  with `project_path` = the workbook workspace, `permission_mode: "normal"` and `async: true`. Every message is prefixed
  `[from ocode desktop] `.
- **`send_workbook_chat` makes the pane's agent act** (it can change the workbook). It is risk `write` and runs in
  `normal` permission mode only.
- ocode session ids must match `[A-Za-z0-9_-]{1,128}` because they reach a URL path.

**Decision: a new chat is not written to the `sessions` index.** The pane's `SessionIndex.add` is a read-modify-write
(GET the list, then PUT the whole list). The bridge does not serialize with it, so a bridge append between the pane's GET
and PUT would be lost, or would overwrite the pane's entry. So `send_workbook_chat` without `session` returns the new
session id and leaves the index alone. Consequences, all open:

1. The pane does not list a desktop-started chat (it only shows its own index).
2. `send_workbook_chat` cannot continue it: the id is not in the index, so the call is refused. Each call without
   `session` starts another chat.
3. Fix options, for a decision: (a) a bridge-owned state key (for example `desktop-sessions`) that only the bridge
   writes, checked as membership next to `sessions`. This is race-free, but the pane still does not show those chats.
   (b) Change the pane so it appends through a bridge endpoint that is atomic. This touches `taskpane/`, so it is not in
   this change.

**Other changes.** The `/bridge/stream` `workbook` must match `[A-Za-z0-9._-]{1,128}` (400 otherwise): the id names
state and workspace folders on disk. The bridge's own ocode calls go through `ocodeRequest`, which takes a query string
and a per-call read limit. Session transcript windows read up to 16 MiB; a larger response is an error, never a cut body.

**Tests.** `bridge/paneinfo_test.go` and `bridge/localtools_test.go` use an `httptest` fake ocode and a fake pane. They
cover the pane-info validation table, `lastSeen` and detach on the hub, listing order, pagination and bounds, loopback and
key checks, and each tool's happy path and refusals (unknown workbook, several workbooks, session outside the index, ocode
500, oversize and empty message, no workspace folder). Mutation spot-checks in a scratch copy were caught by the tests:
removing the session-index membership check, removing the truncation marker, and dropping the loopback guard.

**Not verified.** Only a fake ocode was used. The ocode response shapes come from reading `internal/server` and
`internal/agent` (`SessionDetail`, `agent.Message`, `ChatRequest`), not from a live `ocode serve`. Unverified: that
`POST /api/chat` accepts a workspace that the pane registered as a project, that a bridge-started chat appears in ocode's
own session list, and the real Excel pane's behaviour.

**2026-11-01 update: the bridge now writes the session index.** This supersedes the decision above. Fix option (a) was
not taken and (b) is still open, but the pane now gets a bridge endpoint:

- `POST /bridge/sessions/{workbook}` (pane key, like `/bridge/state`). Body: one `{id, title, created}`. `id` matches
  `apiSessionIDRe`, `title` is at most 200 characters (may be empty), `created` is RFC 3339. Unknown keys, trailing data,
  an empty body or a bad workbook id: 400. Body over 4 KiB: 413. Success: 200 with the whole index, newest first. A
  re-posted id leaves the index unchanged. A stored index that is not a list: 500, nothing written.
- Every writer of the `sessions` key takes one per-workbook lock: this POST and `PUT /bridge/state/{workbook}/sessions`.
  The lock is in-process only.
- `send_workbook_chat` without `session` registers the new chat through the same helper: title `[desktop] ` plus the first
  60 characters of the message, `created` = now (UTC). If registration fails after the chat started, the tool errors and
  names the session id.
- **Pane action (pane agent):** switch `SessionIndex.add` in `taskpane/src/identity.ts` to `POST /bridge/sessions/{workbook}`
  and drop its GET-then-PUT. Until then the lock does not protect the pane: a whole-list PUT built from an older GET still
  overwrites a POST that landed in between. The pane's `add` also moves a re-added id to the front; the bridge keeps the
  existing entry, so the pane must accept that.
- Side effect: `read_workbook_chat` with no `session` now defaults to the newest index entry, which can be a desktop chat.

**Tests.** `bridge/sessions_test.go` covers the POST table, idempotency, 50 concurrent POSTs (all survive), a PUT racing
POSTs (the index ends with the PUT list, and every later append is kept), a check that PUT and POST both wait on the
lock, and `send_workbook_chat` registering then continuing a chat. Removing the lock in `addSession` fails the concurrent
POST test; removing it from the PUT path fails the lock-wait test. The PUT-vs-POST race test alone does not catch a
missing lock.

**2026-11-01 update: `send_workbook_chat` posts into an existing chat only in `normal` mode.** It first calls `GET /api/sessions/{id}/status` and reads `permission_mode`; yolo, locked, sandbox, a missing or unknown value, or a failed status call is refused before the message POST.
Tests: `TestSendWorkbookChatOnlyPostsIntoANormalChat` (`bridge/localtools_test.go`) and `TestSendWorkbookChatContinuationRefusesAChatSwitchedToYolo` (`bridge/sessions_test.go`).

**2026-11-01 update: pinned MCP server and a chat's saved mode.** Pane chats and `send_workbook_chat` new chats send
`pinned_mcp_servers: ["excel"]` on `POST /api/chat` (ocode core: `ChatRequest.PinnedMCPServers`, honoured at session
creation only). `constrainBody` accepts that key only when absent, null, or exactly `["excel"]` (`pinnedServersOK`); a
case variant of the key is refused by `bodyKeysExact`. The raw `GET /api/sessions/{id}/status` is not proxied. When a
chat opens, the pane reads its mode from the bridge's `GET /bridge/session-mode/{id}`, which returns only
`{"permission_mode"}` (`refreshSavedMode` in `ui/App.tsx`, mapping `savedModeView` in
`modes.ts`): yolo shows YOLO, normal shows Ask or Auto from the server-wide switch, and locked or sandbox show a read-only
chip with the picker unchanged. A failed read is logged as a warning only. The "pane cannot read a session's mode"
statement in 3c is resolved (TODO.md entry marked done).
**Exposure of the status route:** the body is ocode's whole `TUIStatus` (cwd, `extra_allowed_paths`, `lsp_servers` with
launch commands, `model_prompt`, `modified_files`), not only the mode, and the handler starts the project's LSP manager and
warm-up (`internal/server/handler_session_state.go`, `lspManagerFor`), so a GET can start language servers. The raw route
is therefore off the `/api/` allowlist. The bridge calls it itself (`sessionPermissionMode` in `bridge/sessionmode.go`) and
answers the pane with `permission_mode` alone, so the body's other fields never leave the bridge. The LSP warm-up still runs
on each read.
Tests: `TestChatPinnedMCPServersBodyIsConstrained` (`bridge/allowlist_body_test.go`), the raw route's 403 in
`bridge/allowlist_test.go`, the narrow route in `bridge/sessionmode_test.go`, the new-chat body in `bridge/localtools_test.go`, `taskpane/src/modes.test.ts`,
`ocodeClient.test.ts` and `e2e.test.ts`.

## 4. Desktop setup (do this first)

**The ordered checklist, with the macOS pitfalls, is the `ocode-excel` skill (`skills/ocode-excel/SKILL.md`); check any
step with `scripts/preflight.sh`. Corrections found on 2026-10-31: pin `npx office-addin-dev-certs@3` and
`office-addin-manifest@3`; the manifest `<Version>` must be >= 1.0.0.0 (fixed in the template); macOS refuses writes to
Excel's container (`wef/`) unless the terminal has Full Disk Access.** The notes below are the original rationale.

Prerequisites: Excel (Windows or Mac, Microsoft 365), Go >= 1.26, Node >= 22, pnpm, a running `ocode serve`.

```bash
git checkout ccr-915ec104-s4wquj
cd excel-addin
make test                      # must be green before anything else (Go race + 153 TS tests)
make build                     # taskpane/dist + bin/excel-bridge
```

**HTTPS certificate.** Office will not load an http task pane. Generate and trust a localhost certificate with
Microsoft's tool (it asks for admin rights; read what it does first):
`npx office-addin-dev-certs install` -> files land in `~/.office-addin-dev-certs/localhost.{crt,key}`.

**Pane key** (one value shared by the bridge and the manifest, 16+ chars of `[A-Za-z0-9_-]`):
```bash
export EXCEL_BRIDGE_PANE_KEY=$(node -e "console.log(require('crypto').randomBytes(24).toString('hex'))")
export OCODE_TOKEN=<token printed by ocode serve, if auth is on>
```

**Start things** (three terminals):
```bash
ocode serve                                    # note its port, default assumed 127.0.0.1:4096
./bin/excel-bridge -static taskpane/dist \
  -ocode http://127.0.0.1:4096 \
  -cert ~/.office-addin-dev-certs/localhost.crt -key ~/.office-addin-dev-certs/localhost.key
# it prints the task pane URL and the exact ocode MCP entry
```
State and workspaces default to `<data dir>/excel/` (`state/` and `workspaces/`), where `<data dir>` is ocode's own
data directory (`~/.local/share/opencode` on macOS and Linux, `$XDG_DATA_HOME/opencode` if set, `%LOCALAPPDATA%\opencode`
on Windows). `--data-dir` and `--workspace-root` override them. See `bridge/datadir.go`.

**Register the tools with ocode.** Add to the `mcp` map of the ocode config (same place other MCP servers live; see
`docs/plugins.md` and `internal/config/config.go` `MCPConfig`):
```json
"excel": { "type": "remote", "url": "https://localhost:3443/mcp",
           "headers": { "X-Bridge-Key": "<EXCEL_BRIDGE_PANE_KEY>" }, "timeout": 120000, "enabled": true }
```
`timeout` matters: ocode's default is 20 s and a big read can take longer. Open question U8: ocode talking https to a
self-signed/dev cert; if it rejects the cert, run the bridge for `/mcp` on plain `http://127.0.0.1` (loopback is
allowed without TLS by `validateRemoteServerURL`) via a second listener, or trust the dev CA system-wide.

**Manifest and sideload.**
```bash
node scripts/gen-manifest.mjs --base https://localhost:3443        # uses EXCEL_BRIDGE_PANE_KEY
npx office-addin-manifest validate manifest/manifest.xml           # Microsoft's validator: fix anything it reports
```
Sideload per Microsoft's guide (verify against the current page, steps change):
Windows via a shared-folder catalog ([guide](https://learn.microsoft.com/en-us/office/dev/add-ins/testing/create-a-network-shared-folder-catalog-for-task-pane-and-content-add-ins)),
Mac by copying the manifest into `~/Library/Containers/com.microsoft.Excel/Data/Documents/wef/`,
Excel on the web via Insert > Add-ins > Upload My Add-in. Then Home > **Ask ocode**.

## 5. Desktop verification checklist (OfficePort and the rest)

Work top to bottom. For each failure, the fix location is given. Record results in a new section at the bottom of this
file (date, Excel version/platform, pass/fail, notes).

**A. Loads and connects**
1. Pane opens, no red error text. Both dots green (bridge, ocode events). If the pane is blank: browser devtools
   (F12 in Excel on the web; on desktop use the Edge WebView2 devtools, see Microsoft's debugging guide).
2. `curl -sk https://localhost:3443/bridge/health` lists the workbook id under `workbooks` while the pane is open.
3. Workbook id persists: close and reopen the file; the id in `/bridge/health` is the same (U4).

**B. Each `OfficePort` method** (use a scratch workbook with a small table; run via chat or by calling the MCP endpoint
directly with curl, which is faster for debugging: `POST /mcp` with header `X-Bridge-Key`)
| # | tool | expected | what to watch (fix in `officePort.ts` unless noted) |
|---|---|---|---|
| 1 | `describe_workbook` | sheets, used ranges, tables, calc mode | `getUsedRangeOrNullObject(true)` ignores format-only cells (good) but a sheet with only formatted blanks reads empty |
| 2 | `get_selection` | the user's current selection | selection on a non-active sheet; multi-area selections |
| 3 | `read_range` | values/formulas/types for `A1:C10`, `A:A`, paging | `valueTypes` strings; error cells come back as `#DIV/0!` text; very large ranges (payload limit) |
| 4 | `profile_range` | per-column stats | Excel dates are numbers: a date column shows as `number`, not `text_date`. Decide whether to add a date-aware pass using `numberFormats` |
| 5 | `check_errors` | lists error cells | |
| 6 | `write_range` (values, formulas, `number_format:"@"` then `"007"`) | evaluated read-back, step id | **U12:** does `"007"` stay text with the `@` format set first? booleans/null; very large writes |
| 7 | `fill_formula` | relative refs adjust down and across | spilling/dynamic-array formulas: not handled (U13) |
| 8 | `format_range` | bold/fill/number format/autofit/freeze; `borders` on a single cell, a single row and a block | **U10:** `columnWidth` unit; `numberFormat` 2D assignment; freeze on non-active sheet; borders must not reject the batch |
| 9 | `create_table` | table with banding | name collisions; header must be text |
| 10 | `create_chart` pie from `A1:B7` | chart anchored at the requested cell, title, percent labels | **U9:** `setPosition` end cell (8x16); percent labels; axis titles on bar charts; chart type strings |
| 11 | `create_chart` with a Total row in range | refused with the corrected address | this is pure logic; should already pass |
| 12 | `create_pivot` Region x Month sum | a real PivotTable | **U13:** `summarizeBy` strings, `filterHierarchies`, destination collisions, refresh |
| 13 | `checkpoint` then `undo_step` | cell state restored | undo of a formula cell restores the formula, not the value |
| 14 | edit a cell by hand after a write, then `undo_step` | refused, nothing changed | |
| 15 | `listCharts` / `listPivots` | names returned | chart `dataAddress`/`anchor` are empty by design (describe omits `data`) |
| 15a | `create_chart` twice on the same data with no anchor | the second is refused as overlapping | **U9:** `findChartOverlap` geometry; unit of `Chart.left/top` vs `Range.left/top` |
| 15b | `write_range` with `number_format:"@"`, then `undo_step` | format back to General, text restored | `setNumberFormats` then value order |

**C. Agent loop with a real model**
16. Ask: "what is on this sheet?" -> the agent calls `excel_describe_workbook` / `excel_read_range` (not bash).
17. **U7:** are `excel_*` tools visible on the first turn? ocode's discovery gate starts a cold turn with zero MCP
    tools (`docs/concepts/discovery-mcp-tool-gating.md`). If the agent says it cannot see Excel tools, either turn
    discovery off for these sessions or have the agent call `discover_more`. Decide and document.
18. Permission cards: a write tool should raise a `permission` event; confirm the payload fields the pane renders
    (`tool`, `summary`) exist (U14) and Allow/Deny work. Configure allow rules so read tools never prompt
    (`/api/permissions`), keep prompts for writes.
19. Stop mid-turn: the turn ends, no tool stays "running", an in-flight Excel call is cancelled (bridge logs `cancel`).
20. Reload the pane mid-session: history reappears (`getSession`), pending asks are not recovered yet (backlog B4).
21. Run the benchmark traps through the pane: `docs/okf/xlsx/probe` prompts on matching workbooks (Total row in a pie,
    blank check-in, formula overwrite). This is the real acceptance test; see `docs/okf/xlsx/PLAN.md`.

**D. Environment questions**
- **U3** Excel on the web: can `https://localhost:3443` be reached from the browser-hosted pane? (private-network
  access / mixed content). If not, the bridge must be hosted, which changes the data-path threat model.
- **U6** ocode: does a non-git workspace directory work as a project (sessions keyed by repo root, markdown discovery)?
  If not, `git init` the workspace in the bridge's `/bridge/workspace` handler. Also confirm the `projects` filter on
  `/api/events` matches the registered path string exactly (trailing slash, symlinks, `~`).
- **U5** does Excel's own undo (Ctrl+Z) include API edits? Do not advertise it until known.
- **U8** see section 4.

## 6. Backlog (nothing below is started)

**P0 (blocks calling this usable)**
- B1. Settle every item in section 5; fix `officePort.ts` accordingly; add a `docs` note of what Office.js did.
- B2. Decide discovery gating for `excel_*` tools (U7).
- B3. Permissions: ship allow rules for read tools, ask for writes; document the profile.

**P1 (features promised in the proposal)**
- B4. Pending permission/question recovery after a pane reload: read `pending_asks` from
  `GET /api/sessions/{id}/state` (shape not yet inspected; `livePendingAsks` in `internal/server`).
- B5. **Restore conversation** (rewind): needs `targetIndex`, `targetContent`, `userSeq` per user message. Use
  `GET /api/sessions/{id}/user-messages` for the authoritative list, then `POST /api/sessions/{id}/rewinds` and send the
  next message with `rewindToken`. `OcodeClient.prepareRewind` exists; the UI does not use it. Offer "restore both" /
  "workbook only" / "conversation only"; workbook restore = undo steps newest-first.
- B6. **Compare**: diff two journal states (cells old -> new, formula text) with select-in-Excel on click. The journal
  already stores before/after images. Full-file checkpoints via `Office.context.document.getFileAsync` (slices) enable a
  server-side openpyxl diff; not started.
- B7. Charts: overlap is now checked geometrically, but `listCharts` still cannot return a chart's anchor or data range in real Excel, so `describe_workbook` omits `data` and a second no-anchor chart is refused rather than auto-placed. Read `ChartSeries.getDimensionDataSourceString` (newer API set) or persist created anchors to fix both.
- B8. Missing tools from the proposal: `insert_delete` (with the impact report: formulas, charts, names, validations
  shifted or not extended; **this is the benchmark's `log-append` trap and the highest-value missing tool**),
  `named_item`, `conditional_format`, `data_validation`, `sheet`, `sort_filter`, `remove_duplicates`,
  `text_to_columns_clean`, `find_replace`, `page_layout`, `comments_notes`, `subtotal_summary`, `explain_formula`.
  Adding any tool = `tools.json` + `tools/*.ts` + `TOOLS` map + port method + tests; the contract test enforces both
  directions. Mind prompt-cache stability: tools are sorted, and a changed tool list busts the cache.
- B9. Model picker, permission-mode switch, goal, `/btw` in the pane (endpoints exist, see the plan doc table).

**P2**
- B10. Egress controls: per-sheet "never send", hidden/very-hidden sheets excluded by default, sensitive-column hints.
- B11. Packaging: an `ocode excel` subcommand that starts the bridge with the dev cert, prints sideload steps, and
  writes the MCP entry; installer; unified manifest (the add-in-only XML manifest is used now).
- B12. CI: nothing runs `make test` for `excel-addin/`. Add a workflow only if you want it; the Go tests need no
  network, the TS tests need `pnpm install`, the e2e test needs `go`, the smoke test needs Chromium.
- B13. Derived xlsx tuning skill (from `docs/okf/xlsx`) loaded for Excel sessions; compare "tools vs openpyxl" for the
  same small model (plan doc, section 10).
- B14. Large workbooks: `readRange` pages at 1000 cells by default; profile samples 5000 rows; test 50k rows.

## 7. Commands

```bash
make test | make build | make smoke | make manifest         # from excel-addin/
cd bridge && go test -race -count=1 ./...                    # 31 tests
cd taskpane && pnpm test                                     # 153 tests (e2e builds the Go bridge; skips without go)
cd taskpane && pnpm dev                                      # http://localhost:3000/taskpane.html?fake=1&key=x  (needs the bridge running for /bridge and /api)
node taskpane/scripts/smoke-ui.mjs [out.png]                 # headless Chromium end to end
DEBUG_SMOKE=1 node taskpane/scripts/smoke-ui.mjs             # logs SSE connect/close in the fake ocode
```

## 8. Decisions (confirmed 2026-10-30)

1. **Architecture: bridge now, core later.** Keep the separate bridge. Move the transport into `ocode serve` (allowed
   origin, TLS listener, first-class client-tool round trip) only if the desktop run shows Excel on the web cannot
   reach the bridge (U3), or the discovery gate keeps hiding the `excel_*` tools (U7). The tools, `ExcelPort` and
   `tools.json` stay the same either way.
2. **Deployment: personal sideload on desktop Excel (Windows/Mac) first.** Excel on the web, organisation deployment
   and AppSource are later, separate questions; a hosted bridge changes the threat model.
3. **Sessions stay visible in ocode's normal session list** (project = the workbook workspace, title `[Excel] <file>`).

Order of work once on the desktop: section 5 A and B -> settle U7 and the permission profile (B2, B3) -> build the
`insert_delete` tool (B8) -> rewind/restore and compare (B5, B6) -> only then reconsider core changes.

## 9. Desktop verification log (append results here)

| date | Excel version / platform | item | result | notes |
|---|---|---|---|---|

## 2026-11-01: first real-Excel checklist (nothing below has been seen in Excel yet)
Reload the pane after `make install-excel` + re-copy of the manifest (the template's `TaskpaneId` changed), then paste the Logs tab. Mark each line PASS / FAIL:
1. Tabs Chat / Logs / Changes render; status strip shows the Ask|Auto|YOLO picker, model dropdown, context gauge, file chips.
2. File chip is right for: a new unsaved workbook (`new`), a saved one (`saved`), after typing a cell (`modified`), an empty one (`blank`), a read-only file (`read-only`).
3. Ask "write 3 numbers in A1:A3": a permission card appears; Always allow works on the next ask.
4. Changes tab lists the write; Undo restores A1:A3; Show selects the range.
5. Logs tab shows `tool write_range ok (N ms)` and the ocode requests.
6. Model dropdown lists only connected models; switching changes the gauge's model.
7. Compact asks first, then the chat reloads with the "Earlier conversation was compacted" notice.
8. "⋯" menu: "Open ocode automatically for this workbook" toggles (UNVERIFIED Office settings API).
9. `GET /bridge/workbooks` (pane key) lists this workbook with path and state.
Known not undoable yet: format_range, create_table, create_chart, create_pivot (see TODO.md).
