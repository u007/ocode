package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/discovery"
	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/skill"
	"github.com/u007/ocode/internal/tool"
)

// discoveryConfigEnabled reports whether the config asks for discovery. Used
// by callers (BasePromptMessages, injectDiscoveryContext) that gate on the
// CONFIG FLAG (not the live disco state) so the cached prefix is stable
// regardless of whether the embedder has resolved yet.
func (a *Agent) discoveryConfigEnabled() bool {
	return a.config != nil && a.config.Ocode.Discovery.Enabled
}

type discoveryState struct {
	enabled bool
	engine  *discovery.Engine
	session *discovery.Session
	initErr string // last resolve error (fail-open reason)
}

// ensureDiscovery lazily builds discovery state on first use (by Step time, MCP
// tools are loaded). On any resolve error it FAILS OPEN: leaves disco disabled
// (all tools attached, today's behavior) and logs why.
func (a *Agent) ensureDiscovery() {
	if a.disco != nil || !a.discoveryConfigEnabled() {
		return
	}
	dc := a.config.Ocode.Discovery
	var emb discovery.Embedder
	var err error
	if dc.EmbeddingBackend == "local" {
		// Local backend: spawn the shared model-server (probe-first across
		// ocode processes) and wrap it in the HTTP transport. Supervised
		// spawn is delegated to the agent's process registry so the
		// subprocess participates in shutdown.
		spawn := func(cmdline string) error {
			if a.procs == nil {
				return fmt.Errorf("no process registry available for local server")
			}
			p := a.procs.StartBackground(cmdline)
			// StartBackground sets ProcExited synchronously when cmd.Start (or
			// supervisor Register) fails — surface that instead of letting the
			// caller eat the full health-loop timeout.
			if p != nil && p.SnapshotStatus() == tool.ProcExited {
				return fmt.Errorf("local server process exited immediately on spawn")
			}
			return nil
		}
		base, dim, e := discovery.EnsureLocalServer(spawn, discoveryCacheDir(), func(s string) {
			if err := config.SaveLocalModelStatus(s); err != nil {
				emitDebug("DISCOVERY", fmt.Sprintf("persist local model status %q failed: %v", s, err))
			}
		}, discovery.LocalServerOptions{UserBaseURL: dc.LocalServerURL})
		if e != nil {
			err = e
		} else {
			emb = discovery.NewLocalEmbedder(base, dc.EmbeddingModel, dim)
		}
	} else {
		emb, err = discovery.ResolveEmbedder(dc.EmbeddingBackend, dc.EmbeddingModel, keyForEnv)
	}
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

// discoveryCacheDir returns the directory under ocode's global data dir where
// the local embed server's model + binaries are cached. Layout:
//
//	<GlobalDataDir>/discovery/
//	    local-<os>-<arch>/
//	        llama-b9747/llama-server   (extracted from llama.cpp release tarball)
//	        llama-b9747/lib*.dylib     (sibling libraries, same dir as the binary)
//	        lfm2-5-embedding-350m.gguf
//
// Uses paths.GlobalDataDir() (not os.UserConfigDir()) so the cache lives
// alongside sessions, auth, and usage — one consistent location, not split
// between ~/.config/opencode (XDG/Linux) and ~/Library/Application Support
// (macOS). Falls back to os.TempDir() only if the global dir itself is
// unresolvable, since TempDir is wiped on reboot.
func discoveryCacheDir() string {
	base, err := paths.GlobalDataDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return base + "/discovery"
}

// discoveryDocs gathers the corpus: one Doc per skill (Name + Description +
// WhenToUse) and one Doc per MCP tool (name + description).
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
		docs = append(docs, discovery.Doc{ID: "skill:" + s.Name, Kind: "skill", Name: s.Name, Text: text, Source: s.Source})
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
	// Kick the corpus warm in the background; first turn doesn't block on
	// cold-embed. Until Ready() flips true, the gate attaches everything.
	a.disco.engine.WarmAsync(docs)
	if !a.disco.engine.Ready() {
		emitDebug("DISCOVERY", "corpus still warming — all tools attached this turn")
		return
	}
	added, err := a.disco.session.Discover(context.Background(), query)
	if err != nil {
		emitDebug("DISCOVERY", fmt.Sprintf("rank failed (fail-open, all attached): %v", err))
		a.disco.enabled = false
		return
	}
	if len(added) > 0 && a.OnDiscovery != nil {
		names := make([]string, 0, len(added))
		for _, d := range added {
			names = append(names, d.Name)
		}
		a.OnDiscovery(strings.Join(names, ", "))
	}
	emitDebug("DISCOVERY", fmt.Sprintf("turn rank: %d newly attached, %d total (q=%.60q)",
		len(added), len(a.disco.session.Attached()), query))
}

