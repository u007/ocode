package server

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// GET /api/pulse is the cross-project live-sessions dashboard: one row per
// top-level session across every local project, answering "what is running, and
// what is waiting on me?". All derivation rules (status precedence, sort,
// inclusion windows, cursor paging) live in pulse_rows.go; this file only
// gathers inputs and moves them across the wire.
//
// Query:
//
//	scope   "live" (default) | "all" — how far back idle sessions reach
//	cursor  opaque, from a previous response's next_cursor
//	limit   1..100, default 50
//
// Response: {"items": [PulseRow], "next_cursor": string | null}

// pulseScopeLive / pulseScopeAll are the only accepted scope values.
const (
	pulseScopeLive = "live"
	pulseScopeAll  = "all"
)

const (
	pulseDefaultLimit = 50
	pulseMaxLimit     = 100
)

// PulsePageResponse is the /api/pulse body. NextCursor is a pointer so the final
// page serializes an explicit JSON null rather than omitting the key — the
// client distinguishes "no more pages" from "field missing" by presence.
type PulsePageResponse struct {
	Items      []PulseRow `json:"items"`
	NextCursor *string    `json:"next_cursor"`
}

func (h *Handler) HandlePulse(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	scope := q.Get("scope")
	if scope == "" {
		// An OMITTED scope is the documented default. A PRESENT-BUT-EMPTY one
		// is not: it means the client computed a scope that came out blank
		// (an uninitialized variable, a dropped query param). Silently reading
		// it as "live" would hide that bug behind plausible-looking data, so
		// it is rejected like any other malformed value.
		if q.Has("scope") {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("scope is present but empty (want %q or %q)", pulseScopeLive, pulseScopeAll))
			return
		}
		scope = pulseScopeLive
	}
	if scope != pulseScopeLive && scope != pulseScopeAll {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("invalid scope %q (want %q or %q)", scope, pulseScopeLive, pulseScopeAll))
		return
	}

	limit := pulseDefaultLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > pulseMaxLimit {
			// A malformed limit is a client bug, not a reason to silently
			// serve the default: a dashboard quietly rendering 50 rows when the
			// caller asked for 500 hides the missing pages.
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("invalid limit %q (want an integer 1-%d)", v, pulseMaxLimit))
			return
		}
		limit = n
	}

	now := time.Now()
	inputs := h.gatherPulseInputs(scope, now)
	rows := buildPulseRows(inputs, scope, now)

	page, next, err := pagePulseRows(rows, q.Get("cursor"), limit)
	if err != nil {
		// Only a malformed cursor reaches here; a valid one past the end is an
		// empty page, not an error.
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if page == nil {
		page = []PulseRow{}
	}
	resp := PulsePageResponse{Items: page}
	if next != "" {
		resp.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, resp)
}

// gatherPulseInputs builds one PulseInput per session for the requested scope.
// The "all" scope merges the disk listing UNDER the live registry, so a session
// that is both persisted and resident appears once with its fresher state.
func (h *Handler) gatherPulseInputs(scope string, now time.Time) []PulseInput {
	live := h.gatherLivePulseInputs()
	if scope != pulseScopeAll {
		return live
	}
	byID := make(map[string]PulseInput, len(live))
	for _, in := range live {
		byID[in.SessionID] = in
	}
	for _, in := range h.gatherDiskPulseInputs(now) {
		if _, exists := byID[in.SessionID]; exists {
			continue // live data wins
		}
		byID[in.SessionID] = in
	}
	out := make([]PulseInput, 0, len(byID))
	for _, in := range byID {
		out = append(out, in)
	}
	return out
}

// gatherLivePulseInputs reads every session in the in-process registry. These are
// the sessions with resident agents: running turns, parked asks, and turns that
// recently failed — the set the "live" scope is about.
func (h *Handler) gatherLivePulseInputs() []PulseInput {
	entries := h.sessions.Snapshot()

	// Resident agent sessions, snapshotted under h.mu once. Each one's messages
	// are read under its own as.mu below, because a running turn mutates them
	// concurrently (same hazard as PendingPermissionAsks).
	h.mu.Lock()
	agents := make(map[string]*agentSession, len(h.agents))
	for id, as := range h.agents {
		agents[id] = as
	}
	h.mu.Unlock()

	out := make([]PulseInput, 0, len(entries))
	for _, e := range entries {
		in := PulseInput{
			SessionID:     e.SessionID,
			ProjectPath:   e.ProjectRoot,
			Running:       e.turnActive,
			LastTurnErr:   e.lastTurnErr,
			TurnStartedAt: e.turnStartedAt,
			// turnEndedAt is the truthful "last did something" stamp once a
			// turn has finished; lastActivity moves on other registry activity
			// (eviction bookkeeping) that would misdate the row.
			UpdatedAt: e.turnEndedAt,
		}
		if in.UpdatedAt.IsZero() {
			in.UpdatedAt = e.lastActivity
		}

		if as, ok := agents[e.SessionID]; ok && as != nil {
			// as.agent is write-once at construction and never reassigned, and
			// Activity's tracker is internally synchronised, so this read needs
			// no lock — and it is the field most useful while a turn runs.
			if a := as.agent; a != nil && a.Activity() != nil {
				if tools := a.Activity().Snapshot().ActiveTools; len(tools) > 0 {
					in.ActiveTool = tools[0].Name
				}
			}
			// as.messages, by contrast, is mutated under as.mu by the running
			// turn. TryLock (not Lock): runTurn holds as.mu for the WHOLE turn,
			// so a blocking Lock here would stall the dashboard behind every
			// running turn — the same reason livePendingAsks and the
			// speech-summary handler read non-blockingly. While a turn holds the
			// lock the session is mid-turn, not parked on an ask, so reporting
			// no pending ask (and no last line) is correct.
			if as.mu.TryLock() {
				in.PendingAsk = pulseTailAsk(as.messages)
				in.LastAssistantLine = pulseLastAssistantLine(as.messages)
				as.mu.Unlock()
			}
		}

		if e.ProjectRoot != "" {
			if title, found, err := session.TitleForDir(e.ProjectRoot, e.SessionID); err != nil {
				// A title is decoration. Log it (a broken index would otherwise
				// make every row title-less with no trace) and render the row
				// without one rather than failing the whole dashboard.
				log.Printf("serve: pulse title for %s in %s: %v", e.SessionID, e.ProjectRoot, err)
			} else if found {
				in.Title = title
			}
			if sum, found, err := tool.ReadTodoSummary(e.ProjectRoot, e.SessionID); err != nil {
				// Same reasoning as the title: one corrupt plan must not take
				// down the dashboard. A missing todo bar is a cosmetic loss;
				// a silent one would be a hidden defect.
				log.Printf("serve: pulse todo for %s in %s: %v", e.SessionID, e.ProjectRoot, err)
			} else if found {
				in.Todo = pulseTodoFromSummary(sum)
			}
		}

		out = append(out, in)
	}
	return out
}

