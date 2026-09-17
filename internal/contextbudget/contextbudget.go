// Package contextbudget builds the `/context` token-budget report that both the
// TUI and the web/desktop server render.
//
// The TUI historically computed this report inline from the live *agent.Agent
// (see the old internal/tui handleContextCmd); the web had a separate, far
// thinner implementation backed by GET /api/sessions/{id}/context. This package
// is the single source of truth so both surfaces show the same sections and the
// same numbers. The TUI renders it with RenderText; the server serialises the
// same Report as JSON for the SPA.
//
// Build is read-only. Callers that read a live agent which another goroutine can
// mutate (the web/desktop server) must serialise that read themselves — the TUI
// is single-goroutine and does not need to.
package contextbudget

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/knowledge"
	"github.com/u007/ocode/internal/memory"
	"github.com/u007/ocode/internal/plugins"
	"github.com/u007/ocode/internal/skill"
)

// Input carries everything Build needs. Agent may be nil (a report is still
// produced from the transcript + filesystem, marked by Notes).
type Input struct {
	Agent    *agent.Agent
	Messages []agent.Message
	// WorkDir is the project root the report is anchored to. Ambient files,
	// skills, plugins, memory and the knowledge bundle all resolve against it.
	// Empty falls back to the process working directory.
	WorkDir string
	Config  *config.Config
	// Model is the effective model name used for the context-window display.
	// Empty falls back to the agent client model, then the config model.
	Model string
	// ContextTokens/ContextSource override the derived context estimate when the
	// caller holds a more authoritative value. The TUI tracks provider-reported
	// input tokens in its own state and passes them here.
	ContextTokens int64
	ContextSource string
	// Telemetry overrides the message-derived usage roll-up when the caller
	// tracks live side-channel usage the transcript does not carry (the TUI's
	// session telemetry). Nil derives from Messages.
	Telemetry *Telemetry
}

// Row is one labelled line in a section. Value is the pre-formatted right-hand
// side (e.g. "~1.2k tok", "12", "3/8"). Lines are extra indented detail — either
// nested rows or a verbatim multi-line dump when Raw is set.
type Row struct {
	Label string   `json:"label"`
	Value string   `json:"value,omitempty"`
	Lines []string `json:"lines,omitempty"`
	Raw   bool     `json:"raw,omitempty"`
	// Subhead marks a row that is really an unindented sub-heading inside the
	// section (e.g. "Corpus (names-index)"), rendered with a leading blank line.
	Subhead bool `json:"subhead,omitempty"`
}

// Section is a titled group of rows.
type Section struct {
	Title string `json:"title"`
	Note  string `json:"note,omitempty"`
	Rows  []Row  `json:"rows"`
}

// Report is the full context-budget breakdown.
type Report struct {
	Model    string    `json:"model"`
	Sections []Section `json:"sections"`
	// Notes flags conditions that make the report incomplete (no agent, turn in
	// flight, …).
	Notes []string `json:"notes,omitempty"`
}

// Telemetry is the cumulative token/spend roll-up.
type Telemetry struct {
	InputTokens  int64    `json:"input_tokens"`
	OutputTokens int64    `json:"output_tokens"`
	TotalTokens  int64    `json:"total_tokens"`
	CachedTokens int64    `json:"cached_tokens"`
	Spend        *float64 `json:"spend,omitempty"`
}

// HasData reports whether any usage was recorded.
func (t Telemetry) HasData() bool {
	return t.InputTokens > 0 || t.OutputTokens > 0 || t.TotalTokens > 0 || t.CachedTokens > 0 || t.Spend != nil
}