type DiscoveryStatusInfo struct {
	Active         bool
	Model          string
	Backend        string
	Attached       []string // all attached IDs (skill:* and mcp:*)
	MCPTotal       int
	SkillTotal     int
	AttachedSkills []string // filtered from Attached
	AttachedMCP    []string // filtered from Attached
	InitErr        string
}

// DiscoveryStatus reports the current discovery state (for /discover status, /context).
func (a *Agent) DiscoveryStatus() DiscoveryStatusInfo {
	st := DiscoveryStatusInfo{MCPTotal: len(a.mcpTools)}
	allSkills := skill.LoadSkills()
	st.SkillTotal = len(allSkills)
	if a.config != nil {
		st.Model = a.config.Ocode.Discovery.EmbeddingModel
		st.Backend = a.config.Ocode.Discovery.EmbeddingBackend
	}
	if a.disco != nil {
		st.Active = a.disco.enabled
		st.InitErr = a.disco.initErr
		if a.disco.session != nil {
			st.Attached = a.disco.session.Attached()
			for _, id := range st.Attached {
				if strings.HasPrefix(id, "skill:") {
					st.AttachedSkills = append(st.AttachedSkills, strings.TrimPrefix(id, "skill:"))
				} else if strings.HasPrefix(id, "mcp:") {
					st.AttachedMCP = append(st.AttachedMCP, strings.TrimPrefix(id, "mcp:"))
				}
			}
		}
	}
	return st
}

// DiscoveryGatedTokens reports attached/total MCP counts and the estimated tokens
// saved (schemas of unattached MCP tools) vs the name-index cost. The token
// estimate is bytes/4 — close enough for a human-readable number in /context;
// exact tokenization is not needed for a planning figure.
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
// While the corpus is still warming (Ready() == false), attach everything so a
// fresh session doesn't see a tool list shrink to zero on turn 1.
func (a *Agent) discoveryAllows(name string) bool {
	if a.disco == nil || !a.disco.enabled {
		return true
	}
	if _, isMCP := a.mcpTools[name]; !isMCP {
		return true
	}
	if a.disco.engine == nil || !a.disco.engine.Ready() {
		return true // not warmed yet → don't gate
	}
	return a.disco.session.IsAttached("mcp:" + name)
}

// discoveryQueryFromMessages builds the query from the last user message plus a
// small rolling window of prior user turns (short follow-ups embed to noise
// otherwise). Capped to ~2048 chars.
func discoveryQueryFromMessages(msgs []Message, workDir string) string {
	var userTurns []string
	for i := len(msgs) - 1; i >= 0 && len(userTurns) < 3; i-- {
		if msgs[i].Role == "user" {
			userTurns = append([]string{msgs[i].Content}, userTurns...)
		}
	}
	q := strings.Join(userTurns, "\n")
	// Append project type signals so the embedder can distinguish e.g. Go from
	// Flutter when the user's text is ambiguous (e.g. "refactor this function").
	if sig := projectSignals(workDir); sig != "" {
		q += "\nProject context: " + sig
	}
	if len(q) > 2048 {
		q = q[len(q)-2048:]
	}
	return q
}

// projectSignals detects the project type from marker files in workDir
// (including one level of subdirectories for monorepo support) and returns a
// short descriptive string for the discovery query. Empty if no markers found.
func projectSignals(workDir string) string {
	if workDir == "" {
		return ""
	}
	// Map of marker file → signal text.
	markers := map[string]string{
		"go.mod":         "Go golang project, Go modules",
		"pubspec.yaml":   "Flutter Dart project",
		"package.json":   "JavaScript TypeScript Node.js project",
		"Cargo.toml":     "Rust Cargo project",
		"pyproject.toml": "Python project",
		"pom.xml":        "Java Maven project",
		"build.gradle":   "Java Kotlin Gradle project",
		"Gemfile":        "Ruby project",
		"composer.json":  "PHP project",
		"mix.exs":        "Elixir project",
	}
	seen := make(map[string]bool) // dedupe signals
	var signals []string
	addSignals := func(dir string) {
		for file, signal := range markers {
			if seen[signal] {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, file)); err == nil {
				seen[signal] = true
				signals = append(signals, signal)
			}
		}
	}
	// Check root first.
	addSignals(workDir)
	// Scan immediate subdirectories for monorepo support (e.g. root has go.mod,
	// sub/flutter has pubspec.yaml). Limit to one level to avoid slow traversals.
	entries, err := os.ReadDir(workDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			addSignals(filepath.Join(workDir, e.Name()))
		}
	}
	return strings.Join(signals, ", ")
}

const promptDiscoveryMarker = "[ocode:discovery]"

const discoveryPromptContract = `Not every tool is currently loaded. The "Available MCP tools" index below lists every connected MCP tool by name. If you need one that is not in your current tool list, call the discover_more tool with a short description of what you need (e.g. "send an email") BEFORE telling the user you cannot do it — it will attach the matching tools for the rest of this turn.`

