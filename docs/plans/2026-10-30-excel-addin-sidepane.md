Proposal: Microsoft Excel add-in — ocode chat side pane with Excel-aware tools
Level: design proposal (decisions and contracts, no implementation). Status: NOT STARTED, nothing here is built.
Risk: high (spreadsheet data leaves the machine to an LLM, the agent edits a live workbook, cross-origin/HTTPS, new tool surface).

## 1. What the user asked for

A task-pane add-in inside Microsoft Excel that is a "mini chat room" for ocode:

- chat to ask for changes or answers about the open workbook;
- every conversation is a real ocode session on the server, so it is tracked, listable, restorable and
  continuable, and "Excel session" is a visible kind of session;
- everything ocode already does for a session is reachable from the pane (history, restore, compare, recap, compact,
  cancel, retry, permissions, model choice, ...);
- the agent gets tools that make it good at Excel work (read/write ranges, tables, charts, pivots, conditional
  formats, validation, names, sheets, ...). This document proposes that tool set.

Non-goals for v1: VBA/macro authoring or execution, custom worksheet functions, co-authoring conflict handling,
Outlook/Word/PowerPoint hosts (the design should not preclude them).

## 2. Facts this design rests on (checked 2026-10-30)

From the ocode repo:

| fact | consequence |
|---|---|
| `ocode serve` exposes the session API: `POST /api/chat`, `POST /api/sessions/{id}/message` (both `"async": true` -> 202, output over SSE), `GET /api/events` (SSE), `GET /api/sessions`, `GET /api/sessions/{id}` (+`/state`), `/recap`, `/compact`, `/cancel`, `/retry`, `/rewinds`, `/truncate`, `/title`, `/goal`, `/btw`, `PUT /api/sessions/{id}/model`, `/api/permissions*`, `/api/questions`, `/api/models`. | the pane is a thin client over existing endpoints; no new session concept is needed |
| Auth is `Authorization: Bearer <token>` (or `?token=` for EventSource, refused in `--remote` mode). | use `fetch` streaming for SSE so the token stays in a header |
| **CORS accepts only `http://localhost[:port]`, `http://127.0.0.1[:port]` and `https://hub.mercstudio.com`** (`allowedCORSOrigin`, `internal/server/server.go`). | an Office task pane page must be HTTPS, so its origin is rejected: the pane cannot call `ocode serve` directly |
| Session event names are fixed (`user_message`, `text`, `thinking`, `tool_start`, `tool_result`, `turn_started/heartbeat/done/error`, `permission`, `question`, `compaction_*`, `messages`, ...) and are session-scoped. | the pane renders the same stream the web UI renders |
| The MCP client supports remote servers by URL with headers (SSE transport) and local servers. | the agent can be given Excel tools without changing ocode core |
| A pending permission/question must be recoverable from live state (`pending_asks`); `Handler.mu` must never be held across slow work. | the pane must resync after reconnect via `/state`, not rely on having seen every event |
| Project dirs are per-session; `allowedProjectRoots()` is a security boundary. | a workbook session needs a project root that is allowed (see 4) |

From Microsoft docs (links at the end):

- Office.js supports **real PivotTables from ExcelApi 1.8**, and data validation APIs in 1.8. openpyxl cannot create
  PivotTables, so a live-workbook agent can do something the file-based agent cannot.
- A task pane `SourceLocation` (XML manifest) / `code.page` (unified manifest) must be an **HTTPS** URL, including
  during development (`https://localhost:3000`).
- NOT verified here, check before depending on them: the requirement set of `Range.removeDuplicates` and of
  conditional-format APIs; behaviour of Excel on the web calling a `localhost` service; whether workbook edits made by
  the API are on Excel's undo stack; custom document settings size limits.

## 3. Architecture

```
 Excel (Windows / Mac / Web)                local machine
+--------------------------+      HTTPS    +------------------------------+     http      +-------------+
| Task pane (React, Office |<------------->| excel-bridge                 |<------------->| ocode serve |
| .js)                     |  same origin  |  1. serves the add-in files  |  Bearer token |  sessions   |
|  - chat UI               |               |  2. proxies /api/* + SSE     |               |  agent      |
|  - history/restore/diff  |  WebSocket    |  3. WebSocket to the pane    |  MCP (SSE)    |  tools      |
|  - Office.js tool runner |<------------->|  4. MCP server: excel_* tools|<--------------|             |
+--------------------------+               |  5. checkpoints + journal    |               +-------------+
                                           +------------------------------+
```

