# Spec: `/ide` VS Code integration — remaining tasks

Status: **core implemented, one open bug (connection keepalive).** This file
tracks what is done and what remains. Companion plan:
`~/.claude/plans/parsed-munching-metcalfe.md`.

## What is done

- New package `internal/ide`: lock discovery (`discover.go`), WebSocket + MCP
  client (`client.go`), wire parsers (`parse.go`), types + `SelectionKey`/
  `LineSpan` (`types.go`), `InVSCode()` env detector (`env.go`). Unit-tested
  (`discover_test.go`, `parse_test.go`) — all green.
- Config: `IDEMode` field + `ide_mode` JSON key + `SaveIDEMode` targeted saver
  + `IDEModeOff`/`IDEModeClaude` constants (`internal/config/ocodeconfig.go`).
- TUI wiring (`internal/tui/model.go`): model fields, `ideUpdateMsg`/
  `ideStartedMsg`, `waitForIDEUpdate`, Update cases, `/ide` command
  (`commands.go` + `runIDECmd`), `handleIDECmd` (claude/off/status),
  `connectIDE`/`autoConnectIDE`/`startIDEClient`, status chip (`ideStatusChip`),
  `/ide status` report, `at_mentioned` insert (`insertIDEMention`), auto-attach
  block in `buildSelectionContext`, sent-marking in `askAgent`, shutdown cancel
  in `cleanupCurrentSession`.
- Auto-enable: when `TERM_PROGRAM=vscode` and `ide_mode` unset, defaults to
  `claude`; explicit `ide_mode` in ocode.json always wins.
- Module rename `github.com/jamesmercstudio/ocode` → `github.com/u007/ocode`
  (repo-wide, builds clean).
- Docs: `TODO.md` (deferred opencode backend + limits),
  `skills/ocode-usage/SKILL.md` (command table).
- Verified end-to-end against the live Claude Code extension: handshake +
  `getOpenEditors` returns real tabs (untitled filtered). See open bug below.

## OPEN BUG — connection keepalive (must fix before shipping)

**Symptom:** the Go client connects, completes the MCP handshake, receives
`getOpenEditors`, then the extension closes the socket (`websocket: close 1005`)
~1s later, triggering the reconnect loop (connect → editors → drop, repeating).
Functionally it still delivers data on each cycle, but it churns and would spam
the debug log.

**Isolation so far (via python probe against the live extension):**
- `initialize` + `notifications/initialized` then **idle** → server CLOSES.
- `initialize` + `initialized` + a `tools/call` → **stayed alive 5s**.
- The Go client *does* call `getOpenEditors`+`getCurrentSelection` yet still
  drops at ~1s — so the keepalive trigger is more specific than "make any call".

**Implemented now:** `notifications/initialized` no longer sends an empty
`params:{}` object. The client omits the field entirely when params are nil, and
there is a unit test for the JSON shape. Live smoke validation is still needed
to confirm whether that change eliminates the churn.

**Leading hypotheses (test in order):**
1. `notifications/initialized` payload shape. opencode's `editor.ts` sends it
   with **no `params`**; our `client.go notify()` always includes `params:{}`.
   The extension may reject/disconnect on the unexpected params. → Make `notify`
   omit `params` when nil, and send `initialized` with no params.
2. Missing WebSocket keepalive. The extension may ping and expect pong, or
   expect periodic client traffic. gorilla auto-pongs by default, but verify;
   if needed, add a periodic `getCurrentSelection`/ping ticker (e.g. every
   10–20s) to keep the channel warm.
3. Server-initiated request we ignore. The extension may send us a request
   (e.g. roots/ping) expecting a response; not answering → close. → Log every
   inbound message during a 10s window to confirm whether the server sends
   anything before closing.

**Resolution tasks:**
- [ ] Finish the interrupted isolation experiment (cases D/E/F: exact Go
      sequence, `initialized` with vs without params, single vs double call).
- [✓] Fix `client.go`: `notifications/initialized` now omits empty params.
- [ ] Re-run the live smoke test until a single connection survives ≥30s with no
      reconnect churn, and `selection_changed` pushes arrive when text is
      highlighted in VS Code. If churn persists, add a keepalive.
- [ ] Confirm reconnect backoff still recovers from a genuinely dropped socket.

## Remaining verification & polish

- [ ] **Manual E2E in VS Code with ocode's own folder open** (so a matching
      `~/.claude/ide/<port>.lock` exists for this cwd):
      - `/ide claude` → "connected", chip shows `IDE ⚡`.
      - Highlight code → chip → `IDE <file>:L<a>-<b>` within ~1s.
      - Send a prompt → outgoing message contains the `## IDE selection` block
        (check debug panel); chip flips to sent.
      - `/ide status` → shows selection + open-tab count.
      - `/ide off` → chip clears, goroutine stops, no reconnect logs.
      - Auto-enable: launch ocode from a VS Code terminal with no `ide_mode`
        set → connects automatically; from a non-VS Code terminal → stays off.
- [ ] **TUI-safety under reconnect**: confirm all client diagnostics land in the
      debug panel (std `log`), never on the alt-screen; no frame corruption
      during connect/drop cycles.
- [ ] **Off-by-one**: spot-check multi-line selection line numbers in the
      attached context match VS Code's 1-based gutter.
- [ ] Decide fate of `internal/ide/smoke_manual_test.go` (env-gated manual
      test): keep as a documented manual check, or remove before commit.
- [ ] Update `CHANGES.md`/changelog with the `/ide` feature + module rename.
- [ ] Run full `go test ./...` once more after the keepalive fix.

## Out of scope (tracked in TODO.md)

- opencode-extension backend (`/ide opencode`): one-shot `@file#Lx-y` POST to an
  HTTP server; superseded by the Claude Code backend for live data.
- Driving VS Code from ocode (the extension's `openFile` MCP tool).
