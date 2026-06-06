# LSP Status — Sidebar Section & Log Tab Integration

**Date:** 2026-06-07  
**Status:** Approved for implementation

---

## Goal

Surface real-time LSP server state and diagnostic counts in the TUI so the user knows which language servers are active, what they found, and when they are still indexing — without leaving the chat tab or running `/lsp`.

---

## Approved Design (Approach B)

A dedicated **LSP section** in the sidebar (parallel to Git and Files sections), each active server on its own row. A new `KindLSP` filter in the log tab captures server-start and diagnostic-update events. Both surfaces update reactively when the LSP manager fires events.

---

## Sidebar: LSP Section

### Layout

Placed in the scrollable section of `buildSidebarRenderData`, as a new named section. The existing combined `appendScrollSection("Tools", mcpLine + lspLine)` line is split: MCP stays in the Tools section alone; LSP gets its own section below it.

```
── Tools ────────────────────────
  MCP: 3 configured, 12 loaded

── LSP ──────────────────────────
  gopls         ● 3 errors
  ts-server     ✓ clean
  pyright       ◌ indexing…
```

- Each row: `  <binary-name>  <state-symbol> <label>`
- **Rows are grouped by binary name**, not by extension. `typescript-language-server` handling `.ts/.tsx/.js/.jsx` produces one row, not four. Counts are summed across all extensions that binary serves.
- Binary name is the short name from `serversByExt.cmd` (e.g. `gopls`, `ts-server`, `pyright-langserver`), truncated to fit the column.
- When no servers have started yet, the section header is **omitted entirely** — no "(none active)" row. The section appears only once at least one server has been lazily started.

### State Machine Per Server (by binary)

| State | Symbol | Label | Trigger |
|---|---|---|---|
| Not started | *(section absent)* | — | No client for this binary ever started |
| Indexing | `◌` | `indexing…` | Within 3 s of server-start event |
| Clean | `✓` | `clean` | 0 diagnostics across all its extensions |
| Errors | `●` | `N error` / `N errors` | 1+ error-severity diagnostics |
| Warnings only | `△` | `N warning` / `N warnings` | Warnings, 0 errors |
| Mixed | `●` | `N errors, M warnings` | Both present |

**Indexing note:** The 3s timer is a deliberate approximation. The LSP client drops `$/progress` notifications (by design), so actual indexing completion is not observable. The timer is a good-enough heuristic for the display. It is tracked via a typed `lspIndexingDoneMsg{cmd string}` message (see Reactivity section) so per-binary expiry is unambiguous.

### Colors

Reuse existing sidebar styles:
- `◌ indexing…` — `sidebarTextStyle` (dim)
- `✓ clean` — green (`lipgloss.Color("#9ECE6A")`)
- `● N errors` — red (`lipgloss.Color("1")`)
- `△ N warnings` — yellow (`lipgloss.Color("#E0AF68")`)
- `● N errors, M warnings` — red

---

## Log Tab: KindLSP Events

### New Entry Kind

Add `KindLSP EntryKind = "LSP"` to `internal/debuglog/debuglog.go`.

Expose as `DebugKindLSP` in `internal/tui/debuglog.go` alongside the existing aliases.

### Filter Toggle

Add a fifth filter button `[5]LSP` to the log tab's kind bar (after `[4]ERROR`). Key `5` toggles `DebugKindLSP` visibility. Extend `handleLogKeys` with `case "5": m.toggleLogKind(DebugKindLSP)`.

### Events Emitted

| Trigger | Log message format |
|---|---|
| Server starts (after Initialize) | `gopls started  (go · <root>)` |
| Diagnostic update (any publish) | `gopls: 3 errors, 0 warnings in 2 files` |
| Diagnostic cleared | `gopls: clean` |
| Server restarted | `gopls restarted` |

All log entries use the same count format on every diagnostic publish — no "first vs subsequent" distinction. Log entries are written via `debuglog.Log.Append(debuglog.Entry{Kind: KindLSP, Message: ...})` from the LSP manager's callbacks — never from the TUI goroutine, per alt-screen safety rules.

---

## Real-Time Reactivity

### Existing: Diagnostic Changes

`lspDiagCh chan struct{}` already signals the TUI on every `publishDiagnostics`. `lspDiagChangedMsg` triggers a sidebar re-render. No change needed.

### New: Server Start Events

Define in **`internal/lsp`** (not TUI — avoids circular import):

```go
// In internal/lsp/manager.go

type ServerStartedEvent struct {
    Cmd    string // e.g. "gopls"
    LangID string // e.g. "go"
    Root   string // project root
}
```

Add to `Manager`:

```go
func (m *Manager) SetEventChan(ch chan ServerStartedEvent)
```

Stored as `m.eventCh chan ServerStartedEvent`. Non-blocking send in `ClientForExt` after `Initialize` succeeds. If `m.eventCh` is nil (headless mode), the send is skipped — no-op.

`SetEventChan` is called **only by the TUI** in `getInitialTools` (after `m.lspMgr` is created). It is never called from `NewManager` itself, since headless callers (runcli, acp, server) have no channel to provide.