The bridge is a small local HTTPS server. It exists because of three independent constraints:

1. **Origin/HTTPS.** The pane page is served by the bridge over HTTPS, and its `/api/*` calls go to the same origin,
   which the bridge forwards to `ocode serve`. No CORS change and no mixed content in the webview.
2. **Secrets.** The ocode bearer token lives in the bridge (or its config), never in the webview, the workbook or the
   add-in bundle.
3. **Tools that run inside Excel.** The agent calls `excel_*` tools over MCP; the bridge relays each call over the
   WebSocket to the pane, which executes it with Office.js and returns the result. The webview cannot host an MCP
   server itself.

Alternatives considered:

| option | verdict |
|---|---|
| Pane calls `ocode serve` directly | blocked by CORS + HTTPS unless core adds an allowed origin and a TLS listener |
| Add a first-class "client tool" round trip to ocode core (server emits `client_tool_call`, client POSTs the result, like permission/question) | cleaner long term and removes the MCP hop; a core change touching the agent loop and the cache-stable tool list. Phase 3 candidate |
| File-based: agent edits the `.xlsx` on disk with openpyxl, Excel reloads | fails when the workbook is open (locks, unsaved edits, no selection awareness, no live pivots/charts); keep only as a fallback for closed files |
| Excel LAMBDA/Copilot style in-cell calls | out of scope |

Risks of the bridge design: one more process to install/start (mitigate: `ocode excel` subcommand that starts it with
a trusted dev cert, prints the sideload steps); Excel on the web may not be able to reach a loopback service
(verify; a hosted bridge is the fallback and then the data-path/threat model in 8 changes).

## 4. Session model ("an Excel session")

- **One workbook = one conversation thread of sessions.** On first use the pane creates a workbook id (GUID) stored in
  the workbook (`Office.context.document.settings`, or a custom XML part if size demands it; verify limits). Every
  session the pane starts for that workbook records that id, so reopening the file, even on another machine pointed at
  the same server, finds its history.
- **Where the session lives.** `POST /api/chat` with `project_path` set to a per-workbook directory, for example
  `<GlobalDataDir>/excel/<workbookId>/` (same idea as `<GlobalDataDir>/pulse`). That directory is also the
  workspace for checkpoints, exports and any scratch files. It must be accepted by `allowedProjectRoots()`; do NOT
  widen that boundary globally (Pulse adds its root only to session lookup). Needs a small server change or a
  registered project: open item.
- **Marking the kind.** Title prefix `[Excel] <file name>` for humans, plus a session metadata key `client=excel`
  and `workbook_id=<guid>` for filtering. Metadata keys the TUI must not overwrite belong in
  `sessionOwnedMetadataKeys` (see CLAUDE.md goal-loop note). Until `GET /api/sessions` can filter by metadata, the
  bridge keeps a sidecar index `workbookId -> [sessionId...]` and the pane lists from that.
- **Cached prompt prefix.** The tool list must be byte-stable per session and sorted (prompt-cache rules in
  CLAUDE.md). Volatile workbook context (active sheet, selection, used range size) goes into a **user-role**
  `[ocode:excel]` block prepended to the user message, never into system or tools.
- **Which workbook a tool acts on.** MCP calls carry no session id, so every `excel_*` tool takes a `workbook`
  argument (the id above), taken from the `[ocode:excel]` context block. The bridge rejects a call for a workbook
  with no connected pane.

## 5. Side-pane feature map

