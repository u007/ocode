# /context Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `/context` slash command that shows a token budget inspector — every source contributing tokens to the base prompt, MCP tool schemas grouped by server, available skills (on-demand, not pre-injected), and live session token usage.

**Architecture:** Two files change. `commands.go` registers the command spec and a thin `runContextCmd` wrapper. All logic lives in a new `handleContextCmd` method in `model.go` plus a small package-level `estimateTok` helper. Output is appended as a display-only `message` (no `raw` field) so it never reaches the LLM.

**Tech Stack:** Go, BubbleTea TUI, `internal/skill`, `internal/agent`, `internal/plugins`, `encoding/json`

---

### Task 1: Add `estimateTok` helper and unit test

Token estimation is `len(s) / 4`. Extracting it keeps the main handler readable and makes it independently testable.

**Files:**
- Modify: `internal/tui/model.go` (add helper near top of file, after imports)
- Modify: `internal/tui/command_test.go` (add test)

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/command_test.go`:

```go
func TestEstimateTok(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"abcd", 1},
		{"abcde", 1},
		{"abcdefgh", 2},
	}
	for _, c := range cases {
		if got := estimateTok(c.input); got != c.want {
			t.Errorf("estimateTok(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tui/... -run TestEstimateTok -v 2>&1 | grep -E "FAIL|PASS|undefined"
```

Expected: `undefined: estimateTok`

- [ ] **Step 3: Add the helper to `model.go`**

Find the block of package-level helpers near the top of `internal/tui/model.go` (after the `var` declarations, before `func init` or the first function). Add:

```go
// estimateTok approximates token count as len(s)/4.
func estimateTok(s string) int {
	return len(s) / 4
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/tui/... -run TestEstimateTok -v 2>&1 | grep -E "FAIL|PASS"
```

Expected: `PASS`

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/command_test.go
git commit -m "feat: add estimateTok helper for /context token estimation"
```

---

### Task 2: Register `/context` in the command system

Wire up the command entry and a thin dispatcher in `commands.go`. No logic here — handler lives in `model.go`.

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/command_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/command_test.go`:

```go
func TestContextCommandIsRegistered(t *testing.T) {
	spec := lookupCommand("/context")
	if spec == nil {
		t.Fatal("expected /context to be registered")
	}
	if spec.help == "" {
		t.Fatal("expected /context to have help text")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tui/... -run TestContextCommandIsRegistered -v 2>&1 | grep -E "FAIL|PASS"
```

Expected: `FAIL` — `/context` not registered

- [ ] **Step 3: Add command spec and dispatcher to `commands.go`**

In `internal/tui/commands.go`, add to the `commandSpecs` slice (after `/compact`, before `/undo` is a natural spot):

```go
{name: "/context", help: "Show context window token budget", handler: runContextCmd},
```

At the bottom of `commands.go`, add the dispatcher:

```go
func runContextCmd(m *model, args []string) tea.Cmd {
	m.handleContextCmd(args)
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/tui/... -run TestContextCommandIsRegistered -v 2>&1 | grep -E "FAIL|PASS|undefined"
```

Expected: `undefined: handleContextCmd` (command registered, handler missing — expected at this stage)

- [ ] **Step 5: Commit**

```bash
git add internal/tui/commands.go internal/tui/command_test.go
git commit -m "feat: register /context slash command"
```

---

### Task 3: Implement `handleContextCmd` — base prompt section

Build the handler skeleton and populate the Base Prompt section (mode system prompt + ambient files + plugin instructions).

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Write failing test for nil-agent guard**

Add to `internal/tui/command_test.go`:

```go
func TestContextCommandNilAgentGuard(t *testing.T) {
	m := model{width: 80}
	// must not panic
	m.handleContextCmd(nil)
	if len(m.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m.messages))
	}
	if !strings.Contains(m.messages[0].text, "No agent") {
		t.Fatalf("expected no-agent message, got %q", m.messages[0].text)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tui/... -run TestContextCommandNilAgentGuard -v 2>&1 | grep -E "FAIL|PASS|undefined"
```

Expected: `undefined: handleContextCmd`

- [ ] **Step 3: Add `handleContextCmd` to `model.go`**

Find the `handleSkillsCmd` method in `internal/tui/model.go` (around line 2875). Add the new method nearby:

```go
func (m *model) handleContextCmd(args []string) {
	if m.agent == nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "No agent configured."})
		return
	}

	var b strings.Builder
	b.WriteString("Context Budget\n")
	b.WriteString(strings.Repeat("═", 38) + "\n")

	// ── Base Prompt ──────────────────────────────
	b.WriteString("\nBase Prompt\n")
	baseTotal := 0

	// Mode system prompt
	modePrompt := m.agent.Mode().SystemPrompt()
	modeTok := estimateTok(modePrompt)
	baseTotal += modeTok
	fmt.Fprintf(&b, "  Mode (%s)%s~%s tok\n",
		m.agent.Mode(),
		columnPad("Mode ("+m.agent.Mode().String()+")", 28),
		formatTok(modeTok))

	// Ambient files
	ambientFiles := []string{"AGENTS.md", "CLAUDE.md", ".cursorrules"}
	rulesDir := filepath.Join(".opencode", "rules")
	if entries, err := os.ReadDir(rulesDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
				ambientFiles = append(ambientFiles, filepath.Join(rulesDir, e.Name()))
			}
		}
	}
	anyAmbient := false
	for _, f := range ambientFiles {
		content, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		anyAmbient = true
		tok := estimateTok(string(content))
		baseTotal += tok
		label := filepath.Base(f)
		fmt.Fprintf(&b, "  %-28s ~%s tok\n", label, formatTok(tok))
	}
	if !anyAmbient {
		b.WriteString("  (no ambient files found)\n")
	}

	// Plugin instructions
	plugs := plugins.LoadPlugins()
	for _, p := range plugs {
		if p.Instructions == "" {
			continue
		}
		tok := estimateTok(p.Instructions)
		baseTotal += tok
		fmt.Fprintf(&b, "  Plugin: %-20s ~%s tok\n", p.Name, formatTok(tok))
	}

	fmt.Fprintf(&b, "  %-28s ~%s tok\n", "Subtotal", formatTok(baseTotal))

	m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
}
```

Also add two small helpers at package level in `model.go` (near `estimateTok`):

```go
// formatTok formats an integer token count with comma separators.
func formatTok(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return strconv.Itoa(n)
}

// columnPad returns spaces to pad label to width w for alignment.
func columnPad(label string, w int) string {
	pad := w - len(label)
	if pad < 1 {
		pad = 1
	}
	return strings.Repeat(" ", pad)
}
```

Ensure these imports are present in `model.go` (most are already there):
- `"os"`
- `"path/filepath"`
- `"strconv"`
- `"strings"`
- `"fmt"`
- `"github.com/u007/ocode/internal/plugins"`

- [ ] **Step 4: Run test**

```bash
go test ./internal/tui/... -run TestContextCommandNilAgentGuard -v 2>&1 | grep -E "FAIL|PASS"
```

Expected: `PASS`

- [ ] **Step 5: Build check**

```bash
go build ./... 2>&1
```

Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/command_test.go
git commit -m "feat: /context base prompt section (mode, ambient files, plugins)"
```

---

### Task 4: Add Tools section (built-in + MCP per server)

Populate the Tools section using `GetToolDefinitions()` grouped by server. The "Injected per request" grand total follows.

**Files:**
- Modify: `internal/tui/model.go` (extend `handleContextCmd`)
- Modify: `internal/tui/command_test.go`

- [ ] **Step 1: Write the test**

Add to `internal/tui/command_test.go`:

```go
func TestContextMCPGrouping(t *testing.T) {
	// groupMCPToolDefs groups tool definitions by server name using config MCP keys.
	serverNames := []string{"claude_ai_Gmail", "context7"}
	toolNames := map[string]struct{}{
		"claude_ai_Gmail_search": {},
		"claude_ai_Gmail_send":   {},
		"context7_query":         {},
		"bash":                   {},
	}
	defs := []map[string]interface{}{
		{"name": "claude_ai_Gmail_search", "description": "search"},
		{"name": "claude_ai_Gmail_send", "description": "send"},
		{"name": "context7_query", "description": "query"},
		{"name": "bash", "description": "run bash"},
	}

	grouped, builtin := groupMCPToolDefs(defs, toolNames, serverNames)

	if len(grouped["claude_ai_Gmail"]) != 2 {
		t.Errorf("expected 2 tools for claude_ai_Gmail, got %d", len(grouped["claude_ai_Gmail"]))
	}
	if len(grouped["context7"]) != 1 {
		t.Errorf("expected 1 tool for context7, got %d", len(grouped["context7"]))
	}
	if len(builtin) != 1 || builtin[0]["name"] != "bash" {
		t.Errorf("expected bash in builtin, got %v", builtin)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tui/... -run TestContextMCPGrouping -v 2>&1 | grep -E "FAIL|PASS|undefined"
```

Expected: `undefined: groupMCPToolDefs`

- [ ] **Step 3: Add `groupMCPToolDefs` helper to `model.go`**

Add as a package-level function near `estimateTok`:

```go
// groupMCPToolDefs separates tool definitions into per-server MCP groups and builtin.
// serverNames are the keys from config.MCP. mcpToolSet is the set of MCP tool names.
func groupMCPToolDefs(
	defs []map[string]interface{},
	mcpToolSet map[string]struct{},
	serverNames []string,
) (grouped map[string][]map[string]interface{}, builtin []map[string]interface{}) {
	grouped = make(map[string][]map[string]interface{})
	for _, def := range defs {
		name, _ := def["name"].(string)
		if _, isMCP := mcpToolSet[name]; !isMCP {
			builtin = append(builtin, def)
			continue
		}
		matched := false
		for _, srv := range serverNames {
			if strings.HasPrefix(name, srv+"_") {
				grouped[srv] = append(grouped[srv], def)
				matched = true
				break
			}
		}
		if !matched {
			builtin = append(builtin, def)
		}
	}
	return
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/tui/... -run TestContextMCPGrouping -v 2>&1 | grep -E "FAIL|PASS"
```

Expected: `PASS`

- [ ] **Step 5: Extend `handleContextCmd` with the Tools section**

In `handleContextCmd` in `model.go`, after the base prompt block (before appending the message), add:

```go
// ── Tools ────────────────────────────────────
b.WriteString("\nTools (injected every request)\n")
toolsTotal := 0

allDefs := m.agent.GetToolDefinitions()
mcpSet := make(map[string]struct{})
for _, name := range m.agent.MCPToolNames() {
    mcpSet[name] = struct{}{}
}
serverNames := make([]string, 0, len(m.config.MCP))
for name := range m.config.MCP {
    serverNames = append(serverNames, name)
}
sort.Strings(serverNames)

grouped, builtinDefs := groupMCPToolDefs(allDefs, mcpSet, serverNames)

// Built-in tools
builtinTok := 0
for _, def := range builtinDefs {
    raw, _ := json.Marshal(def)
    builtinTok += estimateTok(string(raw))
}
toolsTotal += builtinTok
fmt.Fprintf(&b, "  Built-in (%d tools)%s~%s tok\n",
    len(builtinDefs),
    columnPad(fmt.Sprintf("Built-in (%d tools)", len(builtinDefs)), 28),
    formatTok(builtinTok))

// MCP tools per server
for _, srv := range serverNames {
    defs, ok := grouped[srv]
    if !ok {
        continue
    }
    srvTok := 0
    for _, def := range defs {
        raw, _ := json.Marshal(def)
        srvTok += estimateTok(string(raw))
    }
    toolsTotal += srvTok
    label := fmt.Sprintf("MCP: %s  %d tools", srv, len(defs))
    fmt.Fprintf(&b, "  %-28s ~%s tok\n", label, formatTok(srvTok))
}

fmt.Fprintf(&b, "  %-28s ~%s tok\n", "Subtotal", formatTok(toolsTotal))

injectedTotal := baseTotal + toolsTotal
fmt.Fprintf(&b, "\n  %-28s ~%s tok\n", "Injected per request", formatTok(injectedTotal))
```

Ensure `"encoding/json"` and `"sort"` are imported in `model.go`.

- [ ] **Step 6: Build check**

```bash
go build ./... 2>&1
```

Expected: no errors

- [ ] **Step 7: Commit**

```bash
git add internal/tui/model.go internal/tui/command_test.go
git commit -m "feat: /context tools section with MCP grouping by server"
```

---

### Task 5: Add Skills and Session sections, finalize output

Complete the handler with the Skills (on-demand) and Session Messages sections.

**Files:**
- Modify: `internal/tui/model.go` (extend `handleContextCmd`)

- [ ] **Step 1: Extend `handleContextCmd` with Skills section**

After the Tools block, before appending the message, add:

```go
// ── Skills ───────────────────────────────────
b.WriteString("\nSkills (on-demand, not pre-injected)\n")
skills := skill.LoadSkills()
if len(skills) == 0 {
    b.WriteString("  (none found)\n")
} else {
    shown := skills
    extra := 0
    if len(skills) > 5 {
        shown = skills[:5]
        extra = len(skills) - 5
    }
    skillTotal := 0
    for _, s := range skills {
        skillTotal += estimateTok(s.Content)
    }
    for _, s := range shown {
        tok := estimateTok(s.Content)
        fmt.Fprintf(&b, "  %-28s ~%s tok\n", s.Name, formatTok(tok))
    }
    if extra > 0 {
        fmt.Fprintf(&b, "  ... +%d more (%d total)%s~%s tok available\n",
            extra, len(skills),
            columnPad(fmt.Sprintf("... +%d more (%d total)", extra, len(skills)), 24),
            formatTok(skillTotal))
    }
}
```

- [ ] **Step 2: Add Session Messages section**

After the Skills block:

```go
// ── Session Messages ─────────────────────────
b.WriteString("\nSession Messages\n")
modelName := m.currentModelName()
if used := latestPromptTokens(m.messages); used > 0 {
    if window, ok := modelContextWindow(modelName); ok {
        pct := formatPercent(used, window)
        fmt.Fprintf(&b, "  Context window  %s / %s (%s)\n",
            strconv.FormatInt(used, 10),
            strconv.FormatInt(window, 10),
            pct)
    } else {
        fmt.Fprintf(&b, "  Context window  %s tokens\n", strconv.FormatInt(used, 10))
    }
} else {
    b.WriteString("  Context window  n/a\n")
}
sessionTok := m.sessionTelemetry.usedTokens()
if sessionTok > 0 {
    fmt.Fprintf(&b, "  Session total   %s tok\n", strconv.FormatInt(sessionTok, 10))
} else {
    b.WriteString("  Session total   n/a\n")
}
```

- [ ] **Step 3: Move the `m.messages = append(...)` to after all sections**

The final append in `handleContextCmd` should be:

```go
m.messages = append(m.messages, message{role: roleAssistant, text: b.String()})
```

Confirm there is NO `raw:` field on this message — this is what keeps it out of the LLM context.

- [ ] **Step 4: Build check**

```bash
go build ./... 2>&1
```

Expected: no errors

- [ ] **Step 5: Run all tui tests**

```bash
go test ./internal/tui/... -run "TestEstimateTok|TestContextCommand|TestContextMCPGrouping" -v 2>&1 | grep -E "FAIL|PASS"
```

Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat: /context skills and session sections, complete token budget inspector"
```

---

### Task 6: Smoke test and help text verification

Verify `/context` appears in `/help`, autocompletes, and the palette.

**Files:**
- Modify: `internal/tui/command_test.go`

- [ ] **Step 1: Add coverage tests**

Add to `internal/tui/command_test.go`:

```go
func TestContextCommandInHelp(t *testing.T) {
	help := commandHelpText()
	if !strings.Contains(help, "/context") {
		t.Fatalf("expected /context in help text, got:\n%s", help)
	}
}

func TestContextCommandAutocompletes(t *testing.T) {
	m := model{}
	results := autocompleteSlashInput(&m, "/con")
	found := false
	for _, r := range results {
		if r == "/context" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected /context in autocomplete for '/con', got %v", results)
	}
}

func TestContextCommandOutputHasNoRaw(t *testing.T) {
	m := model{width: 80}
	// nil agent path — output must have no raw field
	m.handleContextCmd(nil)
	for _, msg := range m.messages {
		if msg.raw != nil {
			t.Fatal("context command output must not set raw field (would inject into LLM)")
		}
	}
}
```

- [ ] **Step 2: Run tests**

```bash
go test ./internal/tui/... -run "TestContextCommand" -v 2>&1 | grep -E "FAIL|PASS"
```

Expected: all PASS

- [ ] **Step 3: Final build**

```bash
go build ./... 2>&1
```

Expected: no errors

- [ ] **Step 4: Final commit**

```bash
git add internal/tui/command_test.go
git commit -m "test: /context autocomplete, help text, and LLM isolation coverage"
```
