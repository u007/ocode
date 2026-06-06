# LSP Status Sidebar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show per-server LSP status (indexing / clean / N errors) in a dedicated TUI sidebar section and add a KindLSP filter to the log tab, with real-time updates on server start and diagnostic changes.

**Architecture:** Add `ServerStartedEvent` and `ActiveServers()` to `internal/lsp/manager.go`; wire a typed event channel from the LSP manager into the TUI; render a grouped-by-binary sidebar section from `Active Servers()` + `DiagnosticStore.All()`; add `KindLSP` to the debuglog and a `[6]LSP` filter toggle in the log tab.

**Tech Stack:** Go, Bubble Tea v2 (`charm.land/bubbletea/v2`), lipgloss v2 (`charm.land/lipgloss/v2`), `internal/lsp`, `internal/debuglog`, `internal/tui`

---

## Web UI scope

The web UI (`web/`) is **not** covered by this plan. `CoworkSidebar.tsx` shows `"lsp"` as a static tool-name badge only. LSP status in the web UI is a separate future task.

---

## File Map

| File | Role |
|---|---|
| `internal/debuglog/debuglog.go` | Add `KindLSP` constant |
| `internal/tui/debuglog.go` | Export `DebugKindLSP` alias |
| `internal/lsp/manager.go` | Add `ServerStartedEvent`, `ServerStatus`, `ActiveServers()`, `SetEventChan()` |
| `internal/tui/model.go` | New fields, message types, handlers, `renderLSPSection`, sidebar/log wiring |

---

## Task 1: Add KindLSP to debuglog

**Files:**
- Modify: `internal/debuglog/debuglog.go`
- Modify: `internal/tui/debuglog.go`

- [ ] **Add `KindLSP` constant to `internal/debuglog/debuglog.go`**

  The existing constants block is:
  ```go
  const (
      KindLLM     EntryKind = "LLM"
      KindTool    EntryKind = "TOOL"
      KindAgent   EntryKind = "AGENT"
      KindError   EntryKind = "ERROR"
      KindSession EntryKind = "SESSION"
      KindGit     EntryKind = "GIT"
  )
  ```
  Add one line:
  ```go
  const (
      KindLLM     EntryKind = "LLM"
      KindTool    EntryKind = "TOOL"
      KindAgent   EntryKind = "AGENT"
      KindError   EntryKind = "ERROR"
      KindSession EntryKind = "SESSION"
      KindGit     EntryKind = "GIT"
      KindLSP     EntryKind = "LSP"
  )
  ```

- [ ] **Export alias in `internal/tui/debuglog.go`**

  Current file ends at:
  ```go
  DebugKindGit     = debuglog.KindGit
  ```
  Add:
  ```go
  DebugKindLSP     = debuglog.KindLSP
  ```

- [ ] **Commit**
  ```bash
  git add internal/debuglog/debuglog.go internal/tui/debuglog.go
  git commit -m "feat(lsp): add KindLSP debuglog entry kind"
  ```

---

## Task 2: LSP manager — event types and ActiveServers()