// injectDiscoveryContext appends discovery context split by VOLATILITY so the
// prompt cache survives sticky-set growth. No-op when discovery is off.
//
// Cache rationale (Anthropic): the request builder hoists EVERY system-role
// message into the top-level `system` field, which carries cache_control (see
// collectAndRemoveSystemMessages in client.go). So a system-role tail message is
// NOT in the uncached suffix — it rides the cached system block. Therefore:
//
//   - STABLE content (prompt contract + the full name index — names don't change
//     turn to turn) is emitted as a SYSTEM message → hoisted into the cached
//     system prompt → caches across turns.
//   - VOLATILE content (full descriptions of ATTACHED skills, which grow with the
//     sticky set) is emitted as a USER message → stays in the uncached message
//     tail (collectAndRemove only pulls system-role) → a skill-attach turn no
//     longer rewrites/busts the cached system block. It is wrapped in the
//     discovery marker so the model reads it as system-origin, not user speech.
//
// Three modes:
//   - off (config flag false): no-op.
//   - on but not yet active (e.g. embedder failed to resolve): fail-open by
//     re-emitting the full skill catalog (system-role, stable) so skills are
//     never lost.
//   - on + active: stable name index (system) + attached-skill descriptions (user).
func (a *Agent) injectDiscoveryContext(messages []Message) []Message {
	if !a.discoveryConfigEnabled() {
		return messages
	}
	active := a.disco != nil && a.disco.enabled

	if !active {
		// Fail-open: LoadContext suppressed the catalog (config flag on), but
		// discovery isn't actually running — re-emit the full skill catalog so
		// skills are never lost. MCP tools are all attached (gate off).
		if cat := skill.BuildCatalog(); cat != "" {
			return append(messages, Message{Role: "system", Content: promptDiscoveryMarker + "\n" + cat})
		}
		return messages
	}

	sysContent, volContent := renderDiscoveryContext(a.discoveryDocs(), a.disco.session.IsAttached)
	messages = append(messages, Message{Role: "system", Content: sysContent})
	if volContent != "" {
		messages = append(messages, Message{Role: "user", Content: volContent})
	}
	return messages
}

// renderDiscoveryContext builds the two discovery blocks from the (sorted) docs
// and the attachment predicate, split by volatility:
//
//   - sysContent: prompt contract + full name index. A function of the doc-SET
//     only (which is stable per session) — NOT of which docs are attached. This
//     is what makes it cache-safe: attaching a skill mid-session must leave this
//     string byte-identical so the hoisted system prompt stays cached.
//   - volContent: full descriptions of attached skills (empty when none). This is
//     the only per-turn-growing part; it rides the uncached user tail.
//
// Kept as a pure function so the cache invariant (sysContent is independent of
// attachment) is unit-testable without the filesystem-backed skill loader.
func renderDiscoveryContext(docs []discovery.Doc, isAttached func(id string) bool) (sysContent, volContent string) {
	var sys strings.Builder
	sys.WriteString(promptDiscoveryMarker)
	sys.WriteString("\n")
	sys.WriteString(discoveryPromptContract)
	sys.WriteString("\n\nAvailable MCP tools (names only — not all loaded):\n")
	for _, d := range docs {
		if d.Kind == "mcp" {
			writeIndexLine(&sys, d)
		}
	}
	sys.WriteString("\nAvailable skills (names only — load full detail with the skill tool):\n")
	for _, d := range docs {
		if d.Kind == "skill" {
			writeIndexLine(&sys, d)
		}
	}

	var attachedDocs []discovery.Doc
	for _, d := range docs {
		if d.Kind == "skill" && isAttached(d.ID) {
			attachedDocs = append(attachedDocs, d)
		}
	}
	if len(attachedDocs) > 0 {
		var vol strings.Builder
		vol.WriteString(promptDiscoveryMarker)
		vol.WriteString(" relevant skills for this task (you may use these inline):\n")
		for _, d := range attachedDocs {
			vol.WriteString("- ")
			vol.WriteString(kindIcon(d.Kind))
			vol.WriteString(" ")
			vol.WriteString(d.Text)
			if d.Source != "" {
				vol.WriteString("\n  File: ")
				vol.WriteString(d.Source)
			}
			vol.WriteString("\n")
		}
		volContent = vol.String()
	}
	return sys.String(), volContent
}

// writeIndexLine appends one "- name — hint" name-index line with a kind icon.
func writeIndexLine(b *strings.Builder, d discovery.Doc) {
	b.WriteString("- ")
	b.WriteString(kindIcon(d.Kind))
	b.WriteString(" ")
	b.WriteString(d.Name)
	if h := shortHint(d.Text); h != "" {
		b.WriteString(" — ")
		b.WriteString(h)
	}
	b.WriteString("\n")
}

// kindIcon returns an emoji icon for a doc kind.
func kindIcon(kind string) string {
	switch kind {
	case "skill":
		return "📄" // SKILL.md file
	case "mcp":
		return "🔧" // MCP tool
	default:
		return "•"
	}
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