// Build assembles the report. It never mutates the agent or the filesystem.
func Build(in Input) Report {
	cfg := in.Config
	workDir := in.WorkDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	ag := in.Agent

	rep := Report{Model: in.Model}
	if rep.Model == "" && ag != nil {
		if c := ag.Client(); c != nil {
			rep.Model = c.GetModel()
		}
	}
	if rep.Model == "" && cfg != nil {
		rep.Model = cfg.Model
	}
	if ag == nil {
		rep.Notes = append(rep.Notes, "no live agent — values are estimated from the persisted transcript")
	}

	discoveryOn := cfg != nil && cfg.Ocode.Discovery.Enabled && ag != nil

	// ── Base Prompt ────────────────────────────────────────────────────────
	baseTotal := 0
	base := Section{Title: "Base Prompt"}
	if ag != nil {
		for _, msg := range ag.BasePromptMessages() {
			if !strings.Contains(msg.Content, "[ocode:environment]") {
				continue
			}
			tok := estimateTok(msg.Content)
			baseTotal += tok
			base.Rows = append(base.Rows, Row{Label: "Environment", Value: "~" + formatTok(tok) + " tok"})
			break
		}

		modePrompt := ag.Mode().SystemPrompt()
		modeTok := estimateTok(modePrompt)
		baseTotal += modeTok
		modeLabel := fmt.Sprintf("Mode (%s)", ag.Mode().String())
		base.Rows = append(base.Rows, Row{Label: modeLabel, Value: "~" + formatTok(modeTok) + " tok"})

		var providerModel, providerPrompt string
		if ag.Client() != nil {
			providerModel = fmt.Sprintf("%s/%s", ag.Client().GetProvider(), ag.Client().GetModel())
			providerPrompt = ag.ModelFamilyPrompt()
		}
		if providerPrompt != "" {
			ppTok := estimateTok(providerPrompt)
			baseTotal += ppTok
			base.Rows = append(base.Rows, Row{
				Label: fmt.Sprintf("Provider prompt (%s)", providerModel),
				Value: "~" + formatTok(ppTok) + " tok",
				Lines: strings.Split(providerPrompt, "\n"),
				Raw:   true,
			})
		}
	}

	// Ambient context files (always-on briefing), resolved against WorkDir.
	ambientFiles := []string{"AGENTS.md", "CLAUDE.md", ".cursorrules"}
	rulesDir := filepath.Join(workDir, ".opencode", "rules")
	if entries, err := os.ReadDir(rulesDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
				ambientFiles = append(ambientFiles, filepath.Join(".opencode", "rules", e.Name()))
			}
		}
	}
	anyAmbient := false
	for _, f := range ambientFiles {
		content, err := os.ReadFile(filepath.Join(workDir, f))
		if err != nil {
			continue
		}
		anyAmbient = true
		tok := estimateTok(string(content))
		baseTotal += tok
		base.Rows = append(base.Rows, Row{Label: filepath.Base(f), Value: "~" + formatTok(tok) + " tok"})
	}
	if !anyAmbient {
		base.Rows = append(base.Rows, Row{Label: "(no ambient files found)"})
	}

	refCatalog := agent.BuildReferenceCatalog(enabledPluginMap(cfg))
	if refCatalog != "" {
		refTok := estimateTok(refCatalog)
		baseTotal += refTok
		base.Rows = append(base.Rows, Row{
			Label: "Reference catalog",
			Value: "~" + formatTok(refTok) + " tok",
			Lines: strings.Split(refCatalog, "\n"),
			Raw:   true,
		})
	}

	if ag != nil {
		if kind, path := ag.ModelContextInfo(); kind != "" {
			mcTok := estimateTok(ag.ModelContextContent())
			baseTotal += mcTok
			source := "embedded:" + path
			if kind == "file" {
				source = "disk:" + path
			}
			mcModel := ""
			if ag.Client() != nil {
				mcModel = ag.Client().GetModel()
			}
			base.Rows = append(base.Rows, Row{
				Label: fmt.Sprintf("Model ctx (%s)", mcModel),
				Value: fmt.Sprintf("~%s tok (%s)", formatTok(mcTok), source),
			})
		}
	}

	for _, p := range plugins.LoadPluginsForProject(enabledPluginMap(cfg), workDir) {
		if p.Instructions == "" {
			continue
		}
		tok := estimateTok(p.Instructions)
		baseTotal += tok
		base.Rows = append(base.Rows, Row{Label: "Plugin: " + p.Name, Value: "~" + formatTok(tok) + " tok"})
	}
	base.Rows = append(base.Rows, Row{Label: "Base subtotal", Value: "~" + formatTok(baseTotal) + " tok"})
	rep.Sections = append(rep.Sections, base)

	// ── Knowledge Bundle (OKF) ─────────────────────────────────────────────
	kb := Section{Title: "Knowledge Bundle"}
	switch {
	case ag != nil && ag.DocPromptEnabled():
		if bundle, ok := knowledge.DetectBundle(workDir); ok {
			kb.Rows = append(kb.Rows, Row{Label: "Active (context-agent access only, 0 prompt tokens)"})
			kb.Rows = append(kb.Rows, Row{Label: "Source", Value: bundle.Root})
		} else {
			kb.Rows = append(kb.Rows, Row{Label: "Inactive (no OKF bundle at docs/ — run /docs init)"})
		}
	default:
		kb.Rows = append(kb.Rows, Row{Label: "Disabled (/docs off — run /docs on to enable)"})
	}
	rep.Sections = append(rep.Sections, kb)

	// ── Memory ─────────────────────────────────────────────────────────────
	mem := Section{Title: "Memory"}
	if ag != nil && ag.MemoryEnabled() {
		if snap, err := memory.Status(workDir); err == nil {
			for _, ms := range []struct {
				title string
				s     memory.Scope
			}{
				{"Project memory", snap.Project},
				{"User memory", snap.User},
				{"Global history", snap.Global},
			} {
				status := "present"
				if !ms.s.Present {
					status = "not found"
				}
				mem.Rows = append(mem.Rows, Row{Label: ms.title, Value: status, Lines: []string{ms.s.Path}})
			}
		} else {
			mem.Rows = append(mem.Rows, Row{Label: "Error", Value: err.Error()})
		}
	} else {
		mem.Rows = append(mem.Rows, Row{Label: "Disabled (/mem off — run /mem on to enable)"})
	}
	rep.Sections = append(rep.Sections, mem)

	// ── Tools ──────────────────────────────────────────────────────────────
	toolsTitle := "Tools (injected every request)"
	if discoveryOn {
		toolsTitle = "Tools (built-in always injected, MCP gated by discovery)"
	}
	tools := Section{Title: toolsTitle}
	toolsTotal := 0
	if ag != nil {
		allDefs := ag.GetToolDefinitions()
		mcpSet := make(map[string]struct{})
		for _, name := range ag.MCPToolNames() {
			mcpSet[name] = struct{}{}
		}
		var serverNames []string
		if cfg != nil {
			serverNames = make([]string, 0, len(cfg.MCP))
			for name := range cfg.MCP {
				serverNames = append(serverNames, name)
			}
		}
		sort.Strings(serverNames)

		grouped, builtinDefs := GroupMCPToolDefs(allDefs, mcpSet, serverNames)
		builtinTok := 0
		for _, def := range builtinDefs {
			builtinTok += definitionTokens(def)
		}
		toolsTotal += builtinTok
		builtinLabel := fmt.Sprintf("Built-in (%d tools)", len(builtinDefs))
		tools.Rows = append(tools.Rows, Row{Label: builtinLabel, Value: "~" + formatTok(builtinTok) + " tok"})

		if len(serverNames) == 0 {
			tools.Rows = append(tools.Rows, Row{Label: "MCP: (none)", Value: "~0 tok"})
		}
		for _, srv := range serverNames {
			defs, ok := grouped[srv]
			if !ok {
				continue
			}
			srvTok := 0
			var lines []string
			for _, def := range defs {
				tok := definitionTokens(def)
				srvTok += tok
				fullName, _ := def["name"].(string)
				shortName := strings.TrimPrefix(fullName, srv+"_")
				lines = append(lines, fmt.Sprintf("%-24s ~%s tok", shortName, formatTok(tok)))
			}
			toolsTotal += srvTok
			label := fmt.Sprintf("MCP: %s  %d tools", srv, len(defs))
			tools.Rows = append(tools.Rows, Row{Label: label, Value: "~" + formatTok(srvTok) + " tok", Lines: lines})
		}
		tools.Rows = append(tools.Rows, Row{Label: "Subtotal", Value: "~" + formatTok(toolsTotal) + " tok"})
	}
	rep.Sections = append(rep.Sections, tools)

	injectedTotal := baseTotal + toolsTotal

	// ── Skill catalog (names-index / pre-injected) ─────────────────────────
	catalogNote := "Skill catalog (pre-injected)"
	if discoveryOn {
		catalogNote = "Skill catalog (not pre-injected — discovery active)"
	}
	catalog := Section{Title: catalogNote}
	catalogTok := 0
	skills := skill.LoadSkillsForModel(workDir, rep.Model)
	if len(skills) == 0 {
		catalog.Rows = append(catalog.Rows, Row{Label: "(none found)"})
	} else {
		for _, s := range skills {
			line := "- " + s.Name
			if s.Description != "" {
				line += ": " + s.Description
			}
			if s.WhenToUse != "" {
				line += " When to use: " + s.WhenToUse
			}
			lineTok := estimateTok(line)
			catalogTok += lineTok
			catalog.Rows = append(catalog.Rows, Row{Label: s.Name, Value: "~" + formatTok(lineTok) + " tok"})
		}
	}
	// Kaizen digest: per-model tuned directives force-injected into the base
	// prompt, unconditionally (unlike the catalog). "" for a non-matching model.
	var kaizenRows []Row
	for _, s := range skill.KaizenSkillsForModel(workDir, rep.Model) {
		if s.Digest == "" {
			continue
		}
		label := fmt.Sprintf("%s → %s", s.Name, s.TunedFor)
		kaizenRows = append(kaizenRows, Row{Label: label, Value: "~" + formatTok(estimateTok(s.Digest)) + " tok"})
	}
	if len(kaizenRows) > 0 {
		catalog.Rows = append(catalog.Rows, Row{Label: "Model directives (kaizen digest, force-injected)", Subhead: true})
		catalog.Rows = append(catalog.Rows, kaizenRows...)
	}
	if digest := skill.KaizenDigestBlock(workDir, rep.Model); digest != "" {
		injectedTotal += estimateTok(digest)
	}
	if !discoveryOn {
		injectedTotal += catalogTok
	}
	catalog.Rows = append(catalog.Rows, Row{Label: "Injected per request", Value: "~" + formatTok(injectedTotal) + " tok"})
	rep.Sections = append(rep.Sections, catalog)

	// ── Skills (full contents available on demand) ─────────────────────────
	sk := Section{Title: "Skills (full contents available on demand, not pre-injected)"}
	allSkills := skill.LoadSkillsForRoot(workDir)
	if len(allSkills) == 0 {
		sk.Rows = append(sk.Rows, Row{Label: "(none found)"})
	} else {
		shown := allSkills
		extra := 0
		if len(allSkills) > 5 {
			shown = allSkills[:5]
			extra = len(allSkills) - 5
		}
		skillTotal := 0
		for _, s := range allSkills {
			skillTotal += estimateTok(s.Content)
		}
		for _, s := range shown {
			sk.Rows = append(sk.Rows, Row{Label: s.Name, Value: "~" + formatTok(estimateTok(s.Content)) + " tok"})
		}
		if extra > 0 {
			moreLabel := fmt.Sprintf("... +%d more (%d total)", extra, len(allSkills))
			sk.Rows = append(sk.Rows, Row{Label: moreLabel, Value: "~" + formatTok(skillTotal) + " tok available"})
		}
	}
	rep.Sections = append(rep.Sections, sk)

	// ── Session Messages ───────────────────────────────────────────────────
	sess := Section{Title: "Session Messages"}

	ctxTokens, ctxSource := in.ContextTokens, in.ContextSource
	if ctxTokens == 0 {
		cpt := 4.0
		if ag != nil {
			cpt = ag.CharsPerToken()
		}
		tok, src := agent.CurrentContextEstimate(in.Messages, cpt)
		ctxTokens, ctxSource = int64(tok), src
	}
	if ctxTokens > 0 {
		if window, ok := ContextWindow(rep.Model); ok {
			sess.Rows = append(sess.Rows, Row{
				Label: "Context",
				Value: fmt.Sprintf("%d / %d (%s)  %s", ctxTokens, window, formatPercent(ctxTokens, window), ctxSource),
			})
		} else {
			sess.Rows = append(sess.Rows, Row{
				Label: "Context",
				Value: fmt.Sprintf("%d tok  %s", ctxTokens, ctxSource),
			})
		}
	} else {
		sess.Rows = append(sess.Rows, Row{Label: "Context", Value: "n/a"})
	}

	lastIn, lastOut, lastTotal := LatestRequestUsage(in.Messages)
	if lastTotal > 0 {
		sess.Rows = append(sess.Rows, Row{
			Label: "Last req",
			Value: fmt.Sprintf("In %d  Out %d  Total %d", lastIn, lastOut, lastTotal),
		})
	}

	telemetry := Telemetry{}
	if in.Telemetry != nil {
		telemetry = *in.Telemetry
	} else {
		telemetry = AggregateTelemetry(in.Messages)
	}
	if telemetry.HasData() {
		cacheRate := formatPercent(telemetry.CachedTokens, telemetry.InputTokens+telemetry.CachedTokens)
		value := fmt.Sprintf("In %d  Cache %d (%s)  Out %d",
			telemetry.InputTokens, telemetry.CachedTokens, cacheRate, telemetry.OutputTokens)
		row := Row{Label: "Usage", Value: value}
		if telemetry.Spend != nil {
			row.Lines = []string{fmt.Sprintf("$%.4f", *telemetry.Spend)}
		}
		sess.Rows = append(sess.Rows, row)
	} else {
		sess.Rows = append(sess.Rows, Row{Label: "Usage", Value: "n/a"})
	}
	rep.Sections = append(rep.Sections, sess)

	// ── Discovery ──────────────────────────────────────────────────────────
	if discoveryOn {
		st := ag.DiscoveryStatus()
		mcpAttached, mcpTotal, gatedToks, indexToks := ag.DiscoveryGatedTokens()
		disc := Section{Title: "Discovery — [ocode:discovery] injected block"}
		disc.Rows = append(disc.Rows, Row{Label: "Backend/model", Value: strings.TrimSpace(st.Backend + " " + st.Model)})
		if !st.Active && st.InitErr != "" {
			disc.Rows = append(disc.Rows, Row{Label: "Status", Value: "fail-open: " + st.InitErr})
		}
		disc.Rows = append(disc.Rows, Row{Label: "Corpus (names-index, stable — injected every turn)", Subhead: true})
		disc.Rows = append(disc.Rows, Row{Label: "Skills in index", Value: strconv.Itoa(len(st.AllSkills))})
		disc.Rows = append(disc.Rows, Row{Label: "MCP tools in index", Value: strconv.Itoa(len(st.AllMCP))})
		disc.Rows = append(disc.Rows, Row{Label: "Project docs in index", Value: strconv.Itoa(len(st.AllMD))})
		if st.MDPending > 0 {
			disc.Rows = append(disc.Rows, Row{Label: "Docs pending summarization", Value: strconv.Itoa(st.MDPending)})
		}
		disc.Rows = append(disc.Rows, Row{Label: "Attached to volatile tail (per-turn, grows with sticky set)", Subhead: true})
		attachedSkills := Row{Label: "Skills attached", Value: fmt.Sprintf("%d/%d", len(st.AttachedSkills), st.SkillTotal)}
		for _, name := range st.AttachedSkills {
			attachedSkills.Lines = append(attachedSkills.Lines, "- "+name)
		}
		disc.Rows = append(disc.Rows, attachedSkills)
		disc.Rows = append(disc.Rows, Row{Label: "MCP tools attached", Value: fmt.Sprintf("%d/%d", mcpAttached, mcpTotal)})

		attachedMD := Row{Label: "Project docs attached", Value: fmt.Sprintf("%d/%d", len(st.AttachedMD), len(st.AllMD))}
		for _, name := range st.AttachedMD {
			attachedMD.Lines = append(attachedMD.Lines, "- "+name)
		}
		disc.Rows = append(disc.Rows, attachedMD)

		const queryEmbedToks = 64 // rough per-turn query embedding cost
		net := gatedToks - indexToks - queryEmbedToks
		if net < 0 {
			net = 0
		}
		disc.Rows = append(disc.Rows, Row{Label: "Efficiency", Subhead: true})
		disc.Rows = append(disc.Rows, Row{Label: "Context saved (gross)", Value: "~" + formatTok(gatedToks) + " tok"})
		disc.Rows = append(disc.Rows, Row{Label: "Context saved (net)", Value: "~" + formatTok(net) + " tok"})
		disc.Rows = append(disc.Rows, Row{Label: "MCP tools not attached", Value: strconv.Itoa(mcpTotal - mcpAttached)})
		rep.Sections = append(rep.Sections, disc)
	}

	return rep
}

