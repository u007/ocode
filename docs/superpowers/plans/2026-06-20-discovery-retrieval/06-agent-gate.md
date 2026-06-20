# Part 06 — Agent Gate + Per-Turn Discovery Hook

Wires discovery into the agent: a gate inside `GetToolDefinitions()` (filter MCP
tools + deterministic sort), a per-iteration recompute so `discover_more` works
mid-turn, and a `RunDiscovery` hook at the top of `Step()`.

**Scope note (Plan 1):** the gate applies to **MCP tools only** (`a.mcpTools`).
All built-in tools — including `webfetch`/`websearch` — stay always-on (2 tools,
negligible). **Skills are NOT gated in Plan 1**: the skill catalog stays fully
injected exactly as today. Skill gating would require suppressing
`skill.BuildCatalog()` from the cached context prefix, which races with ocode's
context preload + snapshot + marker-dedup path (`askAgent`,
`buildAgentMessagesSnapshot`, `PrepareMessages`) — high risk for a small win, since
MCP tool schemas are the dominant context cost. The corpus is therefore **MCP tools
only**; `discovery.pinned_skills` stays in config for a future skills phase but is
unused in Plan 1.

## Task 8: Discovery state, gate, query hook

**Files:**
- Create: `internal/agent/discovery_glue.go`
- Modify: `internal/agent/agent.go` — `Agent` struct (add one field ~line 80); `GetToolDefinitions` (2698-2707); `Step` (move tool-def computation into loop ~699; add `RunDiscovery` call ~683)
- Create: `internal/agent/discovery_glue_test.go`
- Modify: `TODO.md` (note the keyring-fallback follow-up)

**Interfaces:**
- Consumes: `discovery.Engine`, `discovery.Session`, `discovery.ResolveEmbedder`, `discovery.Doc`, `skill.LoadSkills` (Parts 02–05).
- Produces:
  - `Agent.disco *discoveryState`
  - `func (a *Agent) RunDiscovery(query string)`
  - `func (a *Agent) discoveryAllows(name string) bool`
  - `func (a *Agent) discoveryEnabled() bool`
  - `func discoveryQueryFromMessages(msgs []Message) string`

- [ ] **Step 1: Write the failing test**

Create `internal/agent/discovery_glue_test.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/u007/ocode/internal/discovery"
)

type fakeTool struct{ name string }

func (f fakeTool) Name() string                              { return f.name }
func (f fakeTool) Description() string                       { return f.name }
func (f fakeTool) Definition() map[string]interface{}        { return map[string]interface{}{"name": f.name} }
func (f fakeTool) Execute(json.RawMessage) (string, error)   { return "", nil }
func (f fakeTool) Parallel() bool                            { return false }

func newGateAgent() *Agent {
	a := &Agent{
		tools:    map[string]tool.Tool{},
		mcpTools: map[string]struct{}{},
	}
	a.tools["read"] = fakeTool{"read"}                 // built-in: never gated
	a.tools["Notion/search"] = fakeTool{"Notion/search"}
	a.tools["Notion/update"] = fakeTool{"Notion/update"}
	a.mcpTools["Notion/search"] = struct{}{}
	a.mcpTools["Notion/update"] = struct{}{}
	return a
}

func TestGateOffAttachesEverythingSorted(t *testing.T) {
	a := newGateAgent() // a.disco == nil → discovery off
	defs := a.GetToolDefinitions()
	var names []string
	for _, d := range defs {
		names = append(names, d["name"].(string))
	}
	want := []string{"Notion/search", "Notion/update", "read"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("off path must include all, sorted: got %v want %v", names, want)
	}
}

func TestGateOnFiltersMCPOnly(t *testing.T) {
	a := newGateAgent()
	eng := discovery.NewEngine(discovery.FakeEmbedder{Dimension: 64}, t.TempDir())
	_ = eng.Warm(context.Background(), []discovery.Doc{
		{ID: "mcp:Notion/search", Kind: "mcp", Name: "Notion/search", Text: "search notion"},
		{ID: "mcp:Notion/update", Kind: "mcp", Name: "Notion/update", Text: "update notion page"},
	})
	sess := discovery.NewSession(eng)
	sess.Seed([]string{"mcp:Notion/search"}) // only search attached
	a.disco = &discoveryState{enabled: true, engine: eng, session: sess}

	var names []string
	for _, d := range a.GetToolDefinitions() {
		names = append(names, d["name"].(string))
	}
	want := []string{"Notion/search", "read"} // update gated out; read (built-in) stays
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("on path should gate unattached MCP only: got %v want %v", names, want)
	}
}

func TestDiscoveryQueryUsesRecentUserTurns(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "set up notion sync"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "yes"},
	}
	q := discoveryQueryFromMessages(msgs)
	if !contains(q, "notion") || !contains(q, "yes") {
		t.Fatalf("query must blend recent user turns, got %q", q)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run 'TestGate|TestDiscoveryQuery' -v`
