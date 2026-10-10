package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// Read-only tools the Pulse assistant uses to look at other sessions and
// terminals (docs/concepts/pulse-assistant.md). Phase 3 is read-only on
// purpose: nothing here sends, approves or mutates. Every result is a JSON
// string, every list is sorted deterministically, and every result is capped
// at pulseToolResultCap with a trailing "truncated": true when clipped.

const (
	// pulseToolResultCap bounds one tool result so a busy dashboard cannot
	// blow the assistant's context window.
	pulseToolResultCap = 24 * 1024

	pulseSessionReadDefault = 30
	pulseSessionReadMax     = 200
	// pulseMessageRuneBudget / pulseCallArgsRuneBudget clip one message body and
	// one tool call's arguments in session_read output.
	pulseMessageRuneBudget  = 2000
	pulseCallArgsRuneBudget = 200

	pulseTerminalDefaultLines = 100
	pulseTerminalMaxLines     = 1000
	// pulseTerminalLineRuneBudget clips one terminal line (a progress bar can
	// emit megabytes without a newline).
	pulseTerminalLineRuneBudget = 2000
	// pulseTerminalWindowBytes is how much of a terminal's history one read covers:
	// the newest bytes for a tail read, the oldest for a head read. Lines beyond it
	// are not reachable through terminal_read.
	pulseTerminalWindowBytes = 1 << 20

	// pulseRunResultRuneBudget clips a sub-agent run's result in agent_runs.
	pulseRunResultRuneBudget = 500
)

// pulseTool is one read-only assistant tool. It implements tool.Tool.
type pulseTool struct {
	name  string
	desc  string
	props map[string]any
	run   func(args json.RawMessage) (string, error)
	// ask marks a write tool: it gets NO allow rule, so each call raises a
	// permission ask for the operator.
	ask bool
}

func (t *pulseTool) Name() string        { return t.name }
func (t *pulseTool) Description() string { return t.desc }
func (t *pulseTool) Parallel() bool      { return true }

func (t *pulseTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        t.name,
		"description": t.desc,
		"parameters": map[string]interface{}{
			"type":       "object",
			"properties": t.props,
		},
	}
}

func (t *pulseTool) Execute(args json.RawMessage) (string, error) { return t.run(args) }

// pulseTools builds the assistant's whole tool set, bound to this handler.
func (h *Handler) pulseTools() []*pulseTool {
	tools := h.pulseReadTools()
	tools = append(tools, h.pulseWriteTools()...)
	return append(tools, h.pulseMemoryTools()...)
}

func (h *Handler) pulseReadTools() []*pulseTool {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	integer := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	return []*pulseTool{
		{
			name:  "pulse_board",
			desc:  "List the dashboard rows (one per top-level session across all projects): status, project, title, current task or pending ask, todo progress. scope \"live\" (default) is what is active now; \"all\" also includes idle sessions from the last 7 days.",
			props: map[string]any{"scope": str(`"live" (default) or "all"`)},
			run:   h.pulseBoardTool,
		},
		{
			name: "session_read",
			desc: fmt.Sprintf("Read a session's transcript. Returns the last N messages (default %d, max %d), each clipped to %d characters, with absolute message indexes. Use search to return only messages containing a string (case-insensitive). Child sessions (ids containing _child_) are readable.", pulseSessionReadDefault, pulseSessionReadMax, pulseMessageRuneBudget),
			props: map[string]any{
				"session_id": str("Session id, as shown on the board"),
				"last":       integer(fmt.Sprintf("How many of the most recent messages to return (1-%d, default %d)", pulseSessionReadMax, pulseSessionReadDefault)),
				"search":     str("Only return messages whose content contains this text (case-insensitive)"),
			},
			run: h.pulseSessionReadTool,
		},
		{
			name:  "session_recap",
			desc:  "Produce the recap of a session (what was asked, done, found, decided, next). Runs a model call, so prefer session_read for a quick look.",
			props: map[string]any{"session_id": str("Session id, as shown on the board")},
			run:   h.pulseSessionRecapTool,
		},
		{
			name:  "terminal_tabs",
			desc:  "List the open terminal tabs, grouped by project. Each terminal says whether it is live and whether a program (command) is running in it.",
			props: map[string]any{},
			run:   h.pulseTerminalTabsTool,
		},
		{
			name: "terminal_read",
			desc: fmt.Sprintf("Read N lines (default %d, max %d) of a terminal's output, ANSI escape codes removed. By default the last lines; offset skips that many lines back from the end, so repeated calls page backwards. head=true reads from the start of the output instead, and offset then skips forward from the start. Only the newest or oldest %d KiB of history is reachable. Each call reads the output as it is now, so on a terminal that is still writing, paging can overlap or skip lines. Terminal ids come from terminal_tabs.", pulseTerminalDefaultLines, pulseTerminalMaxLines, pulseTerminalWindowBytes>>10),
			props: map[string]any{
				"terminal_id": str("Terminal id from terminal_tabs"),
				"lines":       integer(fmt.Sprintf("How many lines to return (1-%d, default %d)", pulseTerminalMaxLines, pulseTerminalDefaultLines)),
				"offset":      integer("Lines to skip from the end (or from the start with head), default 0"),
				"head":        map[string]any{"type": "boolean", "description": "true to read from the start of the output, default false (the end)"},
			},
			run: h.pulseTerminalReadTool,
		},
		{
			name:  "agent_runs",
			desc:  "List live sub-agent runs (task dispatches) as a nested tree, grouped by session. Optionally restrict to one session. Run transcripts are omitted; use session_read on the parent for context.",
			props: map[string]any{"session_id": str("Only this session's runs (default: every session that has runs)")},
			run:   h.pulseAgentRunsTool,
		},
	}
}