// GroupMCPToolDefs separates tool definitions into per-server MCP groups and the
// built-in remainder. Exported so the TUI, which documents /context grouping, can
// share one implementation.
func GroupMCPToolDefs(
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

// AggregateTelemetry rolls up per-message usage into a single Telemetry. It
// mirrors the TUI sidebar's accounting: NormalizedPromptTokens (cache reads are
// counted once, as cached tokens) and spend summed across messages.
func AggregateTelemetry(messages []agent.Message) Telemetry {
	var t Telemetry
	for _, msg := range messages {
		messageTotal := int64(0)
		if msg.Usage != nil {
			promptTokens := msg.Usage.NormalizedPromptTokens()
			t.InputTokens += promptTokens
			messageTotal += promptTokens
			if msg.Usage.CompletionTokens != nil {
				t.OutputTokens += *msg.Usage.CompletionTokens
				messageTotal += *msg.Usage.CompletionTokens
			}
			if msg.Usage.CacheReadTokens != nil {
				t.CachedTokens += *msg.Usage.CacheReadTokens
			}
			if msg.Usage.CacheWriteTokens != nil {
				t.CachedTokens += *msg.Usage.CacheWriteTokens
			}
			if msg.Usage.TotalTokens != nil {
				messageTotal = *msg.Usage.TotalTokens
			}
			t.TotalTokens += messageTotal
		}
		if msg.Spend != nil {
			if t.Spend == nil {
				t.Spend = new(float64)
			}
			*t.Spend += *msg.Spend
		}
	}
	return t
}

// LatestRequestUsage returns the usage of the most recent assistant message that
// carried any. total falls back to input+output when the provider omits it.
func LatestRequestUsage(messages []agent.Message) (input, output, total int64) {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Usage == nil {
			continue
		}
		u := messages[i].Usage
		if u.PromptTokens != nil {
			input = *u.PromptTokens
		}
		if u.CompletionTokens != nil {
			output = *u.CompletionTokens
		}
		if u.TotalTokens != nil {
			total = *u.TotalTokens
		} else {
			total = input + output
		}
		return
	}
	return 0, 0, 0
}