| pane feature | ocode capability it reuses | notes |
|---|---|---|
| Send message, streaming answer | `POST /api/sessions/{id}/message` async + SSE `text`/`thinking`/`tool_*` | render tool calls compactly: "Read Sheet1!A1:F24", "Wrote 12 cells" |
| New chat | `POST /api/chat` (new session) | binds workbook id, project_path, permission mode |
| History list | `GET /api/sessions` filtered by sidecar index | title, last activity, turn count, status |
| Open an old session | `GET /api/sessions/{id}` (+ `/state` for revision/pending asks) | read-only until the user sends |
| Continue | message to the same id | pane shows a banner if the workbook changed since the session's last checkpoint |
| Restore | `POST /api/sessions/{id}/rewinds` then message with `rewindToken` (conversation) **and** the checkpoint journal (workbook), see 7 | two independent things, offered together, never silently coupled |
| Compare | checkpoint diff, see 7 | between two turns, or turn vs now |
| Recap | `GET /api/sessions/{id}/recap` | one tap summary of what changed in the workbook |
| Compact / context gauge | `POST .../compact`, `GET .../context` | important: range reads are large |
| Stop | `POST .../cancel` | must also cancel any in-flight tool run in the pane |
| Retry | `POST .../retry` | |
| Permissions / questions | `permission` and `question` events + `/api/permissions/resolve`, `/api/questions` | render as inline cards; resolve from the pane |
| Permission mode | `PUT /api/permissions/mode` (normal / yolo / locked / sandbox) | default `normal`; see 6 for per-tool policy |
| Model picker | `GET /api/models`, `PUT /api/sessions/{id}/model` | |
| Goal loop, `/btw`, title generate, export, share | existing endpoints | later |
| Slash commands | the same command list the web UI uses | pane forwards `/recap`, `/compact`, ... |

Resync rule: on reconnect the pane calls `GET /api/sessions/{id}/state` and rebuilds pending asks from it; it never
assumes it saw every event. One SSE connection for the whole pane, filtered client-side by session id.

## 6. Proposed Excel tools (`excel_*`)

All are implemented with Office.js in the pane, exposed over MCP by the bridge. Names are stable and sorted. Inputs
use A1 addresses with an explicit sheet. Every tool returns a bounded result (cell count and byte caps with a
`truncated` flag the model must respect).

### 6.1 Understand the workbook (read-only, allowed without asking)

| tool | purpose |
|---|---|
| `excel_describe_workbook` | sheets, used ranges, tables, named ranges, charts, pivots, protection state, calc mode: the orientation call |
| `excel_get_selection` | active sheet, selected address, values (bounded): "this column", "these rows" |
| `excel_read_range` | values / formulas / number formats / value types (detects text-numbers, error cells, dates) for a range; paged |
| `excel_profile_range` | per-column type mix, blanks, distinct count, min/max, duplicates, text-that-looks-numeric: the first step of any cleanup |
| `excel_find` | find/search across sheets (value or formula, whole-cell, match case) |
| `excel_get_formulas_audit` | list formulas, dependents/precedents of a cell, hard-coded constants inside formulas, formulas referencing blanks |
| `excel_check_errors` | every error value (`#REF!`, `#DIV/0!`, `#NAME?`, ...) with address: the verification step the benchmark shows models skip |
| `excel_list_objects` | charts (type, series refs, title, anchor), pivots, conditional formats, validations, merges, comments |
| `excel_get_formatting` | fonts, fills, widths, number formats of a range, so new cells can match |

### 6.2 Change cells (write, checkpointed, policy: ask or auto per mode)

| tool | purpose |
|---|---|
| `excel_write_range` | values and/or formulas for a range (2D array), optional number format; refuses to overwrite non-empty cells unless `overwrite=true` |
| `excel_fill_formula` | write one formula and fill down/across with correct relative/absolute handling |
| `excel_clear_range` | contents / formats / all |
| `excel_insert_delete` | insert/delete rows/columns/cells; **reports every formula, chart, name and validation range it shifted or failed to extend** |
| `excel_sort_filter` | sort, AutoFilter, clear filter |
| `excel_remove_duplicates` | on chosen columns after normalisation; returns removed count and addresses (verify API set) |
| `excel_text_to_columns_clean` | trim, collapse spaces, non-breaking spaces, text->number, text->date with explicit format, case normalisation via a mapping table |
| `excel_find_replace` | scoped find/replace with a preview count first |

### 6.3 Structure and presentation

