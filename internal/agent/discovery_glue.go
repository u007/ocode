package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/crashguard"
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
	enabled    bool
	engine     *discovery.Engine
	session    *discovery.Session
	initErr    string // last resolve error (fail-open reason)
	lastPinned map[string]struct{}
	warming    atomic.Bool // a background corpus warm is in flight (single-flight)
	// judgeVetoed counts candidates the TypeSafe judge kept out this session
	// (observability for /discovery status). atomic because the TUI/HTTP status
	// reader can run while the agent goroutine is mid-turn.
	judgeVetoed atomic.Int64
	// judge caches the TypeSafe judge resolution for this state's lifetime
	// (judgeOnce guards it). Resolving through the shared client factory on
	// every turn and every /discovery status read would re-emit NewClient's
	// "no API key ... refusing to build client" debug line for everyone
	// without a TypeSafe key. A /connect typesafe mid-session therefore takes
	// effect on the next ResetDiscovery (/discovery toggle) or restart.
	judgeOnce sync.Once
	judge     *TypesafeClient
	// tail is a bounded snapshot of the turn's messages, recorded by
	// runDiscovery so the ON-DEMAND discover_more judge sees the same
	// conversation the per-turn judge saw. Guarded by tailMu because Step
	// writes it while /api status reads run on another goroutine.
	tailMu sync.Mutex
	tail   []Message
}

// discoveryWarmTimeout bounds a background corpus warm. Generous because a local
// embedder (MLX/llama.cpp) needs seconds to embed the cold corpus + load its
// model into RAM — far more than the per-turn synchronous budget. Once this
// completes the cache is hot and per-turn warms early-return in microseconds.
const discoveryWarmTimeout = 20 * time.Second

// discoverySignalMinChars: user text shorter than this gets no project-type
// signal appended to the discovery query (see discoveryQueryFromMessages).
const discoverySignalMinChars = 40

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
			cmdline = tool.WrapWithParentMonitor(cmdline)
			p := a.procs.StartBackground(cmdline)
			// StartBackground sets ProcExited synchronously when cmd.Start (or
			// supervisor Register) fails — surface that instead of letting the
			// caller eat the full health-loop timeout.
			if p != nil && p.SnapshotStatus() == tool.ProcExited {
				return fmt.Errorf("local server process exited immediately on spawn")
			}
			return nil
		}
		// Resolve which local model to serve. If the user hasn't picked one,
		// fall back to the host default (LFM2.5/MLX on Apple Silicon, BGE elsewhere).
		modelID := dc.EmbeddingModel
		if modelID == "" {
			modelID = discovery.DefaultLocalModelID()
		}
		base, dim, e := discovery.EnsureLocalServer(spawn, modelID, discoveryCacheDir(), func(s string) {
			if err := config.SaveLocalModelStatus(s); err != nil {
				a.emitDebug("DISCOVERY", fmt.Sprintf("persist local model status %q failed: %v", s, err))
			}
		}, discovery.LocalServerOptions{UserBaseURL: dc.LocalServerURL})
		if e != nil {
			err = e
		} else {
			emb = discovery.NewLocalEmbedder(base, modelID, dim)
		}
	} else {
		emb, err = discovery.ResolveEmbedder(dc.EmbeddingBackend, dc.EmbeddingModel, keyForEnv)
	}
	if err != nil {
		a.emitDebug("DISCOVERY", fmt.Sprintf("disabled (fail-open): %v", err))
		a.disco = &discoveryState{enabled: false, initErr: err.Error()}
		return
	}
	eng := discovery.NewEngine(emb, discoveryCacheDir())
	a.disco = &discoveryState{
		enabled:    true,
		engine:     eng,
		session:    discovery.NewSession(eng),
		lastPinned: map[string]struct{}{},
	}
	// Seed permanently-pinned skills into the discovery session so they are
	// always treated as "attached" regardless of embedding rank.
	a.SyncPinnedSkills()
	// Seed the markdown corpus from the on-disk cache and kick off background
	// summarization of new/changed files.
	a.ensureMDState()
	// Register the discover_more recovery tool. It is intentionally not in
	// a.mcpTools so discoveryAllows always returns true for it. Sub-agents with
	// a spec.Tools whitelist will exclude it via isToolAllowed — acceptable in
	// Plan 1 (sub-agents still get the name index and seeded gating).
	if a.tools != nil {
		a.tools["discover_more"] = discoverMoreTool{agent: a}
	}
}