// gatherDiskPulseInputs lists recent sessions for every LOCAL project so the
// "all" scope can show a session that finished yesterday, which has no registry
// entry and therefore no live state at all.
//
// This reads metadata only — one directory scan plus one indexed query per
// project, no transcript bodies — so the cost is bounded by the number of
// projects, not by how much the user has chatted. Remote-host projects are
// skipped: v1 of the dashboard is local-only, and their transcripts live on
// another machine.
func (h *Handler) gatherDiskPulseInputs(now time.Time) []PulseInput {
	if h.projects == nil {
		return nil
	}
	cutoff := now.Add(-pulseAllWindow)
	var out []PulseInput
	for _, p := range h.projects.List() {
		if p.Host != "" {
			continue
		}
		refs, err := session.ListRefsForDir(p.Path)
		if err != nil {
			// One unreadable project (permissions, a removed directory) must
			// not empty the dashboard — report it and serve the rest.
			log.Printf("serve: pulse list sessions for %s: %v", p.Path, err)
			continue
		}
		for _, ref := range refs {
			if ref.UpdatedAt.Before(cutoff) {
				continue
			}
			out = append(out, PulseInput{
				SessionID:   ref.ID,
				ProjectPath: p.Path,
				Title:       ref.Title,
				UpdatedAt:   ref.UpdatedAt,
				// Deliberately no PendingAsk / Todo / task line here: a
				// disk-only session has no resident agent to ask, and reading a
				// todo file per historical session would make "all" scale with
				// history rather than with the page size. Cards for these rows
				// show project + title + age, which is what that list is for.
			})
		}
	}
	return out
}

func pulseTodoFromSummary(sum tool.TodoSummary) *PulseTodo {
	items := make([]PulseTodoItem, 0, len(sum.Items))
	for _, it := range sum.Items {
		items = append(items, PulseTodoItem{Text: it.Text, State: it.State})
	}
	return &PulseTodo{Done: sum.Done, Total: sum.Total, Current: sum.Current, Items: items}
}

// pulseTailAsk reports the interaction blocking a turn, or nil. It reads the
// trailing tool round only: an ask persisted earlier in the transcript was
// already answered, and reporting it would show a session as "needs you" that
// has moved on.
func pulseTailAsk(messages []agent.Message) *PulseAsk {
	for i := trailingToolRunStart(messages); i < len(messages); i++ {
		m := messages[i]
		if m.Role != "tool" {
			continue
		}
		if req, ok := parsePermissionAsk(m.Content); ok {
			return &PulseAsk{Kind: PulseAskKindPermission, Summary: pulsePermissionSummary(req)}
		}
		if prompts, ok := parseQuestionAsk(m.Content); ok && isQuestionAsk(m.Content) {
			summary := ""
			if len(prompts) > 0 {
				summary = prompts[0].Question
			}
			return &PulseAsk{Kind: PulseAskKindQuestion, Summary: summary}
		}
	}
	return nil
}

// pulsePermissionSummary reduces a permission request to the one line a card can
// show. Order follows what the user is actually deciding: the command they are
// being asked to run, then the model's explanation, then the tool name.
func pulsePermissionSummary(req agent.PermissionRequest) string {
	if req.Command != "" {
		return req.Command
	}
	if req.Summary != "" {
		return req.Summary
	}
	if req.ToolName != "" {
		return "allow " + req.ToolName + "?"
	}
	// An empty tool name is rejected by parsePermissionAsk, so this is
	// unreachable in practice; returning the reason beats returning "".
	return req.DenyReason
}

// pulseLastAssistantLine returns the text of the most recent assistant message
// that said something, used as the fallback task line. Ask sentinels and tool
// results are skipped, so the line is prose the model actually wrote.
func pulseLastAssistantLine(messages []agent.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role != "assistant" || m.Content == "" {
			continue
		}
		return truncateRunes(pulseFirstLine(m.Content), 200)
	}
	return ""
}

// pulseFirstLine clips to the first newline so a multi-paragraph reply cannot
// blow up the card.
func pulseFirstLine(s string) string {
	for i := range len(s) {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
