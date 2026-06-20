package agent

import (
	"context"
	"encoding/json"
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
	// Register the discover_more recovery tool. It is intentionally not in
	// a.mcpTools so discoveryAllows always returns true for it. Sub-agents with
	// a spec.Tools whitelist will exclude it via isToolAllowed — acceptable in
	// Plan 1 (sub-agents still get the name index and seeded gating).
	if a.tools != nil {
		a.tools["discover_more"] = discoverMoreTool{agent: a}
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

// markMCPFrom marks this agent's tools as MCP when the parent treats them as MCP.
// NewAgent receives a flat tool slice and loses the MCP markers; sub-agents call
// this so their discovery gate knows which tools are gateable.
func (a *Agent) markMCPFrom(parent *Agent) {
	if parent == nil {
		return
	}
	if a.mcpTools == nil {
		a.mcpTools = make(map[string]struct{})
	}
	for name := range a.tools {
		if _, ok := parent.mcpTools[name]; ok {
			a.mcpTools[name] = struct{}{}
		}
	}
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