// decodePulseArgs strictly decodes tool arguments: an unknown field is a model
// typo that should be reported, not silently ignored.
func decodePulseArgs(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

func marshalPulse(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// pulseCapped renders build(n, truncated) for the largest n <= total whose JSON
// fits pulseToolResultCap. build must keep the n items that matter most (it owns
// the ordering) and set the trailing "truncated" field from its second argument.
// An error is returned when not even zero items fit, rather than an empty result.
func pulseCapped(total int, build func(n int, truncated bool) any) (string, error) {
	full, err := marshalPulse(build(total, false))
	if err != nil {
		return "", fmt.Errorf("encode result: %w", err)
	}
	if len(full) <= pulseToolResultCap {
		return string(full), nil
	}
	lo, hi := 0, total-1
	var best []byte
	for lo <= hi {
		mid := (lo + hi) / 2
		out, err := marshalPulse(build(mid, true))
		if err != nil {
			return "", fmt.Errorf("encode result: %w", err)
		}
		if len(out) <= pulseToolResultCap {
			best = out
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if best == nil {
		return "", fmt.Errorf("result exceeds %d bytes even with no items", pulseToolResultCap)
	}
	return string(best), nil
}

// --- pulse_board ---

func (h *Handler) pulseBoardTool(raw json.RawMessage) (string, error) {
	var args struct {
		Scope string `json:"scope"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	scope := args.Scope
	if scope == "" {
		scope = pulseScopeLive
	}
	if scope != pulseScopeLive && scope != pulseScopeAll {
		return "", fmt.Errorf("invalid scope %q (want %q or %q)", scope, pulseScopeLive, pulseScopeAll)
	}
	now := time.Now()
	rows := buildPulseRows(h.gatherPulseInputs(scope, now), scope, now)
	return pulseCapped(len(rows), func(n int, truncated bool) any {
		return struct {
			Items     []PulseRow `json:"items"`
			Truncated bool       `json:"truncated,omitempty"`
		}{Items: append([]PulseRow{}, rows[:n]...), Truncated: truncated}
	})
}

// --- session_read ---

type pulseReadCall struct {
	Name string `json:"name"`
	Args string `json:"args"`
}

type pulseReadMessage struct {
	Index     int             `json:"index"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	ToolCalls []pulseReadCall `json:"tool_calls,omitempty"`
}

// pulseSessionTranscript loads a session's messages for the assistant. Live
// state wins over disk: a resident session's in-memory transcript is fresher
// than its last persisted copy. The read is non-blocking (TryLock) because a
// running turn holds the session lock for its whole duration; in that case the
// persisted copy is used.
func (h *Handler) pulseSessionTranscript(id string) (title, project string, msgs []agent.Message, err error) {
	if id == "" {
		return "", "", nil, fmt.Errorf("session_id is required")
	}
	if isPulseSession(id) {
		return "", "", nil, fmt.Errorf("refusing to read the assistant's own session %q", id)
	}
	entry, err := h.sessions.Resolve(id)
	if err != nil {
		return "", "", nil, fmt.Errorf("session %q not found", id)
	}
	project = entry.ProjectRoot

	live := false
	if as := h.lookupAgentSession(id); as != nil && as.mu.TryLock() {
		msgs = append([]agent.Message(nil), as.messages...)
		as.mu.Unlock()
		live = true
	}
	if !live {
		s, err := session.LoadForDir(project, id)
		if err != nil {
			return "", "", nil, fmt.Errorf("load session %q: %w", id, err)
		}
		msgs = s.Messages
	}
	t, found, terr := session.TitleForDir(project, id)
	if terr != nil {
		log.Printf("serve: pulse session_read title for %s in %s: %v", id, project, terr)
	} else if found {
		title = t
	}
	return title, project, msgs, nil
}

func (h *Handler) pulseSessionReadTool(raw json.RawMessage) (string, error) {
	var args struct {
		SessionID string `json:"session_id"`
		Last      *int   `json:"last"`
		Search    string `json:"search"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	last := pulseSessionReadDefault
	if args.Last != nil {
		last = *args.Last
		if last < 1 || last > pulseSessionReadMax {
			return "", fmt.Errorf("last must be 1-%d, got %d", pulseSessionReadMax, last)
		}
	}
	title, project, msgs, err := h.pulseSessionTranscript(args.SessionID)
	if err != nil {
		return "", err
	}

	needle := strings.ToLower(args.Search)
	selected := make([]pulseReadMessage, 0, last)
	for i, m := range msgs {
		if needle != "" && !strings.Contains(strings.ToLower(m.Content), needle) {
			continue
		}
		pm := pulseReadMessage{Index: i, Role: m.Role, Content: truncateRunes(m.Content, pulseMessageRuneBudget)}
		for _, c := range m.ToolCalls {
			pm.ToolCalls = append(pm.ToolCalls, pulseReadCall{Name: c.Function.Name, Args: truncateRunes(c.Function.Arguments, pulseCallArgsRuneBudget)})
		}
		selected = append(selected, pm)
	}
	if len(selected) > last {
		selected = selected[len(selected)-last:]
	}
	// Under the byte cap the OLDEST messages are dropped first: the end of a
	// transcript is what the operator is asking about.
	return pulseCapped(len(selected), func(n int, truncated bool) any {
		return struct {
			SessionID   string             `json:"session_id"`
			ProjectPath string             `json:"project_path"`
			Title       string             `json:"title"`
			Messages    []pulseReadMessage `json:"messages"`
			Truncated   bool               `json:"truncated,omitempty"`
		}{args.SessionID, project, title, append([]pulseReadMessage{}, selected[len(selected)-n:]...), truncated}
	})
}

// --- session_recap ---

func (h *Handler) pulseSessionRecapTool(raw json.RawMessage) (string, error) {
	var args struct {
		SessionID string `json:"session_id"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	if args.SessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}
	if isPulseSession(args.SessionID) {
		return "", fmt.Errorf("refusing to recap the assistant's own session %q", args.SessionID)
	}
	text, err := pulseRecapWithTimeout(pulseRecapTimeout, func(ctx context.Context) (string, error) {
		return h.recapSessionCtx(ctx, args.SessionID)
	})
	if err != nil {
		return "", fmt.Errorf("recap %q: %w", args.SessionID, err)
	}
	// A recap is model output of unbounded length; clip it, front-first, by
	// runes so the cap holds and the JSON stays valid.
	runes := []rune(text)
	return pulseCapped(len(runes), func(n int, truncated bool) any {
		return struct {
			SessionID string `json:"session_id"`
			Recap     string `json:"recap"`
			Truncated bool   `json:"truncated,omitempty"`
		}{args.SessionID, string(runes[:n]), truncated}
	})
}

// --- terminal_tabs / terminal_read ---

// pulseTabTerminal is one open tab, joined with its live process when the
// terminal has one: Live says a shell is running, Running says a program other
// than the shell is in the foreground, Command names it.
type pulseTabTerminal struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	OSCTitle string `json:"osc_title,omitempty"`
	Live     bool   `json:"live"`
	Running  bool   `json:"running"`
	Command  string `json:"command,omitempty"`
}

type pulseTabsProject struct {
	Project   string             `json:"project"`
	Terminals []pulseTabTerminal `json:"terminals"`
}

func (h *Handler) pulseTerminalTabsTool(raw json.RawMessage) (string, error) {
	if err := decodePulseArgs(raw, &struct{}{}); err != nil {
		return "", err
	}
	if h.termTabsStore == nil {
		return "", fmt.Errorf("terminal tab store is unavailable")
	}
	all := h.termTabsStore.All()
	keys := make([]string, 0, len(all))
	for k, pt := range all {
		if len(pt.Terminals) > 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	live := make(map[string]pulseTerminalRow)
	for _, row := range h.pulseTerminalRows() {
		live[row.ID] = row
	}
	projects := make([]pulseTabsProject, 0, len(keys))
	for _, k := range keys {
		terms := make([]pulseTabTerminal, 0, len(all[k].Terminals))
		for _, t := range all[k].Terminals {
			pt := pulseTabTerminal{ID: t.ID, Title: t.Title, OSCTitle: t.OSCTitle}
			if row, ok := live[t.ID]; ok {
				pt.Live = true
				pt.Running = row.Running
				pt.Command = row.Command
			}
			terms = append(terms, pt)
		}
		sort.Slice(terms, func(i, j int) bool { return terms[i].ID < terms[j].ID })
		projects = append(projects, pulseTabsProject{Project: k, Terminals: terms})
	}
	return pulseCapped(len(projects), func(n int, truncated bool) any {
		return struct {
			Projects  []pulseTabsProject `json:"projects"`
			Truncated bool               `json:"truncated,omitempty"`
		}{append([]pulseTabsProject{}, projects[:n]...), truncated}
	})
}

func (h *Handler) pulseTerminalReadTool(raw json.RawMessage) (string, error) {
	var args struct {
		TerminalID string `json:"terminal_id"`
		Lines      *int   `json:"lines"`
		Offset     int    `json:"offset"`
		Head       bool   `json:"head"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	if args.TerminalID == "" {
		return "", fmt.Errorf("terminal_id is required")
	}
	n := pulseTerminalDefaultLines
	if args.Lines != nil {
		n = *args.Lines
		if n < 1 || n > pulseTerminalMaxLines {
			return "", fmt.Errorf("lines must be 1-%d, got %d", pulseTerminalMaxLines, n)
		}
	}
	if args.Offset < 0 {
		return "", fmt.Errorf("offset must be non-negative, got %d", args.Offset)
	}
	project, text, windowBefore, windowAfter, lineClipped, err := h.pulseTerminalWindow(args.TerminalID, args.Head)
	if err != nil {
		return "", err
	}
	lines, pageBefore, pageAfter := pulseLinePage(pulseDisplayLines(text), n, args.Offset, args.Head)
	moreBefore := windowBefore || pageBefore
	moreAfter := windowAfter || pageAfter
	// Under the byte cap the far end is dropped: the OLDEST lines of a tail
	// page, the NEWEST lines of a head page. Either way the caller is told.
	return pulseCapped(len(lines), func(k int, truncated bool) any {
		kept := lines[len(lines)-k:]
		before, after := moreBefore, moreAfter
		if args.Head {
			kept = lines[:k]
			after = after || truncated
		} else {
			before = before || truncated
		}
		return struct {
			TerminalID    string   `json:"terminal_id"`
			Project       string   `json:"project"`
			Head          bool     `json:"head,omitempty"`
			Offset        int      `json:"offset,omitempty"`
			Lines         []string `json:"lines"`
			HasMoreBefore bool     `json:"has_more_before,omitempty"`
			HasMoreAfter  bool     `json:"has_more_after,omitempty"`
			Truncated     bool     `json:"truncated,omitempty"`
			LineClipped   bool     `json:"line_clipped,omitempty"`
		}{args.TerminalID, project, args.Head, args.Offset, append([]string{}, kept...), before, after, truncated, lineClipped}
	})
}

// pulseLinePage picks one page of n lines from a window of display lines. A tail
// page (head false) counts offset back from the end; a head page counts it
// forward from the start. moreBefore and moreAfter say whether lines exist in
// the window before and after the page.
func pulseLinePage(lines []string, n, offset int, head bool) (page []string, moreBefore, moreAfter bool) {
	total := len(lines)
	if head {
		start := min(offset, total)
		end := min(start+n, total)
		return lines[start:end], start > 0, end < total
	}
	end := max(total-offset, 0)
	start := max(end-n, 0)
	return lines[start:end], start > 0, end < total
}

// pulseTailLines turns raw terminal output into at most n display lines.
// Escape sequences are stripped, a carriage return keeps only the text after
// the last one on that line (the visible result of a progress-bar redraw), and
// each line is rune-clipped.
func pulseTailLines(text string, n int) []string {
	all := pulseDisplayLines(text)
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all
}

// pulseDisplayLines turns raw terminal output into display lines, all of them.
// The normalisation is the one pulseTailLines always applied.
func pulseDisplayLines(text string) []string {
	text = stripANSIEscapes(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return []string{}
	}
	all := strings.Split(text, "\n")
	for i, l := range all {
		if j := strings.LastIndexByte(l, '\r'); j >= 0 {
			l = l[j+1:]
		}
		all[i] = truncateRunes(l, pulseTerminalLineRuneBudget)
	}
	return all
}

// --- agent_runs ---

type pulseRunView struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Status       string         `json:"status"`
	Result       string         `json:"result,omitempty"`
	Err          string         `json:"err,omitempty"`
	Model        string         `json:"model,omitempty"`
	StartedAt    time.Time      `json:"started_at"`
	EndedAt      *time.Time     `json:"ended_at,omitempty"`
	InputTokens  int64          `json:"input_tokens"`
	OutputTokens int64          `json:"output_tokens"`
	Children     []pulseRunView `json:"children"`
}

func pulseRunViewOf(d agentRunDTO) pulseRunView {
	v := pulseRunView{
		ID: d.ID, Name: d.Name, Status: d.Status,
		Result: truncateRunes(d.Result, pulseRunResultRuneBudget), Err: d.Err, Model: d.Model,
		StartedAt: d.StartedAt, EndedAt: d.EndedAt,
		InputTokens: d.InputTokens, OutputTokens: d.OutputTokens,
		Children: make([]pulseRunView, 0, len(d.Children)),
	}
	for _, c := range d.Children {
		v.Children = append(v.Children, pulseRunViewOf(c))
	}
	return v
}

type pulseSessionRuns struct {
	SessionID string         `json:"session_id"`
	Runs      []pulseRunView `json:"runs"`
}

func (h *Handler) pulseAgentRunsTool(raw json.RawMessage) (string, error) {
	var args struct {
		SessionID string `json:"session_id"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	var ids []string
	if args.SessionID != "" {
		if isPulseSession(args.SessionID) {
			return "", fmt.Errorf("refusing to read the assistant's own session %q", args.SessionID)
		}
		if h.lookupAgentSession(args.SessionID) == nil {
			return "", fmt.Errorf("session %q has no live agent, so it has no runs to show", args.SessionID)
		}
		ids = []string{args.SessionID}
	} else {
		h.mu.Lock()
		for id := range h.agents {
			if !isPulseSession(id) {
				ids = append(ids, id)
			}
		}
		h.mu.Unlock()
		sort.Strings(ids)
	}
	groups := make([]pulseSessionRuns, 0, len(ids))
	for _, id := range ids {
		dtos := h.runsSnapshot(id)
		if len(dtos) == 0 {
			continue
		}
		g := pulseSessionRuns{SessionID: id, Runs: make([]pulseRunView, 0, len(dtos))}
		for _, d := range dtos {
			g.Runs = append(g.Runs, pulseRunViewOf(d))
		}
		groups = append(groups, g)
	}
	return pulseCapped(len(groups), func(n int, truncated bool) any {
		return struct {
			Sessions  []pulseSessionRuns `json:"sessions"`
			Truncated bool               `json:"truncated,omitempty"`
		}{append([]pulseSessionRuns{}, groups[:n]...), truncated}
	})
}
