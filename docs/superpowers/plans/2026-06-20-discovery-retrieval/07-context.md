# Part 07 — MCP Name Index + Prompt Contract (tail injection)

When discovery is active, the model must (a) know every MCP tool exists even when
its schema isn't attached, and (b) be told to call `discover_more` instead of giving
up. Both ride the message tail (like `injectNotesTail`).

> **Superseded by Part 11.** This part builds the MCP-only injector as a single
> `system`-role tail message. Part 11 (skill gating) replaces it with the
> **volatility-split** form: the stable name index + contract stay `system`-role
> (hoisted into the cached system block), while the volatile attached-skill
> descriptions move to a `user`-role tail message. The note in Part 07 that the
> injection "never touches the cached prefix" is **not** accurate for Anthropic —
> `collectAndRemoveSystemMessages` hoists system-role tail messages into the
> cached `system` field. See Part 11's "Why the split" for the corrected model.

## Task 9: injectDiscoveryContext

**Files:**
- Modify: `internal/agent/discovery_glue.go` (add the injector + marker + contract)
- Modify: `internal/agent/agent.go` — `Step()` add `messages = a.injectDiscoveryContext(messages)` immediately after `messages = injectNotesTail(messages, a)`
- Modify: `internal/agent/discovery_glue_test.go`

**Interfaces:**
- Consumes: `discoveryState`, `discoveryDocs`, `Message`.
- Produces: `func (a *Agent) injectDiscoveryContext(messages []Message) []Message`

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/discovery_glue_test.go`:

```go
func TestInjectDiscoveryContextOnlyWhenActive(t *testing.T) {
	a := newGateAgent()
	base := []Message{{Role: "user", Content: "hi"}}

	// Off: byte-identical (no-op).
	if got := a.injectDiscoveryContext(base); len(got) != len(base) {
		t.Fatalf("off must be a no-op, got %d msgs", len(got))
	}

	// On: appends one system message naming every MCP tool + the contract.
	a.disco = &discoveryState{enabled: true}
	got := a.injectDiscoveryContext(base)
	if len(got) != len(base)+1 {
		t.Fatalf("on must append one tail message, got %d", len(got))
	}
	last := got[len(got)-1]
	if last.Role != "system" {
		t.Fatalf("tail must be a system message")
	}
	if !contains(last.Content, "Notion/search") || !contains(last.Content, "Notion/update") {
		t.Fatalf("name index must list all MCP tools: %q", last.Content)
	}
	if !contains(last.Content, "discover_more") {
		t.Fatalf("prompt contract must mention discover_more")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run 'TestInjectDiscoveryContext' -v`
Expected: FAIL — `injectDiscoveryContext` undefined.

- [ ] **Step 3: Write the implementation**

Append to `internal/agent/discovery_glue.go`:

```go
const promptDiscoveryMarker = "[ocode:discovery]"

const discoveryPromptContract = `Not every tool is currently loaded. The "Available MCP tools" index below lists every connected MCP tool by name. If you need one that is not in your current tool list, call the discover_more tool with a short description of what you need (e.g. "send an email") BEFORE telling the user you cannot do it — it will attach the matching tools for the rest of this turn.`

// injectDiscoveryContext appends the name index + prompt contract as a single
// system message at the tail (volatile, like injectNotesTail). No-op when
// discovery is inactive — bytes are identical to today.
func (a *Agent) injectDiscoveryContext(messages []Message) []Message {
	if a.disco == nil || !a.disco.enabled {
		return messages
	}
	docs := a.discoveryDocs() // sorted by ID; MCP only in Plan 1
	if len(docs) == 0 {
		return messages
	}
	var b strings.Builder
	b.WriteString(discoveryPromptContract)
	b.WriteString("\n\nAvailable MCP tools (names only — not all loaded):\n")
	for _, d := range docs {
		b.WriteString("- ")
		b.WriteString(d.Name)
		if hint := shortHint(d.Text); hint != "" {
			b.WriteString(" — ")
			b.WriteString(hint)
		}
		b.WriteString("\n")
	}
	return append(messages, Message{Role: "system", Content: promptDiscoveryMarker + "\n" + b.String()})
}

// shortHint returns the description part of a doc text, trimmed to ~40 chars.
func shortHint(text string) string {
	if i := strings.Index(text, ": "); i >= 0 {
		text = text[i+2:]
	}
	text = strings.TrimSpace(text)
	if len(text) > 40 {
		text = strings.TrimSpace(text[:40]) + "…"
	}
	return text
}
```

- [ ] **Step 4: Wire into `Step()`**

In `internal/agent/agent.go` `Step()`, after the notes-tail line, add the discovery
injector (keep it last so it's the very tail, after LSP + notes):

```go
	messages = injectNotesTail(messages, a)
	messages = a.injectDiscoveryContext(messages)
	toolDefs := a.GetToolDefinitions()  // (this line is being moved into the loop in Part 06 Step 6)
```

(If Part 06 Step 6 already moved `toolDefs` into the loop, just add the
`injectDiscoveryContext` line after `injectNotesTail`.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/agent/ -run 'TestInjectDiscoveryContext|TestGate' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/discovery_glue.go internal/agent/discovery_glue_test.go internal/agent/agent.go
git commit -m "feat(agent): MCP name index + discover_more prompt contract on volatile tail"
```