In `internal/tui/model.go`:

```go
lspEventCh chan lsp.ServerStartedEvent  // buffered, cap 16
```

The cap of 16 exceeds the maximum number of configured servers (currently 7) to absorb bursts.

**Indexing expiry:** When `lspServerStartedMsg` is handled, schedule a typed expiry:

```go
tea.After(3*time.Second, lspIndexingDoneMsg{cmd: event.Cmd})
```

`lspIndexingDoneMsg` carries the binary name so the handler clears exactly one binary's indexing state without ambiguity. No shared tick is used.

```go
type lspIndexingDoneMsg struct{ cmd string }
```

**TUI listen command** mirrors `listenLSPDiags`:

```go
func listenLSPEvents(ch chan lsp.ServerStartedEvent) tea.Cmd {
    return func() tea.Msg {
        e := <-ch
        return lspServerStartedMsg{event: e}
    }
}
```

Handler for `lspServerStartedMsg` in `Update`:
1. Record `m.lspServerStartTimes[event.Cmd] = time.Now()`
2. Append `KindLSP` log entry: `"gopls started  (go · /path/to/root)"`
3. Return `tea.Batch(listenLSPEvents(m.lspEventCh), tea.After(3*time.Second, lspIndexingDoneMsg{cmd: event.Cmd}))`

Handler for `lspIndexingDoneMsg` in `Update`:
1. Delete `m.lspServerStartTimes[msg.cmd]`
2. Re-render sidebar

---

## Data Layer Changes

### `internal/lsp/manager.go`

Add:

```go
type ServerStatus struct {
    Cmd    string // binary name, e.g. "gopls"
    LangID string // primary language ID, e.g. "go"
    Root   string
}

func (m *Manager) ActiveServers() []ServerStatus
```

Returns one entry **per unique Cmd** (not per extension) for binaries that have at least one running client. Sorted by Cmd. Used by `renderLSPSection` to build the row list.

`DiagCounts` and per-binary diagnostic summing live in the TUI render function, not a new store method — see below.

### `internal/lsp/diagnostics.go`

No new public methods. `renderLSPSection` calls the existing `All()` method and groups by `filepath.Ext(d.Path)`, then maps extensions back to server binaries using `lsp.ServerForExt`. This keeps `DiagnosticStore`'s API surface minimal.

---

## TUI Model Changes

### Prerequisites

Fix `buildSidebarRenderData` receiver from `(m model)` to `(m *model)` so the `sidebarComputeCache` assignment actually persists. This is a pre-existing bug that must be fixed before adding `renderLSPSection` (which calls `ActiveServers()` and `All()` under locks — 6× per frame uncached is too expensive).

### New Fields on `model`

```go
lspEventCh         chan lsp.ServerStartedEvent
lspServerStartTimes map[string]time.Time  // binary cmd -> start time
```

Initialized in `getInitialTools`.

### New Functions

- `renderLSPSection(outerBodyWidth int) []string` — builds sidebar rows; calls `m.lspMgr.ActiveServers()` for the server list, then `m.lspMgr.Diagnostics().All()` and groups counts by binary. Only called when `m.lspMgr != nil`.
- `listenLSPEvents(ch chan lsp.ServerStartedEvent) tea.Cmd` — blocks on channel, returns `lspServerStartedMsg`.
- Handlers for `lspServerStartedMsg` and `lspIndexingDoneMsg` in `Update`.

### Modified Functions

- `buildSidebarRenderData` (**receiver fixed to `*model`**) — calls `renderLSPSection` and appends as a named scroll section when `m.lspMgr != nil` and at least one server is active.
- `buildSidebarRenderData` — the existing `appendScrollSection("Tools", mcpLine + "  |  " + lspLine)` becomes `appendScrollSection("Tools", mcpLine)` only; `renderLSPStatus` is removed.
- `handleLogKeys` — adds `case "5"` toggle for `DebugKindLSP`.
- `renderLogTab` — adds `{DebugKindLSP, "LSP", "5"}` to the kinds list.

---

## Unchanged

- `renderMCPStatus` — unchanged; still used in the Tools section.
- LSP tool definitions (`lsp`, `ast`, `lsp_diagnostics`).
- Diagnostic auto-injection into agent messages (`injectLSPDiagnostics`).
- `/lsp` command handler.

---

## Out of Scope

- Clicking a server row to restart it (future).
- Per-file diagnostic breakdown in the sidebar.
- Configuring which servers appear.
- Tracking real `$/progress` indexing completion (intentionally approximated with 3s timer).

---

## File Touch List

| File | Change |
|---|---|
| `internal/debuglog/debuglog.go` | Add `KindLSP` |
| `internal/tui/debuglog.go` | Export `DebugKindLSP` alias |
| `internal/lsp/manager.go` | Add `ServerStartedEvent`, `ServerStatus`, `ActiveServers()`, `SetEventChan()`, emit on start |
| `internal/tui/model.go` | Fix `buildSidebarRenderData` receiver; new fields, `renderLSPSection`, `listenLSPEvents`, `lspIndexingDoneMsg`, handlers, sidebar/log wiring |
