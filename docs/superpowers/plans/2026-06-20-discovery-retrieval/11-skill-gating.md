# Part 11 — Skill Gating

Extends discovery to skills: skills join the corpus and the name index, the full
skill catalog is suppressed from the cached context (keyed on the **config flag**, so
no race with the discovery result), and only **attached** skills get full
descriptions on the tail. Fail-open re-emits the full catalog so skills are never
lost.

**Prerequisite:** Tasks 1–14 green (HTTP-backed MCP gating working).

## Task 15: Skill corpus + catalog suppression + skill index

**Files:**
- Modify: `internal/agent/discovery_glue.go` (`discoveryDocs` adds skills; rewrite `injectDiscoveryContext`; add `discoveryConfigEnabled`)
- Modify: `internal/agent/context.go` (`LoadContext` gains a `discoveryOn bool` param; skips `BuildCatalog` when true)
- Modify: `internal/agent/prompt.go` (`BasePromptMessages` passes the flag)
- Modify: `internal/tui/model.go` (`askAgent` preload passes the flag) — and any other `LoadContext(` caller (build will flag them)
- Test: `internal/agent/discovery_glue_test.go`

**Interfaces:**
- Produces: `func (a *Agent) discoveryConfigEnabled() bool`; `LoadContext(enabled map[string]bool, memoryEnabled, discoveryOn bool) string`.

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/discovery_glue_test.go`:

```go
func TestSkillsJoinCorpusAndIndex(t *testing.T) {
	a := newGateAgent()
	a.config = &config.Config{}
	a.config.Ocode.Discovery.Enabled = true
	// Active discovery with an empty corpus engine is fine for the index test;
	// injectDiscoveryContext lists docs from discoveryDocs(), not the corpus.
	a.disco = &discoveryState{enabled: true,
		session: discovery.NewSession(discovery.NewEngine(discovery.FakeEmbedder{Dimension: 8}, t.TempDir()))}

	got := a.injectDiscoveryContext([]Message{{Role: "user", Content: "hi"}})
	last := got[len(got)-1].Content
	if !contains(last, "Notion/search") {
		t.Fatalf("MCP tools must appear in the index: %q", last)
	}
	// discoveryDocs now also returns skills (from skill.LoadSkills); the section
	// header must be present even if this test env has no skills installed.
	if !contains(last, "Available skills") {
		t.Fatalf("skill index section header must be present: %q", last)
	}
}

