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