// SyncPinnedSkills re-seeds the discovery session so it matches the current
// pinned-skill set in config. Pinned skills bypass embedding ranking and are
// always treated as "attached" — they always appear in the volatile
// skill-description block injected by injectDiscoveryContext.
//
// The discovery session is grow-only by design (see internal/discovery), so
// unpinning a skill cannot remove an existing attachment. We work around that
// by rebuilding the session from scratch whenever the pinned set changes.
// Non-pinned attachments (skills and MCPs discovered via embedding) are
// preserved by re-seeding the union of current attached IDs that were NOT
// pinned before plus the new pinned IDs.
//
// No-op when discovery is off or the session has not been created yet.
// Safe to call mid-session (e.g. after the user pins or unpins a skill).
func (a *Agent) SyncPinnedSkills() {
	if a.disco == nil || !a.disco.enabled || a.disco.session == nil {
		return
	}
	pinned := a.pinnedSkillIDs()
	pinnedSet := make(map[string]struct{}, len(pinned))
	for _, id := range pinned {
		pinnedSet[id] = struct{}{}
	}

	// If the previously-known pinned set matches the current one, the
	// session is already in sync (or will grow organically). No need to
	// rebuild — that would drop the user's discover_more results.
	if maps.Equal(a.disco.lastPinned, pinnedSet) {
		return
	}

	// Compute the desired attached set: existing non-stale attachments +
	// current pinned IDs. An attachment is "stale" if it was pinned before
	// but is no longer pinned now.
	existing := a.disco.session.Attached()
	kept := make([]string, 0, len(existing)+len(pinned))
	seen := make(map[string]struct{}, len(existing)+len(pinned))
	for _, id := range existing {
		if a.disco.lastPinned != nil {
			if _, wasPinned := a.disco.lastPinned[id]; wasPinned {
				if _, stillPinned := pinnedSet[id]; !stillPinned {
					continue // stale pinned id, drop it
				}
			}
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		kept = append(kept, id)
	}
	for _, id := range pinned {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		kept = append(kept, id)
	}
	a.disco.session = discovery.NewSession(a.disco.engine)
	a.disco.session.Seed(kept)
	a.disco.lastPinned = pinnedSet
}

// pinnedSkillIDs returns the configured pinned-skill IDs ("skill:<name>").
func (a *Agent) pinnedSkillIDs() []string {
	if a.config == nil {
		return nil
	}
	pinned := a.config.Ocode.Discovery.PinnedSkills
	if len(pinned) == 0 {
		return nil
	}
	ids := make([]string, len(pinned))
	for i, name := range pinned {
		ids[i] = "skill:" + name
	}
	return ids
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

// DiscoveryCacheDir exposes discoveryCacheDir to other packages (e.g. the TUI
// /localmodel command, which spawns local chat-model servers into the same
// cache layout as the embedder).
func DiscoveryCacheDir() string {
	return discoveryCacheDir()
}

// discoveryModelRoot returns the active model id and project root used to gate
// Kaizen (per-model tuned) skills. Mirrors the resolution in prompt.go so the
// discovery corpus admits the same tuned skills LoadContext would.
func (a *Agent) discoveryModelRoot() (activeModel, root string) {
	if a.client != nil {
		activeModel = a.client.GetModel()
	}
	root = a.workDir
	if root == "" {
		if cwd, err := os.Getwd(); err == nil {
			root = cwd
		}
	}
	return activeModel, root
}

// skillDoc builds the discovery corpus Doc for a skill (Name + Description +
// WhenToUse). Shared by the normal and Kaizen skill passes so both index lines
// render identically.
func skillDoc(s skill.Skill) discovery.Doc {
	text := s.Name
	if s.Description != "" {
		text += ": " + s.Description
	}
	if s.WhenToUse != "" {
		text += " When to use: " + s.WhenToUse
	}
	return discovery.Doc{ID: "skill:" + s.Name, Kind: "skill", Name: s.Name, Text: text, Source: s.Source}
}

// discoveryDocs gathers the corpus: one Doc per skill (Name + Description +
// WhenToUse) and one Doc per MCP tool (name + description). Kaizen (per-model
// tuned) skills admitted for the active model+stack are appended so they always
// appear in the names-index (never dependent on embedding rank); the model still
// loads their full body on demand via the skill tool.
func (a *Agent) discoveryDocs() []discovery.Doc {
	var docs []discovery.Doc
	activeModel, root := a.discoveryModelRoot()
	for _, s := range skill.LoadSkillsForRoot(root) {
		if s.TunedFor != "" {
			continue // Kaizen skills are gated separately below
		}
		docs = append(docs, skillDoc(s))
	}
	for _, s := range skill.KaizenSkillsForModel(root, activeModel) {
		docs = append(docs, skillDoc(s))
	}
	for name := range a.mcpTools {
		t, ok := a.tools[name]
		if !ok {
			continue
		}
		docs = append(docs, discovery.Doc{ID: "mcp:" + name, Kind: "mcp", Name: name, Text: name + ": " + t.Description()})
	}
	// Project markdown docs (summaries generated in the background; only files
	// with a ready cached summary appear here).
	docs = append(docs, a.mdDocs()...)
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
	return docs
}

// RunDiscovery ranks the query and grows the sticky set. No-op when discovery is
// off or has failed open. Fail-open on any error. It carries no transcript tail
// for the TypeSafe judge; callers holding the live message list should use
// RunDiscoveryForMessages so the judge can see the conversation.
func (a *Agent) RunDiscovery(query string) {
	a.runDiscovery(query, nil)
}

// RunDiscoveryForMessages derives the discovery query from the message list and
// runs discovery with those messages available to the TypeSafe judge. Step uses
// this so the judge sees the same conversation the embedder ranked against.
func (a *Agent) RunDiscoveryForMessages(messages []Message) {
	a.runDiscovery(discoveryQueryFromMessages(messages, a.workDir), messages)
}

// runDiscovery is the shared implementation behind both entry points. It ranks
// the query (Select) and then decides what joins the sticky set: when TypeSafe
// is connected it judges the candidates and seeds only the kept ones, otherwise
// (or on any judge failure) it seeds every candidate — today's behavior.
func (a *Agent) runDiscovery(query string, tail []Message) {
	// The "context" knowledge sub-agent already has dedicated doc_search/
	// doc_get tools over the OKF bundle; it does not need the repo-wide
	// markdown summarization pass (mdSummarizePass) or embedder warm-up.
	// Skipping here removes a major source of knowledge_lookup latency.
	if a.skipDiscovery {
		return
	}
	a.ensureDiscovery()
	// Record the turn's messages for the discover_more judge BEFORE any early
	// return below. The cold-cache deferral is precisely the turn where nothing
	// is attached and the model must fall back to discover_more, so that is
	// exactly when the judge needs the conversation.
	a.noteDiscoveryTail(tail)
	if a.disco == nil || !a.disco.enabled || strings.TrimSpace(query) == "" {
		return
	}
	// Re-scan project markdown for new/changed files (throttled, background).
	a.refreshMDSummaries()
	docs := a.discoveryDocs()
	if len(docs) == 0 {
		return
	}
	// Warm the corpus before ranking. On a hot cache (unchanged doc-set) Warm is
	// a microsecond hash-check + early-return, so we try it synchronously under a
	// tight budget to attach skills on the same turn. On a COLD cache a local
	// embedder needs seconds — far more than any per-turn budget — so we defer to
	// a background warm (generous deadline) that actually completes and persists
	// the cache. This turn attaches nothing (the gate holds — see
	// discoveryAllows); the names-only index plus discover_more cover the gap, and
	// every later turn hits the fast path.
	//
	// The all-or-nothing cache (BuildCorpusCached persists only on full success)
	// is why a too-tight synchronous budget could never make progress on a local
	// embedder — the background warm breaks that deadlock.
	if a.disco.warming.Load() {
		return // a background warm is in flight; stay fail-open until it lands
	}
	warmCtx, warmCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	err := a.disco.engine.Warm(warmCtx, docs)
	warmCancel()
	if err != nil {
		a.startBackgroundWarm(docs)
		a.emitDebug("DISCOVERY", fmt.Sprintf("corpus warm deferred to background: %v", err))
		return
	}
	// Select embeds the query against a (now-warm) corpus. On a hot cache this is
	// normally fast, but nothing bounded it before: any embedder slowness
	// (network latency, a local model server serializing this behind other work)
	// stalled the whole turn before the first streamed token, with no timeout to
	// fail open like Warm has. Give it the same tight per-turn budget.
	rankCtx, rankCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	candidates, err := a.disco.session.Select(rankCtx, query)
	rankCancel()
	if err != nil {
		a.emitDebug("DISCOVERY", fmt.Sprintf("rank failed (fail-open, all attached): %v", err))
		a.disco.enabled = false
		return
	}
	if len(candidates) == 0 {
		return // nothing new: no judge call, no attach, no OnDiscovery
	}

	// The TypeSafe judge may veto candidates the embedder proposed. It runs only
	// when TypeSafe is connected; any failure keeps every candidate (fail-open —
	// the judge must never attach fewer docs than today's behavior).
	//
	// judgeNote is folded into the turn's single rank line so a reader can always
	// tell whether Jev filtered this turn: "judge=none" means TypeSafe is not
	// connected (nothing was filtered), "error (fail-open)" means the call failed,
	// and "kept N/M" is the actual verdict. Without this, "Jev vetoed nothing" and
	// "Jev was never consulted" looked identical in the log.
	keep := candidates
	judgeNote := "judge=none (typesafe not connected)"
	if client := a.discoveryJudgeClient(); client != nil {
		judged, jerr := a.judgeDiscoveryCandidates(client, tail, query, candidates)
		if jerr != nil {
			judgeNote = fmt.Sprintf("judge=%s error (fail-open)", client.Model)
			a.emitDebug("DISCOVERY", fmt.Sprintf("typesafe judge failed (fail-open, all attached): %v", jerr))
		} else {
			keep = judged
			if vetoed := len(candidates) - len(keep); vetoed > 0 {
				a.disco.judgeVetoed.Add(int64(vetoed))
			}
			judgeNote = fmt.Sprintf("judge=%s kept %d/%d", client.Model, len(keep), len(candidates))
		}
	}
	ids := make([]string, 0, len(keep))
	for _, d := range keep {
		ids = append(ids, d.ID)
	}
	a.disco.session.Seed(ids)
	if len(keep) > 0 && a.OnDiscovery != nil {
		names := make([]string, 0, len(keep))
		for _, d := range keep {
			names = append(names, d.Name)
		}
		a.OnDiscovery(strings.Join(names, ", "))
	}
	a.emitDebug("DISCOVERY", fmt.Sprintf("turn rank: %d newly attached, %d total [%s] (q=%.60q)",
		len(keep), len(a.disco.session.Attached()), judgeNote, query))
}

// startBackgroundWarm warms the corpus off the turn's critical path with a
// generous deadline, so a slow local embedder can finish embedding + persist the
// cache without blocking the response. Single-flight via disco.warming: repeated
// per-turn calls while a warm is running are no-ops. Once it completes the cache
// is hot and the synchronous per-turn warm early-returns.
func (a *Agent) startBackgroundWarm(docs []discovery.Doc) {
	d := a.disco
	if d == nil || !d.warming.CompareAndSwap(false, true) {
		return // already warming
	}
	crashguard.Go(func() {
		defer d.warming.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), discoveryWarmTimeout)
		defer cancel()
		if err := d.engine.Warm(ctx, docs); err != nil {
			a.emitDebug("DISCOVERY", fmt.Sprintf("background corpus warm failed: %v", err))
			return
		}
		a.emitDebug("DISCOVERY", "background corpus warm complete (cache hot; ranking resumes next turn)")
	})
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
	AttachedMD     []string // filtered from Attached
	AllSkills      []string // full corpus skill names (every name injected into the names-index)
	AllMCP         []string // full corpus MCP tool names (every name injected into the names-index)
	AllMD          []string // project-doc names with a ready summary (injected into the names-index)
	MDPending      int      // md files discovered but not yet summarized (background in flight)
	InitErr        string
	// Judge is the TypeSafe relevance judge model ("typesafe/jev-latest") when
	// TypeSafe is connected, or "" when the judge is not active. JudgeVetoed is
	// the running count of candidates the judge kept out this session.
	Judge       string
	JudgeVetoed int
}

