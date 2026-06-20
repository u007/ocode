# Part 10 — Commands, Picker, /context

## Task 13: /discovery + /discover commands + embedding-model picker

**Files:**
- Modify: `internal/agent/discovery_glue.go` (status/stats accessors + `ResetDiscovery`)
- Modify: `internal/tui/commands.go` (two `commandSpecs` entries + `runDiscoveryCmd`/`runDiscoverCmd`)
- Modify: `internal/tui/model.go` (`isInstantCmd`; `handleDiscoveryCmd`/`handleDiscoverCmd`; `setDiscoveryEnabled`/`setEmbeddingModel`/`showDiscoverStatus`)
- Modify: `internal/tui/picker.go` (`openEmbeddingModelPicker` + 5 conditional sites + routing)
- Test: `internal/agent/discovery_glue_test.go`

**Interfaces:**
- Produces (agent):
  - `type DiscoveryStatusInfo struct { Active bool; Model, Backend string; Attached []string; MCPTotal int; InitErr string }`
  - `func (a *Agent) DiscoveryStatus() DiscoveryStatusInfo`
  - `func (a *Agent) DiscoveryGatedTokens() (attached, total, gatedToks, indexToks int)`
  - `func (a *Agent) ResetDiscovery()`

- [ ] **Step 1: Write the failing test (agent accessors)**

Append to `internal/agent/discovery_glue_test.go`:

```go
func TestDiscoveryStatusAndReset(t *testing.T) {
	a := newGateAgent()
	a.config = &config.Config{}
	a.config.Ocode.Discovery.EmbeddingModel = "openai/text-embedding-3-small"
	a.config.Ocode.Discovery.EmbeddingBackend = "http"
	a.disco = &discoveryState{enabled: true, session: discovery.NewSession(
		discovery.NewEngine(discovery.FakeEmbedder{Dimension: 8}, t.TempDir()))}

	st := a.DiscoveryStatus()
	if !st.Active || st.Model != "openai/text-embedding-3-small" || st.MCPTotal != 2 {
		t.Fatalf("bad status: %+v", st)
	}
	a.ResetDiscovery()
	if a.disco != nil {
		t.Fatal("ResetDiscovery must clear state so it re-inits next turn")
	}
}
```

(Add `"github.com/u007/ocode/internal/config"` to the test imports.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run 'TestDiscoveryStatusAndReset' -v`
Expected: FAIL — `DiscoveryStatus`/`ResetDiscovery` undefined.

- [ ] **Step 3: Add the accessors**

Append to `internal/agent/discovery_glue.go` (add `"encoding/json"` if not already imported):

```go
type DiscoveryStatusInfo struct {
	Active   bool
	Model    string
	Backend  string
	Attached []string
	MCPTotal int
	InitErr  string
}

func (a *Agent) DiscoveryStatus() DiscoveryStatusInfo {
	st := DiscoveryStatusInfo{MCPTotal: len(a.mcpTools)}
	if a.config != nil {
		st.Model = a.config.Ocode.Discovery.EmbeddingModel
		st.Backend = a.config.Ocode.Discovery.EmbeddingBackend
	}
	if a.disco != nil {
		st.Active = a.disco.enabled
		st.InitErr = a.disco.initErr
		if a.disco.session != nil {
			st.Attached = a.disco.session.Attached()
		}
	}
	return st
}

// DiscoveryGatedTokens reports attached/total MCP counts and the estimated tokens
// saved (schemas of unattached MCP tools) vs the name-index cost.
func (a *Agent) DiscoveryGatedTokens() (attached, total, gatedToks, indexToks int) {
	total = len(a.mcpTools)
	indexChars := 0
	for name := range a.mcpTools {
		t, ok := a.tools[name]
		if !ok {
			continue
		}
		indexChars += len(name) + 1
		if a.discoveryAllows(name) {
			attached++
			continue
		}
		if b, err := json.Marshal(t.Definition()); err == nil {
			gatedToks += len(b) / 4
		}
	}
	indexToks = indexChars / 4
	return
}

// ResetDiscovery clears discovery state so it re-initializes on the next turn
// (used after the embedding model changes).
func (a *Agent) ResetDiscovery() { a.disco = nil }
```

- [ ] **Step 4: Register commands + instant + handlers**

In `internal/tui/commands.go` `commandSpecs`, add:

```go
	{name: "/discovery", usage: "/discovery [on|off]", help: "Enable/disable retrieval-based skill/MCP discovery", handler: runDiscoveryCmd},
	{name: "/discover", usage: "/discover [status|model [name]]", help: "Show discovery status / choose the query-embedding model", handler: runDiscoverCmd},
```

Add the handler functions in `commands.go`:

```go
func runDiscoveryCmd(m *model, args []string) tea.Cmd { m.handleDiscoveryCmd(args); return nil }
func runDiscoverCmd(m *model, args []string) tea.Cmd  { return m.handleDiscoverCmd(args) }
```

In `internal/tui/model.go` `isInstantCmd` (~line 5740), extend the chain:

```go
		cmd == "/search" || cmd == "/find" ||
		cmd == "/discovery" || cmd == "/discover"
```

Add the handlers in `model.go` (imports needed: `os`, `github.com/u007/ocode/internal/discovery`; `config` is already imported):

```go
func (m *model) setDiscoveryEnabled(on bool) error {
	if on {
		dc := m.config.Ocode.Discovery
		if _, err := discovery.ResolveEmbedder(dc.EmbeddingBackend, dc.EmbeddingModel, os.Getenv); err != nil {
			return err
		}
	}
	if err := config.SaveDiscoveryEnabled(on); err != nil {
		return err
	}
	m.config.Ocode.Discovery.Enabled = on
	if m.agent != nil {
		m.agent.ResetDiscovery()
	}
	return nil
}

func (m *model) handleDiscoveryCmd(args []string) {
	if len(args) == 0 {
		status := "off"
		if m.config.Ocode.Discovery.Enabled {
			status = "on"
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: "Discovery is " + status + "."})
		return
	}
	switch strings.ToLower(args[0]) {
	case "on", "true", "yes":
		if err := m.setDiscoveryEnabled(true); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Cannot enable discovery: " + err.Error()})
			return
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: "Discovery: enabled"})
	case "off", "false", "no":
		if err := m.setDiscoveryEnabled(false); err != nil {
			m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
			return
		}
		m.messages = append(m.messages, message{role: roleAssistant, text: "Discovery: disabled"})
	default:
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /discovery [on|off]"})
	}
}