Expected: FAIL — `discoveryState`/`discoveryQueryFromMessages` undefined.

- [ ] **Step 3: Add the struct field**

In `internal/agent/agent.go`, add to the `Agent` struct (after `mcpErrors []string`, ~line 78):

```go
	// disco holds discovery (skill/MCP retrieval) state when /discovery is on.
	// nil means discovery is off → no gating, today's behavior.
	disco *discoveryState
```

- [ ] **Step 4: Write the glue file**

Create `internal/agent/discovery_glue.go`:

```go
package agent

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/discovery"
)

type discoveryState struct {
	enabled bool
	engine  *discovery.Engine
	session *discovery.Session
	initErr string // last resolve error (fail-open reason)
}

// discoveryEnabled reports whether the config asks for discovery.
func (a *Agent) discoveryEnabled() bool {
	return a.config != nil && a.config.Ocode.Discovery.Enabled
}

// ensureDiscovery lazily builds discovery state on first use (by Step time, MCP
// tools are loaded). On any resolve error it FAILS OPEN: leaves disco disabled
// (all tools attached, today's behavior) and logs why.
func (a *Agent) ensureDiscovery() {
	if a.disco != nil || !a.discoveryEnabled() {
		return
	}
	dc := a.config.Ocode.Discovery
	emb, err := discovery.ResolveEmbedder(dc.EmbeddingBackend, dc.EmbeddingModel, keyForEnv)
	if err != nil {
		emitDebug("DISCOVERY", fmt.Sprintf("disabled (fail-open): %v", err))
		a.disco = &discoveryState{enabled: false, initErr: err.Error()}
		return
	}
	eng := discovery.NewEngine(emb, discoveryCacheDir())
	a.disco = &discoveryState{
		enabled: true,
		engine:  eng,
		session: discovery.NewSession(eng),
	}
}

// keyForEnv resolves an embedding API key. Env var is primary (matches the
// provider EnvVar precedence). Stored-credential (keyring) fallback is a
// follow-up — see TODO.md.
func keyForEnv(envVar string) string { return os.Getenv(envVar) }

func discoveryCacheDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return base + "/opencode/discovery"
}

// discoveryDocs gathers the corpus: one Doc per MCP tool (Plan 1 gates MCP only).
func (a *Agent) discoveryDocs() []discovery.Doc {
	var docs []discovery.Doc
	for name := range a.mcpTools {
		t, ok := a.tools[name]
		if !ok {
			continue
		}
		desc := t.Description()
		docs = append(docs, discovery.Doc{ID: "mcp:" + name, Kind: "mcp", Name: name, Text: name + ": " + desc})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
	return docs
}

// RunDiscovery ranks the query and grows the sticky set. No-op when discovery is
// off or has failed open. Fail-open on any error.
func (a *Agent) RunDiscovery(query string) {
	a.ensureDiscovery()
	if a.disco == nil || !a.disco.enabled || strings.TrimSpace(query) == "" {
		return
	}
	docs := a.discoveryDocs()
	if len(docs) == 0 {
		return
	}
	if err := a.disco.engine.Warm(context.Background(), docs); err != nil {
		emitDebug("DISCOVERY", fmt.Sprintf("warm failed (fail-open, all attached): %v", err))
		a.disco.enabled = false
		return
	}
	added, err := a.disco.session.Discover(context.Background(), query)
	if err != nil {
		emitDebug("DISCOVERY", fmt.Sprintf("rank failed (fail-open, all attached): %v", err))
		a.disco.enabled = false
		return
	}
	emitDebug("DISCOVERY", fmt.Sprintf("turn rank: %d newly attached, %d total (q=%.60q)",
		len(added), len(a.disco.session.Attached()), query))
}

// discoveryAllows gates MCP tools by the sticky set. Built-ins are never gated.
func (a *Agent) discoveryAllows(name string) bool {
	if a.disco == nil || !a.disco.enabled {
		return true
	}
	if _, isMCP := a.mcpTools[name]; !isMCP {
		return true
	}
	return a.disco.session.IsAttached("mcp:" + name)
}

// discoveryQueryFromMessages builds the query from the last user message plus a
// small rolling window of prior user turns (short follow-ups embed to noise
// otherwise). Capped to ~2048 chars.
func discoveryQueryFromMessages(msgs []Message) string {
	var userTurns []string
	for i := len(msgs) - 1; i >= 0 && len(userTurns) < 3; i-- {
		if msgs[i].Role == "user" {
			userTurns = append([]string{msgs[i].Content}, userTurns...)
		}
	}
	q := strings.Join(userTurns, "\n")
	if len(q) > 2048 {
		q = q[len(q)-2048:]
	}
	return q
}
```