func TestLoadContextSuppressesCatalogWhenDiscoveryOn(t *testing.T) {
	on := LoadContext(map[string]bool{}, false, true)
	off := LoadContext(map[string]bool{}, false, false)
	// The catalog header only appears when there ARE skills; assert the flag at
	// least never ADDS the catalog when on. (If skills exist, off contains it; on must not.)
	if contains(on, "--- Skill Catalog ---") {
		t.Fatalf("discoveryOn must suppress the skill catalog")
	}
	_ = off
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run 'TestSkillsJoinCorpus|TestLoadContextSuppresses' -v`
Expected: FAIL — `LoadContext` arity / `discoveryConfigEnabled` / skill section undefined.

- [ ] **Step 3: Suppress the catalog in `LoadContext`**

In `internal/agent/context.go`, change the signature and guard the catalog:

```go
func LoadContext(enabled map[string]bool, memoryEnabled bool, discoveryOn bool) string {
```

and replace the catalog block:

```go
	if !discoveryOn {
		if skillCatalog := skill.BuildCatalog(); skillCatalog != "" {
			context += skillCatalog
		}
	}
```

- [ ] **Step 4: Update all `LoadContext` callers**

In `internal/agent/prompt.go` `BasePromptMessages`, change the call:

```go
		ctx = LoadContext(enabled, a.MemoryEnabled(), a.discoveryConfigEnabled())
```

In `internal/tui/model.go` `askAgent`, change the preload call:

```go
		m.agent.SetPreloadedContext(agent.LoadContext(enabledPluginMap(m.config), memoryEnabled, m.config.Ocode.Discovery.Enabled))
```

Then `grep -rn 'LoadContext(' internal/ | grep -v _test` and update any remaining
caller (server/CLI/ACP entrypoints) to pass the discovery flag where a config is
available, or `false` where it isn't. The build will fail on any missed caller —
fix each until `go build ./...` is clean.

- [ ] **Step 5: Add skills to the corpus + rewrite the injector**

In `internal/agent/discovery_glue.go`, add the config helper and the skill import:

```go
import (
	// ... existing ...
	"github.com/u007/ocode/internal/skill"
)

func (a *Agent) discoveryConfigEnabled() bool {
	return a.config != nil && a.config.Ocode.Discovery.Enabled
}
```

Extend `discoveryDocs` to also gather skills (keep the MCP loop):

```go
func (a *Agent) discoveryDocs() []discovery.Doc {
	var docs []discovery.Doc
	for _, s := range skill.LoadSkills() {
		text := s.Name
		if s.Description != "" {
			text += ": " + s.Description
		}
		if s.WhenToUse != "" {
			text += " When to use: " + s.WhenToUse
		}
		docs = append(docs, discovery.Doc{ID: "skill:" + s.Name, Kind: "skill", Name: s.Name, Text: text})
	}
	for name := range a.mcpTools {
		t, ok := a.tools[name]
		if !ok {
			continue
		}
		docs = append(docs, discovery.Doc{ID: "mcp:" + name, Kind: "mcp", Name: name, Text: name + ": " + t.Description()})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
	return docs
}
```

Replace `injectDiscoveryContext` (from Part 07) with the skill-aware version:

```go
func (a *Agent) injectDiscoveryContext(messages []Message) []Message {
	if !a.discoveryConfigEnabled() {
		return messages // off → no-op, byte-identical to today
	}
	active := a.disco != nil && a.disco.enabled
	var b strings.Builder

	if !active {
		// Fail-open: LoadContext suppressed the catalog (config flag on), but
		// discovery isn't actually running — re-emit the full skill catalog so
		// skills are never lost. MCP tools are all attached (gate off).
		if cat := skill.BuildCatalog(); cat != "" {
			b.WriteString(cat)
		}
		if b.Len() == 0 {
			return messages
		}
		return append(messages, Message{Role: "system", Content: promptDiscoveryMarker + "\n" + b.String()})
	}

	docs := a.discoveryDocs() // sorted; skills + MCP
	b.WriteString(discoveryPromptContract)

	b.WriteString("\n\nAvailable MCP tools (names only — not all loaded):\n")
	for _, d := range docs {
		if d.Kind != "mcp" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(d.Name)
		if h := shortHint(d.Text); h != "" {
			b.WriteString(" — ")
			b.WriteString(h)
		}
		b.WriteString("\n")
	}

	b.WriteString("\nAvailable skills (names only — load full detail with the skill tool):\n")
	for _, d := range docs {
		if d.Kind != "skill" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(d.Name)
		if h := shortHint(d.Text); h != "" {
			b.WriteString(" — ")
			b.WriteString(h)
		}
		b.WriteString("\n")
	}

	// Full descriptions for attached skills only.
	var attachedSkills []string
	for _, d := range docs {
		if d.Kind == "skill" && a.disco.session.IsAttached(d.ID) {
			attachedSkills = append(attachedSkills, d.Text)
		}
	}
	if len(attachedSkills) > 0 {
		b.WriteString("\nRelevant skills for this task:\n")
		for _, s := range attachedSkills {
			b.WriteString("- ")
			b.WriteString(s)
			b.WriteString("\n")
		}
	}

	return append(messages, Message{Role: "system", Content: promptDiscoveryMarker + "\n" + b.String()})
}
```

- [ ] **Step 6: Run tests + build**

Run: `go test ./internal/agent/ -run 'TestSkillsJoinCorpus|TestLoadContextSuppresses|TestInjectDiscoveryContext|TestGate' -v`
Expected: PASS. (`TestInjectDiscoveryContextOnlyWhenActive` from Part 07 asserts the
off path is a no-op and the on path lists MCP tools + `discover_more` — both still
hold; the agent in that test has no `config`, so `discoveryConfigEnabled()` is false
and the off-path no-op is preserved. If that test set `disco.enabled` without a
config, update it to also set `a.config` with `Discovery.Enabled = true`.)
Run: `go build ./...`
Expected: success (all `LoadContext` callers updated).

- [ ] **Step 7: Commit**

```bash
git add internal/agent/discovery_glue.go internal/agent/context.go internal/agent/prompt.go internal/tui/model.go internal/agent/discovery_glue_test.go
git commit -m "feat(discovery): gate skills — suppress catalog on config flag, skill index + attached details, fail-open catalog"
```