// ContextWindow resolves the model's context window, falling back to a small
// table of common models when the registry has no entry.
func ContextWindow(modelName string) (int64, bool) {
	if mw := agent.ModelWindow(modelName); mw > 0 {
		return mw, true
	}
	switch modelName {
	case "gpt-4o", "gpt-4o-mini", "o1-preview":
		return 128000, true
	case "claude-3-5-sonnet-20241022", "claude-3-opus-20240229", "claude-3-haiku-20240307":
		return 200000, true
	case "gemini-1.5-pro":
		return 1048576, true
	case "gemini-1.5-flash":
		return 1000000, true
	default:
		return 0, false
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

func enabledPluginMap(cfg *config.Config) map[string]bool {
	if cfg == nil || len(cfg.Plugins) == 0 {
		return nil
	}
	enabled := make(map[string]bool, len(cfg.Plugins))
	for name, p := range cfg.Plugins {
		enabled[name] = p.Enabled
	}
	return enabled
}

func definitionTokens(def map[string]interface{}) int {
	raw, err := json.Marshal(def)
	if err != nil {
		return 0
	}
	return estimateTok(string(raw))
}

// estimateTok approximates token count as len(s)/4.
func estimateTok(s string) int {
	return len(s) / 4
}

// formatTok formats an integer token count compactly, e.g. 1234 → "1.2k".
func formatTok(n int) string {
	switch {
	case n >= 1000000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return strconv.Itoa(n)
	}
}

// formatPercent renders used/total as a percentage with one decimal.
func formatPercent(used, total int64) string {
	if total <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%.1f%%", float64(used)/float64(total)*100)
}