- [ ] **Step 5: Modify `GetToolDefinitions` for filter + deterministic order**

Replace the body (agent.go:2698-2707):

```go
func (a *Agent) GetToolDefinitions() []map[string]interface{} {
	names := make([]string, 0, len(a.tools))
	for name := range a.tools {
		if !a.isToolAllowed(name) {
			continue
		}
		if !a.discoveryAllows(name) {
			continue
		}
		names = append(names, name)
	}
	// Deterministic order keeps the provider tool-cache prefix stable across
	// turns (a.tools is a map → random iteration would bust the cache and
	// defeat the sticky-set benefit).
	sort.Strings(names)
	defs := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		defs = append(defs, a.tools[name].Definition())
	}
	return defs
}
```

Ensure `"sort"` is imported in `agent.go` (it is widely used; if the build
complains, add it).

- [ ] **Step 6: Hook `Step()` — run discovery + recompute tool defs per iteration**

In `Step()` (agent.go), two edits:

(a) Right after `messages = a.PrepareMessages(messages, "")` and the inject calls,
BEFORE `toolDefs := a.GetToolDefinitions()`, add the discovery hook. Place it
immediately after `preLen := len(messages)` / before `PrepareMessages` so the query
reflects the user turn:

```go
	a.RunDiscovery(discoveryQueryFromMessages(messages))
	preLen := len(messages)
	messages = a.PrepareMessages(messages, "")
```

(b) Move the tool-def computation INTO the loop so a mid-turn `discover_more` call
(Part 08) is visible on the next iteration. Delete the single
`toolDefs := a.GetToolDefinitions()` line before the loop, and add it as the first
statement inside `for i := 0; ; i++ {`:

```go
	for i := 0; ; i++ {
		if isCancelled() {
			return newMsgs, nil
		}
		toolDefs := a.GetToolDefinitions()
		// ... existing loop body ...
```

(The earlier `emitDebug(... len(toolDefs))` line that referenced the pre-loop
variable must move inside the loop too, or be deleted — keep the build green.)

- [ ] **Step 7: Add the TODO note**

Append to `TODO.md`:

```markdown
- Discovery embedder key resolution uses `os.Getenv` only (internal/agent/discovery_glue.go:keyForEnv). Wire the stored-credential/keyring fallback (same source `/connect` populates) so users who authed via OAuth/keyring rather than env vars can use HTTP embedders.
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run 'TestGate|TestDiscoveryQuery' -v`
Expected: PASS.

Then build everything:
Run: `go build ./...`
Expected: success.

- [ ] **Step 9: Commit**

```bash
git add internal/agent/discovery_glue.go internal/agent/discovery_glue_test.go internal/agent/agent.go TODO.md
git commit -m "feat(agent): discovery gate in GetToolDefinitions + per-turn rank hook"
```