// DiscoveryStatus reports the current discovery state (for /discover status, /context).
func (a *Agent) DiscoveryStatus() DiscoveryStatusInfo {
	st := DiscoveryStatusInfo{MCPTotal: len(a.mcpTools)}
	if a.config != nil {
		st.Model = a.config.Ocode.Discovery.EmbeddingModel
		st.Backend = a.config.Ocode.Discovery.EmbeddingBackend
	}
	if a.disco != nil {
		st.Active = a.disco.enabled
		st.InitErr = a.disco.initErr
		st.JudgeVetoed = int(a.disco.judgeVetoed.Load())
		// The judge's activation condition is TypeSafe connectivity (the shared
		// factory yielding a keyed *TypesafeClient), the sole "connected" check.
		if a.disco.enabled && a.discoveryJudgeClient() != nil {
			st.Judge = discoveryJudgeModel
		}
		if a.disco.session != nil {
			st.Attached = a.disco.session.Attached()
			for _, id := range st.Attached {
				if strings.HasPrefix(id, "skill:") {
					st.AttachedSkills = append(st.AttachedSkills, strings.TrimPrefix(id, "skill:"))
				} else if strings.HasPrefix(id, "mcp:") {
					st.AttachedMCP = append(st.AttachedMCP, strings.TrimPrefix(id, "mcp:"))
				} else if strings.HasPrefix(id, "md:") {
					st.AttachedMD = append(st.AttachedMD, strings.TrimPrefix(id, "md:"))
				}
			}
		}
	}
	// Full corpus: every doc whose name is injected into the names-index, whether
	// or not it is attached. Independent of session/embedder warm state.
	for _, d := range a.discoveryDocs() {
		switch d.Kind {
		case "skill":
			st.AllSkills = append(st.AllSkills, d.Name)
		case "mcp":
			st.AllMCP = append(st.AllMCP, d.Name)
		case "md":
			st.AllMD = append(st.AllMD, d.Name)
		}
	}
	// SkillTotal is the admitted corpus — what the names-index actually
	// contains (every non-Kaizen skill plus the Kaizen skills admitted for the
	// active model). Counting all root skills instead would inflate the
	// denominator with per-model-tuned skills gated out for the active model,
	// making /discover status's attached/total misleading.
	st.SkillTotal = len(st.AllSkills)
	st.MDPending = a.mdPending()
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
//
// There is deliberately NO warm check here. A cold corpus is the NORMAL
// first-turn state for the local backend — runDiscovery defers the warm to the
// background when the 500ms synchronous budget is not enough — and the old
// "warm failed → don't gate" escape therefore fired on exactly the turns
// discovery exists to shrink, exposing the entire MCP corpus (274 zoho-books
// schemas plus the built-ins, "exposing 322 tools"). Failing open was never
// necessary: the names-only index still advertises every tool by name and
// discover_more warms, ranks and attaches on demand, so a cold turn legitimately
// starts with zero MCP tool definitions. A nil session (an invariant violation
// — ensureDiscovery always sets one when enabled) gates rather than fails open,
// so it can never panic IsAttached.
//
// The one fail-open that survives is disco == nil / !disco.enabled above: when
// discovery never initialized (embedder unresolved), the feature is OFF and
// gating would strand every MCP tool behind a discover_more call that cannot
// work.
func (a *Agent) discoveryAllows(name string) bool {
	if a.disco == nil || !a.disco.enabled {
		return true
	}
	if _, isMCP := a.mcpTools[name]; !isMCP {
		return true
	}
	if a.disco.session == nil {
		return false
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
	// Skipped for short text: on a query like "ignore" or "use agent browser"
	// the signal dominates the embedding, every doc about this repo scores
	// near-identical, and rank-relative selection attaches the whole corpus.
	if len(strings.TrimSpace(q)) >= discoverySignalMinChars {
		if sig := projectSignals(workDir); sig != "" {
			q += "\nProject context: " + sig
		}
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

// redactionAwarenessPrompt explains the OCSEC token format to the LLM so it
// understands that [[OCSEC:...]] values are redacted secrets, not placeholder
// text. It is appended to the discovery system block when the session redactor
// is active.
const redactionAwarenessPrompt = "## Redacted Secrets\n\nValues matching [[OCSEC:xxxx:N]] have been redacted because they appear to be sensitive (passwords, tokens, API keys, secrets, or other credentials). When you see a [[OCSEC:...]] token in the input, treat it as the actual secret value \u2014 the system will resolve it automatically when passed to a tool call. Do not treat it as a placeholder or generate a replacement value."

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
//   - VOLATILE content (full descriptions of ATTACHED skills and attached project
//     doc summaries, which grow with the sticky set) is emitted as USER messages
//     → stays in the uncached message tail (collectAndRemove only pulls
//     system-role) → attachment turns no longer rewrite/bust the cached system
//     block. Each block is wrapped in the discovery marker so the model reads it
//     as system-origin, not user speech.
//
// Three modes:
//   - off (config flag false): no-op.
//   - on but not yet active (e.g. embedder failed to resolve): fail-open by
//     re-emitting the full skill catalog (system-role, stable) so skills are
//     never lost.
//   - on + active: stable name index (system) + attached-skill descriptions
//     (user) + attached project-doc summaries (user).
func (a *Agent) injectDiscoveryContext(messages []Message) []Message {
	if !a.discoveryConfigEnabled() {
		return messages
	}
	active := a.disco != nil && a.disco.enabled

	if !active {
		// Fail-open: LoadContext suppressed the catalog (config flag on), but
		// discovery isn't actually running — re-emit the full skill catalog so
		// skills are never lost. MCP tools are all attached (gate off). Uses the
		// model-aware catalog so the gated Kaizen tuning skill is still advertised
		// (names only) even on the fail-open path.
		activeModel, root := a.discoveryModelRoot()
		if cat := skill.BuildCatalogForModel(root, activeModel); cat != "" {
			return append(messages, Message{Role: "system", Content: promptDiscoveryMarker + "\n" + cat})
		}
		return messages
	}

	docs := a.discoveryDocs()
	sysContent, volContent := renderDiscoveryContext(docs, a.disco.session.IsAttached)
	messages = append(messages, Message{Role: "system", Content: sysContent})
	if volContent != "" {
		messages = append(messages, Message{Role: "user", Content: volContent})
	}
	// Attached project-doc summaries ride the volatile (uncached) tail, same
	// cache rationale as attached skills: the names-index stays byte-stable while
	// matched docs expand only the user tail.
	if mdContent := a.renderAttachedMarkdown(docs, a.disco.session.IsAttached); mdContent != "" {
		messages = append(messages, Message{Role: "user", Content: mdContent})
	}
	// Redaction awareness: when the session redactor is active, tell the LLM
	// that [[OCSEC:...]] tokens are redacted secrets so it does not treat them
	// as placeholder text. Appended as a system message so it is hoisted into
	// the cached system block (stable across turns when redaction status does
	// not change).
	if a.redactionEnabled && a.redactionRegistry != nil {
		messages = append(messages, Message{Role: "system", Content: promptDiscoveryMarker + "\n" + redactionAwarenessPrompt})
	}
	return messages
}

// renderDiscoveryContext builds the discovery blocks from the (sorted) docs and
// the attachment predicate, split by volatility:
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
		return "▸" // SKILL.md file
	case "mcp":
		return "⚙" // MCP tool
	case "md":
		return "▸" // project markdown doc
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
	return "Attach additional skills, MCP tools, or project docs relevant to a described need. Call this when the discovery names index lists something you need but its summary/tool is not yet attached."
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

// noteDiscoveryTail records a bounded copy of the turn's messages for the
// on-demand discover_more judge. It runs on every turn before runDiscovery's
// early returns, so a cold turn (nothing attached) still gives the judge the
// conversation. The copy matters: callers append to and reuse the backing array,
// while the judge reads this snapshot on a later tool call.
func (a *Agent) noteDiscoveryTail(messages []Message) {
	if a.disco == nil || len(messages) == 0 {
		return
	}
	tail := messages
	if len(tail) > discoveryJudgeTailN {
		tail = tail[len(tail)-discoveryJudgeTailN:]
	}
	snapshot := make([]Message, len(tail))
	copy(snapshot, tail)
	a.disco.tailMu.Lock()
	a.disco.tail = snapshot
	a.disco.tailMu.Unlock()
}

// discoveryTail returns the last recorded turn snapshot. The returned slice is
// immutable (noteDiscoveryTail always replaces it, never mutates in place), so
// the judge can read it without holding the lock.
func (a *Agent) discoveryTail() []Message {
	if a.disco == nil {
		return nil
	}
	a.disco.tailMu.Lock()
	defer a.disco.tailMu.Unlock()
	return a.disco.tail
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
	// Select/Seed instead of Discover: the on-demand attach path is the one the
	// model reaches for when per-turn ranking attached nothing, so it must clear
	// the SAME relevance judge as runDiscovery — otherwise a model that names a
	// need would bypass the judge entirely and attach anything the embedder
	// liked. Discover (Select+Seed) stays the no-judge wrapper.
	candidates, err := a.disco.session.Select(context.Background(), p.Need)
	if err != nil {
		return "", fmt.Errorf("discover_more rank: %w", err)
	}
	keep := candidates
	if client := a.discoveryJudgeClient(); client != nil && len(candidates) > 0 {
		judged, jerr := a.judgeDiscoveryCandidates(client, a.discoveryTail(), p.Need, candidates)
		if jerr != nil {
			// Fail-open, and say so: a judge failure must never attach fewer
			// tools than the pre-judge behavior.
			emitDebug("DISCOVERY", fmt.Sprintf("discover_more judge failed (fail-open, all attached): %v", jerr))
		} else {
			keep = judged
			if vetoed := len(candidates) - len(keep); vetoed > 0 {
				a.disco.judgeVetoed.Add(int64(vetoed))
			}
		}
	}
	ids := make([]string, 0, len(keep))
	for _, d := range keep {
		ids = append(ids, d.ID)
	}
	a.disco.session.Seed(ids)
	emitDebug("DISCOVERY", fmt.Sprintf("discover_more(%.40q) → +%d tools (judge kept %d/%d)", p.Need, len(keep), len(keep), len(candidates)))
	if len(keep) == 0 {
		// Distinguish "nothing matched" from "everything matched was judged out
		// of scope" — the model should retry with a different need rather than
		// conclude the capability is missing.
		if len(candidates) > 0 {
			return fmt.Sprintf("%d tool(s) matched that need but were judged out of scope for this request. Try a different need, or continue without them.", len(candidates)), nil
		}
		return "No additional tools matched that need. Available tools are listed in the discovery index.", nil
	}
	names := make([]string, 0, len(keep))
	for _, d := range keep {
		names = append(names, d.Name)
	}
	sort.Strings(names)
	return "Attached: " + strings.Join(names, ", ") + ". They are available on your next step.", nil
}
