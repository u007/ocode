# Part 08 — discover_more Tool + Sub-agent Seeding

## Task 10: discover_more tool

Always-available tool that re-ranks against a natural-language need and hot-attaches
matches to the sticky set. Because `GetToolDefinitions` is recomputed per Step loop
iteration (Part 06 Step 6), tools attached here appear on the next iteration.

**Files:**
- Modify: `internal/agent/discovery_glue.go` (tool type + registration in `ensureDiscovery`)
- Modify: `internal/agent/discovery_glue_test.go`

**Interfaces:**
- Consumes: `discoveryState`, `discovery.Session`, `tool.Tool`.
- Produces: `type discoverMoreTool struct { agent *Agent }` implementing `tool.Tool`; registered as `a.tools["discover_more"]` when discovery resolves.

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/discovery_glue_test.go`:

```go
func TestDiscoverMoreAttaches(t *testing.T) {
	a := newGateAgent()
	eng := discovery.NewEngine(discovery.FakeEmbedder{Dimension: 128}, t.TempDir())
	_ = eng.Warm(context.Background(), []discovery.Doc{
		{ID: "mcp:Notion/search", Kind: "mcp", Name: "Notion/search", Text: "search notion pages"},
		{ID: "mcp:Notion/update", Kind: "mcp", Name: "Notion/update", Text: "update notion page content"},
	})
	a.disco = &discoveryState{enabled: true, engine: eng, session: discovery.NewSession(eng)}

	tl := discoverMoreTool{agent: a}
	if tl.Name() != "discover_more" {
		t.Fatalf("name = %s", tl.Name())
	}
	out, err := tl.Execute([]byte(`{"need":"search notion pages"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !a.disco.session.IsAttached("mcp:Notion/search") {
		t.Fatalf("discover_more should attach the matching tool; out=%q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run 'TestDiscoverMore' -v`
Expected: FAIL — `discoverMoreTool` undefined.

- [ ] **Step 3: Write the implementation**

Append to `internal/agent/discovery_glue.go` (add `"encoding/json"` to imports):

```go
type discoverMoreTool struct{ agent *Agent }

func (t discoverMoreTool) Name() string { return "discover_more" }
func (t discoverMoreTool) Description() string {
	return "Attach additional MCP tools relevant to a described need. Call this when you need a capability whose tool is not in your current tool list."
}
func (t discoverMoreTool) Parallel() bool { return false }
func (t discoverMoreTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "discover_more",
		"description": t.Description(),
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"need": map[string]interface{}{
					"type":        "string",
					"description": "Natural-language description of the capability you need, e.g. 'send an email'.",
				},
			},
			"required": []string{"need"},
		},
	}
}

func (t discoverMoreTool) Execute(args json.RawMessage) (string, error) {
	var p struct {
		Need string `json:"need"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("discover_more args: %w", err)
	}
	a := t.agent
	if a.disco == nil || !a.disco.enabled {
		return "Discovery is not active; all tools are already available.", nil
	}
	if err := a.disco.engine.Warm(context.Background(), a.discoveryDocs()); err != nil {
		return "", fmt.Errorf("discover_more warm: %w", err)
	}
	added, err := a.disco.session.Discover(context.Background(), p.Need)
	if err != nil {
		return "", fmt.Errorf("discover_more rank: %w", err)
	}
	emitDebug("DISCOVERY", fmt.Sprintf("discover_more(%.40q) → +%d tools", p.Need, len(added)))
	if len(added) == 0 {
		return "No additional tools matched that need. Available tools are listed in the discovery index.", nil
	}
	names := make([]string, 0, len(added))
	for _, d := range added {
		names = append(names, d.Name)
	}
	sort.Strings(names)
	return "Attached: " + strings.Join(names, ", ") + ". They are available on your next step.", nil
}
```

Register it in `ensureDiscovery()`, in the success branch, right after building the
session:

```go
	a.disco = &discoveryState{
		enabled: true,
		engine:  eng,
		session: discovery.NewSession(eng),
	}
	a.tools["discover_more"] = discoverMoreTool{agent: a}
```

Note: `discover_more` is not in `a.mcpTools`, so `discoveryAllows` always returns
true for it. For sub-agents that carry a `spec.Tools` whitelist, `isToolAllowed`
would exclude it — acceptable in Plan 1 (sub-agents still get the name index and
seeded gating). Widening sub-agent whitelists to include `discover_more` is a minor
follow-up.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run 'TestDiscoverMore' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/discovery_glue.go internal/agent/discovery_glue_test.go
git commit -m "feat(agent): discover_more recovery tool"
```

---

## Task 11: Sub-agent discovery seeding

A sub-agent gets its **own fresh** sticky set ranked from its task prompt (it must
not inherit the parent's accumulated set, or savings are lost). Sub-agents are built
via `NewAgent`, which receives a flat tool slice and does **not** populate
`mcpTools` — so we mark the sub-agent's MCP tools from the parent, then run
discovery on its task prompt.

**Files:**
- Modify: `internal/agent/discovery_glue.go` (add `markMCPFrom` helper)
- Modify: `internal/agent/subagent.go` — in `TaskTool.Execute`, after `subAgent.SetSpec(&subSpec)` and before the sub-agent runs, mark MCP + run discovery
- Modify: `internal/agent/discovery_glue_test.go`

**Interfaces:**
- Produces: `func (a *Agent) markMCPFrom(parent *Agent)`

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/discovery_glue_test.go`:

```go
func TestMarkMCPFromParent(t *testing.T) {
	parent := newGateAgent() // has Notion/search, Notion/update as MCP
	child := &Agent{
		tools:    map[string]tool.Tool{"Notion/search": fakeTool{"Notion/search"}, "read": fakeTool{"read"}},
		mcpTools: map[string]struct{}{},
	}
	child.markMCPFrom(parent)
	if _, ok := child.mcpTools["Notion/search"]; !ok {
		t.Fatal("child should inherit the MCP marker for tools it has")
	}
	if _, ok := child.mcpTools["Notion/update"]; ok {
		t.Fatal("child must not mark MCP tools it doesn't have")
	}
	if _, ok := child.mcpTools["read"]; ok {
		t.Fatal("read is not an MCP tool")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run 'TestMarkMCPFrom' -v`
Expected: FAIL — `markMCPFrom` undefined.

- [ ] **Step 3: Write the implementation**

Append to `internal/agent/discovery_glue.go`:

```go
// markMCPFrom marks this agent's tools as MCP when the parent treats them as MCP.
// NewAgent receives a flat tool slice and loses the MCP markers; sub-agents call
// this so their discovery gate knows which tools are gateable.
func (a *Agent) markMCPFrom(parent *Agent) {
	if parent == nil {
		return
	}
	for name := range a.tools {
		if _, ok := parent.mcpTools[name]; ok {
			a.mcpTools[name] = struct{}{}
		}
	}
}
```

- [ ] **Step 4: Wire into `TaskTool.Execute`**

In `internal/agent/subagent.go`, after `subAgent.SetSpec(&subSpec)`, add **only**
the MCP marking — the sub-agent's own `Step()` already calls `RunDiscovery` with
`discoveryQueryFromMessages`, and the sub-agent's sole user message *is*
`params.Prompt`, so an explicit `RunDiscovery` here would be redundant. What is NOT
redundant is `markMCPFrom`: it must run before `Step` so the gate knows which of the
sub-agent's tools are gateable (`NewAgent` drops the MCP markers).

```go
	// Discovery: the sub-agent gets its OWN fresh sticky set (it does not inherit
	// the parent's). NewAgent drops MCP markers, so re-mark them from the parent
	// here; the sub-agent's Step() then ranks against params.Prompt itself.
	subAgent.markMCPFrom(t.mainAgent)
```

- [ ] **Step 5: Run tests + build**

Run: `go test ./internal/agent/ -run 'TestMarkMCPFrom' -v`
Expected: PASS.
Run: `go build ./...`
Expected: success.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/discovery_glue.go internal/agent/subagent.go internal/agent/discovery_glue_test.go
git commit -m "feat(agent): seed per-sub-agent discovery from task prompt"
```