| tool | purpose |
|---|---|
| `excel_sheet` | add / rename / move / hide / delete / duplicate sheet, freeze panes, tab colour, gridlines |
| `excel_table` | create/resize a Table, totals row, style, structured references |
| `excel_format_range` | font, fill, borders, alignment, number format, column widths and auto-fit (Excel has autofit; openpyxl does not), merge/unmerge |
| `excel_conditional_format` | cell-value, text, formula, data bar, colour scale, icon set rules (verify API set); lists and removes existing ones |
| `excel_data_validation` | dropdown list (inline or from a range), number/date/length rules, input and error messages |
| `excel_named_item` | create/update/delete names; repair names after structural edits |
| `excel_page_layout` | orientation, fit to width, print titles, print area, headers/footers |
| `excel_comments_notes` | read/add/resolve comments |
| `excel_protection` | protect/unprotect sheet **only on explicit request**, never to get around a locked cell |

### 6.4 Analysis, charts, pivots

| tool | purpose |
|---|---|
| `excel_chart` | create / modify / delete: type, series refs, categories, title, axis titles, legend, data labels, placement relative to a range; **validates the ranges** (rejects a Total row inside a pie, header counted as data, anchor over data) and returns the final refs |
| `excel_pivot` | create a **real PivotTable** (ExcelApi 1.8): source, destination, row/column/filter/data hierarchies, summarise-by, layout; refresh; the main capability win over file-based editing |
| `excel_subtotal_summary` | helper that writes a live SUMIFS/COUNTIFS summary grid when a pivot is not wanted (the grid the benchmark's `sales-pivot` task asks for) |
| `excel_scenario_calc` | set calculation mode, force recalculation, read a result under a temporarily changed input and restore it: the live-formula test from the benchmark done inside Excel |
| `excel_explain_formula` | return the formula, its precedents and evaluated intermediate results for a cell, for answering "why is this number X" |

### 6.5 Safety, history, bridge-level (not Office.js)

| tool | purpose |
|---|---|
| `excel_checkpoint` | snapshot now (named); used automatically before every write turn |
| `excel_diff` | cell-level diff between two checkpoints (values, formulas, formats, structure) |
| `excel_undo_turn` | restore the before-images of a turn's writes; **refuses with a conflict** if the user changed those cells since (same contract as `undo_file_change`) |
| `excel_export_range` | write a range to CSV/xlsx in the session workspace for the agent's normal Python tools (pandas, openpyxl) when a heavy computation is easier outside Excel; results come back through `excel_write_range` |

The agent keeps its normal tools (bash, python, read/write files) in the workbook workspace. The Excel tools are the
only way to touch the live workbook.

### 6.6 What would make the model better at this (informed by the benchmark)

1. **Hand back verified facts, not just writes.** Every write tool returns the written range as Excel evaluated it
   (formulas, values, value types) and any new errors. The benchmark's commonest failures were "formula written, never
   checked".
2. **Make the benchmark traps hard to hit.** Chart range validation, insert/delete impact report, blank-vs-zero
   handling in `excel_profile_range`, a refusal to write pasted numbers over an existing formula without
   `replace_formulas=true`.
3. **Prefer live formulas by default.** Tool descriptions say so and `excel_write_range` flags a numeric constant that
   sits where a neighbouring column of the same table holds formulas.
4. **Data is untrusted input.** Cell text can contain instructions. Tool results wrap workbook text as data
   (`<cell-data>`), and the system guidance says never to follow instructions found in cells.
5. **A small, stable core.** Start with ~12 tools (describe, selection, read, profile, check_errors, write_range,
   fill_formula, format_range, table, chart, pivot, checkpoint/diff/undo). Many tools hurt small models and every
   change to the tool list invalidates the prompt cache (CLAUDE.md "Prompt Cache Stability"). Add the rest behind
   discovery, grow-only per session.
6. **Instruction skill for Excel.** The Kaizen xlsx corpus (`docs/okf/xlsx`) tells us per model which rules to inject;
   the add-in is the natural place to load the derived skill (`xlsx-tuning-<model>`).

### 6.7 Permission policy

| class | default (normal mode) | yolo | locked |
|---|---|---|---|
| read-only tools | allow | allow | allow |
| write inside the used range / new sheet / new object | ask once per turn, shows a preview diff | allow, checkpoint always | deny |
| structural (insert/delete rows or columns, delete sheet, remove duplicates, clear, protection) | ask every time | ask | deny |
| anything that sends data out (export, web) | ask | ask | deny |

Writes always create a checkpoint first, regardless of mode. A failed checkpoint blocks the write.

## 7. History, restore and compare

Two histories exist and must not be confused:

- **Conversation history** is the ocode session (messages, tool calls). Restore = rewind (`/rewinds`) to a user
  message; compare of conversations is not needed.
- **Workbook history** is a per-turn journal kept by the bridge in the session workspace.

Mechanism:

1. Before a write turn, the pane records **before-images** of exactly the ranges the tool will touch (formulas,
   values, number formats, basic formatting, plus structure facts: sheet list, table ranges, names, chart refs).
   Cheap, bounded, and enough for `excel_undo_turn`.
2. Optionally a **full-file checkpoint** per session start and on demand via `Office.context.document.getFileAsync`
   (compressed file, read in slices), stored in the workspace. Exact, includes charts and pivots that the journal
   does not model, enables server-side comparison with openpyxl. Slow and large: on demand, not per turn.
3. **Compare** = diff between two journal states or two full checkpoints: changed cells (old -> new, formula text),
   added/removed sheets, objects whose definition changed. The pane lists changes, selecting one selects the range in
   Excel and highlights it.
4. **Restore workbook to turn N** = apply inverse journal entries newest-first; before each, verify the cell still
   holds what the agent wrote, otherwise report a conflict and skip (never overwrite the user's later edit).
   **Restore conversation to turn N** = rewind. The pane offers "restore both", "workbook only", "conversation only".
5. Continue after restore: the first message of the continued session carries a user-role `[ocode:excel]` note stating
   the workbook was restored to turn N so the model does not rely on stale context.

Open: whether Excel's own undo stack records API edits (not verified); treat it as unreliable and do not advertise
"Ctrl+Z undoes the AI".

## 8. Security and privacy

- **Data egress is the headline risk.** Selected cells and tool results go to the configured LLM provider. The pane
  shows the active model/provider, offers "local model only" and a per-workbook "never send sheet X" list enforced in
  the pane before any tool result leaves (hidden/very-hidden sheets excluded by default, columns named like
  ssn/iban/password flagged).
- **Prompt injection through cell text, comments and sheet names.** Treat as data (6.6.4); destructive tools always ask,
  so injected text cannot cause a silent delete; the permission card shows the tool arguments, not the model's prose.
- **Token handling.** The bridge holds the ocode token; the pane authenticates to the bridge with a per-launch secret
  delivered through the sideload flow, never stored in the workbook. Bridge binds to loopback, validates `Origin`, and
  uses a locally trusted certificate generated for the user (no shared key shipped in the add-in).
- **Workbook contents in the session store.** Session files under the ocode data dir will contain spreadsheet data.
  Document it, and exclude the workspace from sync/cloud connectors unless the user opts in.
- **No macro execution, no external-link refresh, no `Application.Run`.** Tools never trigger them.
- **Sandbox mode** applies to the agent's `bash` only (CLAUDE.md); it does not protect the live workbook. The tool
  policy in 6.7 is the workbook protection.

## 9. Constraints from ocode's own rules (do not violate)

- Stable, sorted tool definitions; no per-turn variation in `tools` or `system` (prompt cache).
- Volatile selection/context only as user-role tail blocks.
- Async chat only (`"async": true`), never pin an HTTP connection for a turn; one SSE stream per pane.
- Any new session-scoped SSE event is a two-part change (`sessionScopedEvents` + `liveFrameEvents` if replayable).
- Pressing Stop must answer every outstanding tool call and cancel nested runs; the pane's tool runner must return a
  cancelled result for in-flight calls (no orphans).
- New goroutines in `internal/server`/`agent` go through `crashguard`; the bridge, if written in Go, follows
  the same rule.
- Never call `os.Getwd()` in a handler; the bridge resolves workspaces from explicit config.

## 10. Phased plan

| phase | scope | exit test |
|---|---|---|
| 0 Feasibility (1-2 days) | hello-world task pane, HTTPS bridge, one `excel_read_range` over MCP, one chat round trip. Answer the unverified questions in 2. | pane asks a question, agent reads a range and answers; works on Windows and Mac; Excel on the web tested and documented |
| 1 Chat MVP | pane UI: chat, stream, permissions/questions, model picker, history list, open/continue, stop; tools: describe, selection, read, profile, check_errors, write_range, fill_formula, format_range | 8 of the 13 benchmark tasks (below) pass end to end through the tools |
| 2 Safety and history | checkpoints, journal, undo turn, diff, compare view, restore (both modes), recap, compact | restore leaves no cell the user edited later overwritten; compare matches a reference diff |
| 3 Full toolset | table, chart (validated), pivot (real), conditional format, validation, names, page layout, sheet ops, dedupe, find/replace, comments | all 13 benchmark tasks pass; `excel_pivot` creates a real pivot that refreshes |
| 4 Hardening | per-sheet egress rules, large-workbook paging, cancel semantics, reconnect/resync, telemetry (tool latency, error rates), docs, installer (`ocode excel`) | soak test: 200-turn session, reconnect mid-turn, 50k-row sheet |
| 5 (option) core "client tool" protocol | replace the MCP relay with a first-class client-tool round trip in `ocode serve` | same tests, one fewer process |

### Reusing the benchmark as the acceptance suite

The 13 live tasks in `docs/okf/xlsx/probe` are written as office requests and checked on the saved workbook. Run the
same prompts through the add-in against fixture workbooks, export the workbook (`getFileAsync`) and run `check.py` on
it. Two payoffs:

1. regression tests for the tools (a tool bug shows as a failed task);
2. a direct measurement of **tools vs code**: the same small model on `sales-pivot`, `log-append`, `expense-pie`
   with `excel_*` tools versus openpyxl. If tool-based scores are much higher, the add-in is the better route for small
   models, which is the question behind the whole exercise.

Additions needed to the checker for this: a way to export the live workbook, and tolerance for Excel-saved files
(Excel writes cached values, so LibreOffice recalculation becomes optional).

## 11. Open questions (need an answer before phase 1)

1. Excel on the web and a loopback bridge: reachable, or must the bridge be hosted? Mac Excel same question.
2. Allowed project root for per-workbook workspaces: add a registered root in `ocode serve`, or have the pane create
   a normal project directory and use that?
3. Where the workbook id and session ids live: `document.settings` vs custom XML part (size limits).
4. Whether Excel's undo stack includes API edits; confirm the real requirement sets of `removeDuplicates` and
   conditional-format APIs.
5. MCP SSE transport reliability for long tool calls (timeout default 20 s in `NewRemoteClient`; set larger per server
   for big reads) and behaviour when the pane disconnects mid-call.
6. Licensing/distribution: sideload (dev) vs organisational deployment vs AppSource; unified manifest vs XML manifest
   (unified is the current direction; unified manifests are not supported on network-share catalogs).
7. Is a single local bridge acceptable to the user, or must the add-in work with only `ocode serve` (then a core change
   for CORS + TLS + client tools is on the critical path).

## Sources

- [Excel JavaScript API requirement sets](https://learn.microsoft.com/en-us/javascript/api/requirement-sets/excel/excel-api-requirement-sets?view=common-js-preview)
- [Excel JavaScript API requirement set 1.8 (PivotTables, data validation, charts)](https://learn.microsoft.com/en-us/javascript/api/requirement-sets/excel/excel-api-1-8-requirement-set?view=word-js-preview)
- [Excel.PivotTable class](https://learn.microsoft.com/en-us/javascript/api/excel/excel.pivottable?view=excel-js-preview)
- [Remove duplicates using the Excel JavaScript API](https://learn.microsoft.com/en-us/office/dev/add-ins/excel/excel-add-ins-ranges-remove-duplicates)
- [SourceLocation element in the manifest file](https://learn.microsoft.com/en-us/javascript/api/manifest/sourcelocation?view=word-js-preview)
- [Create add-in commands with the unified manifest for Microsoft 365](https://learn.microsoft.com/en-us/office/dev/add-ins/develop/create-addin-commands-unified-manifest)