func (m *model) setEmbeddingModel(id string) error {
	backend := "http"
	if id == "local" || strings.HasPrefix(id, "local/") {
		backend = "local"
	}
	if err := config.SaveQueryEmbeddingModel(id, backend); err != nil {
		return err
	}
	m.config.Ocode.Discovery.EmbeddingModel = id
	m.config.Ocode.Discovery.EmbeddingBackend = backend
	if m.agent != nil {
		m.agent.ResetDiscovery()
	}
	return nil
}

func (m *model) showDiscoverStatus() {
	var b strings.Builder
	dc := m.config.Ocode.Discovery
	b.WriteString("Discovery\n")
	onoff := "off"
	if dc.Enabled {
		onoff = "on"
	}
	fmt.Fprintf(&b, "  status:  %s\n", onoff)
	fmt.Fprintf(&b, "  backend: %s\n", dc.EmbeddingBackend)
	model := dc.EmbeddingModel
	if model == "" {
		model = "(none — run /discover model)"
	}
	fmt.Fprintf(&b, "  model:   %s\n", model)
	if dc.EmbeddingBackend == "local" {
		fmt.Fprintf(&b, "  local:   %s\n", dc.LocalModelStatus)
	}
	if m.agent != nil {
		st := m.agent.DiscoveryStatus()
		if !st.Active && st.InitErr != "" {
			fmt.Fprintf(&b, "  note:    fail-open (%s)\n", st.InitErr)
		}
		fmt.Fprintf(&b, "  attached this session (%d/%d MCP tools):\n", len(st.Attached), st.MCPTotal)
		for _, id := range st.Attached { // already sorted
			fmt.Fprintf(&b, "    - %s\n", id)
		}
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
}

func (m *model) handleDiscoverCmd(args []string) tea.Cmd {
	if len(args) == 0 || strings.ToLower(args[0]) == "status" {
		m.showDiscoverStatus()
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "model":
		if len(args) > 1 {
			if err := m.setEmbeddingModel(args[1]); err != nil {
				m.messages = append(m.messages, message{role: roleAssistant, text: "Error: " + err.Error()})
				return nil
			}
			m.messages = append(m.messages, message{role: roleAssistant, text: "Embedding model: " + args[1]})
			return nil
		}
		m.openEmbeddingModelPicker()
		return nil
	default:
		m.messages = append(m.messages, message{role: roleAssistant, text: "Usage: /discover [status|model [name]]"})
		return nil
	}
}
```

- [ ] **Step 5: Add the picker kind**

In `internal/tui/picker.go`, add the opener:

```go
func (m *model) openEmbeddingModelPicker() {
	m.input.Blur()
	var items, values []string
	var isHeader []bool
	appendH := func(l string) { items = append(items, l); values = append(values, ""); isHeader = append(isHeader, true) }
	appendM := func(l, v string) { items = append(items, l); values = append(values, v); isHeader = append(isHeader, false) }

	appendH("HTTP embedding models")
	for _, em := range discovery.HTTPModels { // sorted in the registry
		appendM("  "+em.ID, em.ID)
	}
	appendH("Local (downloaded on first use)")
	appendM("  local/lfm2-5-retriever", "local/lfm2-5-retriever")

	m.pickerKind = "embedding-model"
	m.pickerItems = items
	m.pickerValues = values
	m.pickerIsHeader = isHeader
	m.pickerIndex = 0
	m.pickerFilter = ""
	m.pickerFilterPending = ""
	m.showPicker = true
}
```

(Add `"github.com/u007/ocode/internal/discovery"` to `picker.go` imports.)

Add `"embedding-model"` to each of the **five** `pickerKind` conditional sites that
currently list `"redaction-model"` (lines ~488, ~578, ~594, ~649 hint, and the title
block ~684). For the four `isFiltered` checks, extend the OR-chain, e.g.:

```go
	isFiltered := (m.pickerKind == "model" || m.pickerKind == "advisor" || m.pickerKind == "permission-model" || m.pickerKind == "small-model" || m.pickerKind == "redaction-model" || m.pickerKind == "embedding-model") && m.pickerFilter != ""
```

In `renderPicker`, add the hint to the first conditional and a title:

```go
	if m.pickerKind == "embedding-model" {
		title = "Select query-embedding model"
	}
```

In `selectPickerIndex`, add the routing case (next to the `redaction-model` case):

```go
	if kind == "embedding-model" {
		return m.handleCommand("/discover model " + selected)
	}
```

- [ ] **Step 6: Run tests + build**

Run: `go test ./internal/agent/ -run 'TestDiscoveryStatusAndReset' -v`
Expected: PASS.
Run: `go build ./...`
Expected: success.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/discovery_glue.go internal/agent/discovery_glue_test.go internal/tui/commands.go internal/tui/model.go internal/tui/picker.go
git commit -m "feat(tui): /discovery + /discover commands + embedding-model picker"
```

---

## Task 14: /context Discovery section

**Files:**
- Modify: `internal/tui/model.go` — `handleContextCmd` (append a Discovery section before the final output, ~line 8232)

**Interfaces:**
- Consumes: `Agent.DiscoveryStatus`, `Agent.DiscoveryGatedTokens`, `estimateTok`, `formatTok`.

- [ ] **Step 1: Add the section**

In `handleContextCmd`, just before the final `m.messages = append(...)` that emits
the built string, add:

```go
	if m.config != nil && m.config.Ocode.Discovery.Enabled && m.agent != nil {
		st := m.agent.DiscoveryStatus()
		attached, total, gatedToks, indexToks := m.agent.DiscoveryGatedTokens()
		b.WriteString("\nDiscovery\n")
		fmt.Fprintf(&b, "  %-28s %s %s\n", "Backend/model", st.Backend, st.Model)
		if !st.Active && st.InitErr != "" {
			fmt.Fprintf(&b, "  %-28s fail-open: %s\n", "Status", st.InitErr)
		}
		fmt.Fprintf(&b, "  %-28s %d/%d\n", "MCP tools attached", attached, total)
		const queryEmbedToks = 64 // rough per-turn query embedding cost
		net := gatedToks - indexToks - queryEmbedToks
		if net < 0 {
			net = 0
		}
		fmt.Fprintf(&b, "  %-28s ~%s tok\n", "Context saved (gross)", formatTok(gatedToks))
		fmt.Fprintf(&b, "  %-28s ~%s tok\n", "Context saved (net)", formatTok(net))
		fmt.Fprintf(&b, "  %-28s %d\n", "MCP tools not attached", total-attached)
	}
```

- [ ] **Step 2: Build + manual verification**

Run: `go build ./...`
Expected: success.

Manual check (documented, not automated — `/context` output is TUI-rendered):
1. With an `OPENAI_API_KEY` set and at least one MCP server configured, run ocode,
   then `/discover model openai/text-embedding-3-small`, then `/discovery on`.
2. Send a task message, then `/context`. Expect a **Discovery** section showing
   `MCP tools attached X/Y` with X < Y and a non-zero gross "Context saved".
3. `/discover` shows the attached list; the Log tab shows `DISCOVERY` lines.

- [ ] **Step 3: Run the full suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(tui): /context Discovery section with net savings"
```