**Files:**
- Modify: `internal/lsp/manager.go`
- Modify: `internal/lsp/manager_test.go` (or create if it doesn't exist — check with `ls internal/lsp/`)

- [ ] **Write failing test for `ActiveServers()`**

  In `internal/lsp/manager_test.go` (create if absent):
  ```go
  func TestActiveServersEmpty(t *testing.T) {
      m := NewManager(".")
      defer m.Close()
      got := m.ActiveServers()
      if len(got) != 0 {
          t.Fatalf("expected 0 servers, got %d", len(got))
      }
  }
  ```

- [ ] **Run test — expect FAIL (method not defined)**
  ```bash
  cd /Users/james/www/ocode && go test ./internal/lsp/ -run TestActiveServersEmpty -v
  ```

- [ ] **Add types and `ActiveServers()` to `internal/lsp/manager.go`**

  Add after the `serversByExt` map (before `Manager` struct):
  ```go
  // ServerStartedEvent is sent on the event channel when a language server
  // successfully completes its LSP initialize handshake.
  type ServerStartedEvent struct {
      Cmd    string // binary name, e.g. "gopls"
      LangID string // primary language ID, e.g. "go"
      Root   string // project root path
  }

  // ServerStatus describes a running language server.
  type ServerStatus struct {
      Cmd    string // binary name, e.g. "gopls"
      LangID string // primary language ID
  }
  ```

  Add to `Manager` struct (after `diagnostics *DiagnosticStore`):
  ```go
  eventCh chan ServerStartedEvent // optional; nil in headless mode
  ```

  Add method after `KnownServers()`:
  ```go
  // ActiveServers returns one ServerStatus per unique binary that has a
  // running (non-closed) client. Multiple extensions mapping to the same
  // binary (e.g. .ts/.tsx/.js/.jsx → typescript-language-server) produce
  // one entry. Results are sorted by Cmd.
  func (m *Manager) ActiveServers() []ServerStatus {
      m.mu.Lock()
      defer m.mu.Unlock()
      seen := make(map[string]ServerStatus)
      for ext, c := range m.clients {
          if c == nil {
              continue
          }
          spec := serversByExt[ext]
          if _, ok := seen[spec.cmd]; !ok {
              seen[spec.cmd] = ServerStatus{Cmd: spec.cmd, LangID: spec.langID}
          }
      }
      out := make([]ServerStatus, 0, len(seen))
      for _, s := range seen {
          out = append(out, s)
      }
      sort.Slice(out, func(i, j int) bool { return out[i].Cmd < out[j].Cmd })
      return out
  }
  ```

- [ ] **Run test — expect PASS**
  ```bash
  go test ./internal/lsp/ -run TestActiveServersEmpty -v
  ```

- [ ] **Commit**
  ```bash
  git add internal/lsp/manager.go internal/lsp/manager_test.go
  git commit -m "feat(lsp): add ServerStartedEvent, ServerStatus, ActiveServers()"
  ```

---

## Task 3: LSP manager — SetEventChan() and emit on start

**Files:**
- Modify: `internal/lsp/manager.go`
- Modify: `internal/lsp/manager_test.go`

- [ ] **Write failing test for event emission**

  ```go
  func TestSetEventChanReceivesStartEvent(t *testing.T) {
      // We can't actually start a real server in a unit test, but we can verify
      // that SetEventChan stores the channel and that a non-blocking send on a
      // nil channel is a no-op (doesn't panic).
      m := NewManager(".")
      defer m.Close()

      ch := make(chan ServerStartedEvent, 4)
      m.SetEventChan(ch)

      // Internal: simulate what ClientForExt does after Initialize.
      // Call the unexported emitServerStarted helper if we add one, or
      // verify the channel is stored.
      if m.eventCh != ch {
          t.Fatal("event channel not stored")
      }
  }
  ```

- [ ] **Run test — expect FAIL**
  ```bash
  go test ./internal/lsp/ -run TestSetEventChanReceivesStartEvent -v
  ```

- [ ] **Add `SetEventChan()` and non-blocking send to `internal/lsp/manager.go`**

  Add method:
  ```go
  // SetEventChan registers a channel to receive ServerStartedEvent when a
  // language server successfully initialises. Call only from the TUI layer
  // after the Manager has been constructed. Headless callers (runcli, acp,
  // server) never call this; Manager treats a nil channel as a no-op.
  func (m *Manager) SetEventChan(ch chan ServerStartedEvent) {
      m.mu.Lock()
      m.eventCh = ch
      m.mu.Unlock()
  }
  ```

  In `ClientForExt`, after `m.clients[ext] = c` and before `return c, nil`, add:
  ```go
  // Notify the TUI (if wired) that a new server started. Non-blocking:
  // a full channel is silently dropped — the TUI will pick up state on
  // the next diagnostic event anyway.
  if m.eventCh != nil {
      evt := ServerStartedEvent{Cmd: spec.cmd, LangID: spec.langID, Root: m.root}
      select {
      case m.eventCh <- evt:
      default:
      }
  }
  ```

  Note: the `select/default` must be outside `m.mu.Lock()` to avoid deadlock (the channel send may block if the receiver goroutine tries to call a Manager method that also needs the lock). Move the send after `m.mu.Unlock()`. Restructure the end of `ClientForExt`:
  ```go
      m.clients[ext] = c
      // Read eventCh while holding the lock, then send outside the lock.
      eventCh := m.eventCh
      m.mu.Unlock()
      if eventCh != nil {
          evt := ServerStartedEvent{Cmd: spec.cmd, LangID: spec.langID, Root: m.root}
          select {
          case eventCh <- evt:
          default:
          }
      }
      return c, nil
  ```
  Make sure the existing `defer m.mu.Unlock()` at the top of `ClientForExt` is removed and replaced with explicit lock/unlock since we need to unlock before the send. Check the current lock pattern in `ClientForExt` carefully before editing.

- [ ] **Run test — expect PASS**
  ```bash
  go test ./internal/lsp/ -run TestSetEventChanReceivesStartEvent -v
  ```

- [ ] **Run full lsp package tests**
  ```bash
  go test ./internal/lsp/ -v
  ```

- [ ] **Commit**
  ```bash
  git add internal/lsp/manager.go internal/lsp/manager_test.go
  git commit -m "feat(lsp): SetEventChan, emit ServerStartedEvent after Initialize"
  ```

---

## Task 4: TUI — message types, model fields, event listener

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Add message types near the other `lsp*Msg` types (around line 293)**

  After `type lspDiagChangedMsg struct{}`, add:
  ```go
  // lspServerStartedMsg is delivered when a language server completes its
  // initialize handshake. The TUI uses it to start the 3s indexing timer
  // and emit a KindLSP log entry.
  type lspServerStartedMsg struct{ event lsp.ServerStartedEvent }

  // lspIndexingDoneMsg fires 3 seconds after a server starts to clear the
  // "indexing…" sidebar state for that binary.
  type lspIndexingDoneMsg struct{ cmd string }
  ```

- [ ] **Add fields to `model` struct (near `lspDiagCh`, around line 743)**

  After `lspDiagCh chan struct{}`, add:
  ```go
  lspEventCh         chan lsp.ServerStartedEvent
  lspServerStartTimes map[string]time.Time // binary cmd -> start time; present = indexing
  lspStateSeq        uint64               // bumped on any LSP state change; invalidates sidebar cache
  ```

- [ ] **Add `lspStateSeq` to `sidebarCacheKey` (around line 311)**

  Current struct:
  ```go
  type sidebarCacheKey struct {
      msgCount int
      lastLen  int
      model    string
  }
  ```
  Change to:
  ```go
  type sidebarCacheKey struct {
      msgCount   int
      lastLen    int
      model      string
      lspStateSeq uint64
  }
  ```

- [ ] **Update `sidebarCacheKey` construction in `buildSidebarRenderData` to include `lspStateSeq`**

  Find where `cacheKey` is built (the line that sets `msgCount`, `lastLen`, `model`) and add:
  ```go
  lspStateSeq: m.lspStateSeq,
  ```

- [ ] **Add `listenLSPEvents` function near `listenLSPDiags` (around line 8694)**

  ```go
  // listenLSPEvents blocks on the LSP server-start event channel and
  // re-arms itself so subsequent starts are also caught.
  func listenLSPEvents(ch chan lsp.ServerStartedEvent) tea.Cmd {
      return func() tea.Msg {
          e := <-ch
          return lspServerStartedMsg{event: e}
      }
  }

  // lspIndexingTimer returns a Cmd that fires lspIndexingDoneMsg after 3 s.
  func lspIndexingTimer(cmd string) tea.Cmd {
      return func() tea.Msg {
          time.Sleep(3 * time.Second)
          return lspIndexingDoneMsg{cmd: cmd}
      }
  }
  ```

- [ ] **Wire `lspEventCh` in `getInitialTools` (around line 1122)**

  After `m.lspMgr.Diagnostics().SetNotifyChan(m.lspDiagCh)`, add:
  ```go
  if m.lspEventCh == nil {
      m.lspEventCh = make(chan lsp.ServerStartedEvent, 16)
  }
  m.lspMgr.SetEventChan(m.lspEventCh)
  ```

- [ ] **Start `listenLSPEvents` in `Init()` (around line 1490)**

  After `cmds = append(cmds, listenLSPDiags(m.lspDiagCh))`, add:
  ```go
  if m.lspEventCh != nil {
      cmds = append(cmds, listenLSPEvents(m.lspEventCh))
  }
  ```

- [ ] **Handle `lspServerStartedMsg` in `Update()` (near the `lspDiagChangedMsg` case, around line 2405)**

  ```go
  case lspServerStartedMsg:
      if m.lspServerStartTimes == nil {
          m.lspServerStartTimes = make(map[string]time.Time)
      }
      m.lspServerStartTimes[msg.event.Cmd] = time.Now()
      m.lspStateSeq++
      debuglog.Log.Append(debuglog.Entry{
          Kind:    debuglog.KindLSP,
          Message: fmt.Sprintf("%s started  (%s · %s)", msg.event.Cmd, msg.event.LangID, msg.event.Root),
      })
      cmds := []tea.Cmd{listenLSPEvents(m.lspEventCh), lspIndexingTimer(msg.event.Cmd)}
      return m, tea.Batch(cmds...)
  case lspIndexingDoneMsg:
      if m.lspServerStartTimes != nil {
          delete(m.lspServerStartTimes, msg.cmd)
      }
      m.lspStateSeq++
      return m, nil
  ```

  Also bump `lspStateSeq` in the existing `lspDiagChangedMsg` case:
  ```go
  case lspDiagChangedMsg:
      m.lspStateSeq++
      // ... existing re-arm ...
  ```

- [ ] **Build to verify no compile errors**
  ```bash
  cd /Users/james/www/ocode && go build ./...
  ```

- [ ] **Commit**
  ```bash
  git add internal/tui/model.go
  git commit -m "feat(lsp-tui): event channel, message types, lspStateSeq cache key"
  ```

---

## Task 5: LSP log entries on diagnostic updates

**Files:**
- Modify: `internal/tui/model.go`

The `lspDiagChangedMsg` handler already re-renders the sidebar. We only need to append a log entry. Find the `case lspDiagChangedMsg:` block and add the log append before the re-arm:

- [ ] **Add log entry in `lspDiagChangedMsg` handler**

  ```go
  case lspDiagChangedMsg:
      m.lspStateSeq++
      if m.lspMgr != nil {
          store := m.lspMgr.Diagnostics()
          if store != nil {
              snap := store.Snapshot(0) // counts only, no FirstN needed
              for _, srv := range m.lspMgr.ActiveServers() {
                  // Count errors+warnings for this binary's extensions.
                  errs, warns := 0, 0
                  for _, d := range store.All() {
                      ext := filepath.Ext(d.Path)
                      cmd, _, ok := lsp.ServerForExt(ext)
                      if !ok || cmd != srv.Cmd {
                          continue
                      }
                      switch d.Severity {
                      case lsp.SeverityError:
                          errs++
                      case lsp.SeverityWarning:
                          warns++
                      }
                  }
                  var msg string
                  if errs == 0 && warns == 0 {
                      msg = srv.Cmd + ": clean"
                  } else {
                      msg = fmt.Sprintf("%s: %d errors, %d warnings in %d files",
                          srv.Cmd, errs, warns, snap.Files)
                  }
                  debuglog.Log.Append(debuglog.Entry{Kind: debuglog.KindLSP, Message: msg})
              }
          }
      }
      if m.lspDiagCh != nil {
          return m, listenLSPDiags(m.lspDiagCh)
      }
      return m, nil
  ```

  Note: `snap.Files` counts total files across all servers. For a per-binary file count you'd need to group by ext — that's low value for a log entry. The `N files` figure is acceptable as a total approximation.

  Add required imports if not already present:
  - `"path/filepath"` — already imported
  - `"github.com/u007/ocode/internal/lsp"` — already imported
  - `"github.com/u007/ocode/internal/debuglog"` — check; add if absent

- [ ] **Build**
  ```bash
  go build ./...
  ```

- [ ] **Commit**
  ```bash
  git add internal/tui/model.go
  git commit -m "feat(lsp-tui): emit KindLSP log entries on diagnostic updates"
  ```

---

## Task 6: renderLSPSection and sidebar wiring

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Write `renderLSPSection` as a value-receiver method**

  Add near `renderLSPStatus` (around line 11374):

  ```go
  // renderLSPSection produces sidebar rows for the LSP section. Returns nil
  // when no servers are active (caller omits the section header).
  func (m model) renderLSPSection(outerBodyWidth int) []string {
      if m.lspMgr == nil {
          return nil
      }
      servers := m.lspMgr.ActiveServers()
      if len(servers) == 0 {
          return nil
      }

      // Group diagnostics by binary cmd.
      type diagCounts struct{ errors, warnings int }
      byCmd := make(map[string]diagCounts)
      if diags := m.lspMgr.Diagnostics(); diags != nil {
          for _, d := range diags.All() {
              ext := filepath.Ext(d.Path)
              cmd, _, ok := lsp.ServerForExt(ext)
              if !ok {
                  continue
              }
              c := byCmd[cmd]
              switch d.Severity {
              case lsp.SeverityError:
                  c.errors++
              case lsp.SeverityWarning:
                  c.warnings++
              }
              byCmd[cmd] = c
          }
      }

      errStyle  := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
      warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E0AF68"))
      okStyle   := lipgloss.NewStyle().Foreground(lipgloss.Color("#9ECE6A"))
      dimStyle  := sidebarTextStyle

      seen := make(map[string]bool)
      var rows []string
      for _, srv := range servers {
          if seen[srv.Cmd] {
              continue
          }
          seen[srv.Cmd] = true

          c := byCmd[srv.Cmd]
          var sym, label string
          var symStyle lipgloss.Style

          _, isIndexing := m.lspServerStartTimes[srv.Cmd]
          switch {
          case isIndexing:
              sym, symStyle, label = "◌", dimStyle, "indexing…"
          case c.errors > 0 && c.warnings > 0:
              sym, symStyle = "●", errStyle
              label = fmt.Sprintf("%d errors, %d warnings", c.errors, c.warnings)
          case c.errors > 0:
              sym, symStyle = "●", errStyle
              if c.errors == 1 {
                  label = "1 error"
              } else {
                  label = fmt.Sprintf("%d errors", c.errors)
              }
          case c.warnings > 0:
              sym, symStyle = "△", warnStyle
              if c.warnings == 1 {
                  label = "1 warning"
              } else {
                  label = fmt.Sprintf("%d warnings", c.warnings)
              }
          default:
              sym, symStyle, label = "✓", okStyle, "clean"
          }

          nameW := 14
          name := srv.Cmd
          if len(name) > nameW {
              name = name[:nameW]
          }
          row := dimStyle.Render(fmt.Sprintf("  %-*s", nameW, name)) +
              " " + symStyle.Render(sym) +
              " " + dimStyle.Render(label)
          rows = append(rows, row)
      }
      return rows
  }
  ```

- [ ] **Wire `renderLSPSection` into `buildSidebarRenderData`**

  Find the existing combined Tools line:
  ```go
  mcpLine := "MCP: " + m.renderMCPStatus()
  lspLine := "LSP: " + m.renderLSPStatus()
  appendScrollSection("Tools", []string{sidebarTextStyle.Render(mcpLine + "  |  " + lspLine)}, nil)
  ```

  Replace with:
  ```go
  mcpLine := "MCP: " + m.renderMCPStatus()
  appendScrollSection("Tools", []string{sidebarTextStyle.Render(mcpLine)}, nil)

  if lspRows := m.renderLSPSection(outerBodyWidth); len(lspRows) > 0 {
      appendScrollSection("LSP", lspRows, nil)
  }
  ```

  `renderLSPStatus` is now unused — remove its definition (around line 11374 in the original). Check with `grep -n renderLSPStatus` first; if it has no other callers, delete it.

- [ ] **Build**
  ```bash
  go build ./...
  ```

- [ ] **Run existing TUI tests**
  ```bash
  go test ./internal/tui/ -v -timeout 60s 2>&1 | tail -30
  ```

- [ ] **Commit**
  ```bash
  git add internal/tui/model.go
  git commit -m "feat(lsp-tui): renderLSPSection, dedicated LSP sidebar section"
  ```

---

## Task 7: Log tab KindLSP filter [6]

**Files:**
- Modify: `internal/tui/model.go`

Note: key `"5"` is already taken by GIT. LSP uses `"6"`.

- [ ] **Add `DebugKindLSP` to `toggleLogKind` initialisation map**

  Find `toggleLogKind` (around line 3658). The nil-guard block:
  ```go
  m.logKindFilter = map[DebugEntryKind]bool{
      DebugKindLLM:   true,
      DebugKindTool:  true,
      DebugKindAgent: true,
      DebugKindError: true,
      DebugKindGit:   true,
  }
  ```
  Add:
  ```go
  DebugKindLSP:   true,
  ```

- [ ] **Add `case "6"` to `handleLogKeys`**

  After `case "5": m.toggleLogKind(DebugKindGit)`, add:
  ```go
  case "6":
      m.toggleLogKind(DebugKindLSP)
  ```

- [ ] **Add `[6]LSP` to the kinds list in `renderLogTab`**

  Find:
  ```go
  kinds := []struct {
      kind  DebugEntryKind
      label string
      key   string
  }{
      {DebugKindLLM, "LLM", "1"},
      {DebugKindTool, "TOOL", "2"},
      {DebugKindAgent, "AGENT", "3"},
      {DebugKindError, "ERROR", "4"},
  }
  ```
  Add:
  ```go
  {DebugKindLSP, "LSP", "6"},
  ```

- [ ] **Build**
  ```bash
  go build ./...
  ```

- [ ] **Run tests**
  ```bash
  go test ./internal/tui/ ./internal/lsp/ ./internal/debuglog/ -timeout 60s 2>&1 | tail -20
  ```

- [ ] **Commit**
  ```bash
  git add internal/tui/model.go
  git commit -m "feat(lsp-tui): [6]LSP filter toggle in log tab"
  ```

---

## Self-Review Checklist

**Spec coverage:**
- [x] Dedicated LSP sidebar section — Task 6
- [x] Grouped by binary, not extension — Task 6 (`seen` map dedup)
- [x] Section absent when no servers active — Task 6 (nil guard on `renderLSPSection`)
- [x] States: indexing / clean / errors / warnings / mixed — Task 6
- [x] 3s indexing timer per binary with typed message — Task 4 (`lspIndexingTimer`, `lspIndexingDoneMsg{cmd}`)
- [x] MCP stays in Tools section; LSP gets its own section — Task 6
- [x] `KindLSP` in debuglog — Task 1
- [x] `DebugKindLSP` alias — Task 1
- [x] `[6]LSP` log filter (not 5 — GIT owns 5) — Task 7
- [x] Log events: server started — Task 4 handler
- [x] Log events: diagnostic counts — Task 5
- [x] `ServerStartedEvent` in `internal/lsp` (no circular import) — Task 2
- [x] `SetEventChan` post-construction, nil = no-op — Task 3
- [x] Channel cap 16 (> 7 configured servers) — Task 4
- [x] `lspStateSeq` invalidates sidebar cache on LSP state changes — Task 4
- [x] `$/progress` out of scope — documented in spec

**Not covered (out of scope per spec):**
- Web UI LSP status
- Click-to-restart server row
- Per-file diagnostic breakdown in sidebar
- Real `$/progress` tracking
