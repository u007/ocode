package server

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Pulse is the cross-project live-sessions dashboard behind GET /api/pulse. This
// file holds ONLY pure, I/O-free derivation: the handler gathers inputs (live
// registry entries, run states, ask tails, todo files) and calls in. Keeping
// the status/sort/paging rules free of I/O is what makes them testable without a
// running server, and it is the only place those rules are defined — the web
// client re-implements the sort for its own event-driven updates, so a single
// documented ordering here is the contract both sides hold.

// PulseStatus is the coarse state a dashboard row renders. The order of the
// constants is not the sort order; see pulseStatusRank.
type PulseStatus string

const (
	PulseStatusNeedsPermission PulseStatus = "needs_permission"
	PulseStatusNeedsQuestion   PulseStatus = "needs_question"
	PulseStatusRunning         PulseStatus = "running"
	PulseStatusError           PulseStatus = "error"
	PulseStatusIdle            PulseStatus = "idle"
)

// Pulse task kinds (the what-am-I-doing line under a row's title).
const (
	PulseTaskKindTodo = "todo"
	PulseTaskKindTool = "tool"
	PulseTaskKindText = "text"
)

// Pulse ask kinds (the kind of interaction blocking a turn).
const (
	PulseAskKindPermission = "permission"
	PulseAskKindQuestion   = "question"
)

// Todo item states, normalized from the on-disk file's status markers.
const (
	PulseTodoStatePending    = "pending"
	PulseTodoStateInProgress = "in_progress"
	PulseTodoStateDone       = "done"
)

// childSessionInfix is how the agent mints a subagent's session id
// (`<parent>_child_<agentName>_<ts>`, internal/agent/child_session.go). A child
// is execution detail for a parent's turn, not a conversation, so it is counted
// on the parent instead of getting its own row.
const childSessionInfix = "_child_"

// Inclusion windows for the two scopes. "live" answers "what is happening
// right now"; "all" extends the idle tail so a session finished yesterday is
// still findable.
const (
	pulseLiveWindow = 24 * time.Hour
	pulseAllWindow  = 7 * 24 * time.Hour
)

// pulseToolArgsRuneBudget caps how much of a tool's raw arguments ride along on
// the row's task line. The card shows one line of context, not the payload.
const pulseToolArgsRuneBudget = 80

// PulseTask is the single line describing what a session is doing right now.
type PulseTask struct {
	Kind string `json:"kind"` // "todo" | "tool" | "text"
	Text string `json:"text"`
}

// PulseTodoItem is one plan line with its state normalized to a word.
type PulseTodoItem struct {
	Text  string `json:"text"`
	State string `json:"state"` // "pending" | "in_progress" | "done"
}

// PulseTodo is a session's current plan.
type PulseTodo struct {
	Done    int             `json:"done"`
	Total   int             `json:"total"`
	Current string          `json:"current"`
	Items   []PulseTodoItem `json:"items"`
}

// PulseAsk is the interaction blocking a turn.
type PulseAsk struct {
	Kind    string `json:"kind"`    // "permission" | "question"
	Summary string `json:"summary"` // human-readable ask text
}

// PulseRow is one dashboard card.
type PulseRow struct {
	SessionID   string      `json:"session_id"`
	ProjectPath string      `json:"project_path"`
	Title       string      `json:"title"`
	Status      PulseStatus `json:"status"`
	// CurrentTask is nil for needs-you rows: the pending ask already carries
	// the text, so a task line would duplicate it.
	CurrentTask *PulseTask `json:"current_task"`
	Todo        *PulseTodo `json:"todo"`
	PendingAsk  *PulseAsk  `json:"pending_ask"`
	// TurnStartedAt is zero for a session that is not running.
	TurnStartedAt time.Time `json:"turn_started_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	ChildCount    int       `json:"child_count"`
}

// PulseInput is everything the pure builders need about one session. The
// handler fills it; nothing here touches the filesystem or the registry.
type PulseInput struct {
	SessionID   string
	ParentID    string // "" for a top-level session
	ProjectPath string
	Title       string
	Running     bool
	LastTurnErr string
	PendingAsk  *PulseAsk
	// ActiveTool / ActiveToolArgs / LastAssistantLine feed derivePulseTask, in
	// that order, after an in-progress todo item.
	ActiveTool        string
	ActiveToolArgs    string
	LastAssistantLine string
	Todo              *PulseTodo
	TurnStartedAt     time.Time
	UpdatedAt         time.Time
}

// pulseStatusRank orders sections: anything needing the user first, then work
// in progress, then the tail. Rows of equal rank fall back to recency.
func pulseStatusRank(s PulseStatus) int {
	switch s {
	case PulseStatusNeedsPermission, PulseStatusNeedsQuestion:
		return 0
	case PulseStatusRunning:
		return 1
	default:
		return 2
	}
}

// derivePulseStatus picks a row's state. Order matters and is deliberate: a
// turn paused on an ask is not "running" even though turnActive may still be
// set, and a stale error must never mask a fresh ask.
func derivePulseStatus(in PulseInput) PulseStatus {
	if in.PendingAsk != nil {
		switch in.PendingAsk.Kind {
		case PulseAskKindPermission:
			return PulseStatusNeedsPermission
		case PulseAskKindQuestion:
			return PulseStatusNeedsQuestion
		}
	}
	if in.Running {
		return PulseStatusRunning
	}
	if in.LastTurnErr != "" {
		return PulseStatusError
	}
	return PulseStatusIdle
}

// derivePulseTask picks the one-line "what is it doing" descriptor, falling back
// todo item -> active tool -> last assistant line -> nothing.
func derivePulseTask(in PulseInput) *PulseTask {
	// The ask is the headline for a needs-you row; a second line repeating it
	// wastes the space the ask itself needs.
	if in.PendingAsk != nil {
		return nil
	}
	if in.Todo != nil && in.Todo.Current != "" {
		return &PulseTask{Kind: PulseTaskKindTodo, Text: in.Todo.Current}
	}
	if in.ActiveTool != "" {
		text := in.ActiveTool
		if in.ActiveToolArgs != "" {
			hint := truncateRunes(in.ActiveToolArgs, pulseToolArgsRuneBudget)
			text = in.ActiveTool + " " + hint
		}
		return &PulseTask{Kind: PulseTaskKindTool, Text: text}
	}
	if in.LastAssistantLine != "" {
		return &PulseTask{Kind: PulseTaskKindText, Text: in.LastAssistantLine}
	}
	return nil
}

// truncateRunes clips s to at most budget runes, appending an ellipsis when it
// had to cut. Rune-based, not byte-based: a byte slice would split a multi-byte
// character into mojibake at the boundary.
func truncateRunes(s string, budget int) string {
	if utf8.RuneCountInString(s) <= budget {
		return s
	}
	return string([]rune(s)[:budget]) + "…"
}

// buildPulseRows turns per-session inputs into the sorted, filtered row set a
// scope asks for. now is injected so the windows are testable.
func buildPulseRows(inputs []PulseInput, scope string, now time.Time) []PulseRow {
	rows := make([]PulseRow, 0, len(inputs))
	// Children fold into their parent. Two passes: count first, then emit, so a
	// child that appears before its parent in the slice still counts.
	childrenOf := map[string]int{}
	for _, in := range inputs {
		if parent := parentSessionID(in); parent != "" {
			childrenOf[parent]++
		}
	}

	for _, in := range inputs {
		if parentSessionID(in) != "" {
			continue
		}
		status := derivePulseStatus(in)
		if !pulseRowIncluded(status, in.UpdatedAt, scope, now) {
			continue
		}
		rows = append(rows, PulseRow{
			SessionID:     in.SessionID,
			ProjectPath:   in.ProjectPath,
			Title:         in.Title,
			Status:        status,
			CurrentTask:   derivePulseTask(in),
			Todo:          in.Todo,
			PendingAsk:    in.PendingAsk,
			TurnStartedAt: in.TurnStartedAt,
			UpdatedAt:     in.UpdatedAt,
			ChildCount:    childrenOf[in.SessionID],
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		ri, rj := pulseStatusRank(rows[i].Status), pulseStatusRank(rows[j].Status)
		if ri != rj {
			return ri < rj
		}
		if !rows[i].UpdatedAt.Equal(rows[j].UpdatedAt) {
			return rows[i].UpdatedAt.After(rows[j].UpdatedAt)
		}
		return rows[i].SessionID < rows[j].SessionID
	})
	return rows
}

// parentSessionID returns the parent id when in belongs to a child session,
// else "". ParentID is authoritative when the caller has it; the id infix is the
// fallback so a session read off disk (which carries no parent field) is still
// folded instead of getting its own card.
func parentSessionID(in PulseInput) string {
	if in.ParentID != "" {
		return in.ParentID
	}
	if i := strings.Index(in.SessionID, childSessionInfix); i > 0 {
		return in.SessionID[:i]
	}
	return ""
}

// pulseRowIncluded applies a scope's window. Work in progress is always in
// range no matter how old its last update is — a turn that has been running for
// three days is exactly the row a dashboard exists to surface.
func pulseRowIncluded(status PulseStatus, updatedAt time.Time, scope string, now time.Time) bool {
	active := status == PulseStatusRunning ||
		status == PulseStatusNeedsPermission ||
		status == PulseStatusNeedsQuestion
	if active {
		return true
	}
	if updatedAt.IsZero() {
		return false
	}
	age := now.Sub(updatedAt)
	if age < 0 {
		// A clock skew or a future-dated row is treated as "just now" rather
		// than dropped: the user would rather see a row than lose it.
		return true
	}
	if scope == "all" {
		return age <= pulseAllWindow
	}
	return age <= pulseLiveWindow
}

// encodePulseCursor packs a row's sort key so the next page can resume strictly
// after it. The fields are the full sort key, which is what makes paging
// consistent with ordering even if a row's status changes between requests.
func encodePulseCursor(r PulseRow) string {
	raw := fmt.Sprintf("%d|%s|%s", pulseStatusRank(r.Status), r.UpdatedAt.UTC().Format(time.RFC3339Nano), r.SessionID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodePulseCursor is the inverse. A malformed cursor is an error, never a
// silent restart at page one: the client paging from page 3 would silently see
// rows it already has and report them as new.
func decodePulseCursor(cursor string) (rank int, updatedAt time.Time, sessionID string, err error) {
	raw, derr := base64.RawURLEncoding.DecodeString(cursor)
	if derr != nil {
		return 0, time.Time{}, "", fmt.Errorf("invalid cursor: %w", derr)
	}
	parts := strings.SplitN(string(raw), "|", 3)
	if len(parts) != 3 {
		return 0, time.Time{}, "", fmt.Errorf("invalid cursor: expected rank|timestamp|session_id")
	}
	if _, serr := fmt.Sscanf(parts[0], "%d", &rank); serr != nil {
		return 0, time.Time{}, "", fmt.Errorf("invalid cursor rank %q: %w", parts[0], serr)
	}
	ts, terr := time.Parse(time.RFC3339Nano, parts[1])
	if terr != nil {
		return 0, time.Time{}, "", fmt.Errorf("invalid cursor timestamp %q: %w", parts[1], terr)
	}
	if parts[2] == "" {
		return 0, time.Time{}, "", fmt.Errorf("invalid cursor: empty session id")
	}
	return rank, ts, parts[2], nil
}

// pagePulseRows returns the page of rows after cursor, plus the cursor for the
// next page ("" when this was the last one).
func pagePulseRows(rows []PulseRow, cursor string, limit int) ([]PulseRow, string, error) {
	if limit <= 0 {
		return nil, "", fmt.Errorf("limit must be positive, got %d", limit)
	}
	start := 0
	if cursor != "" {
		rank, updatedAt, sessionID, err := decodePulseCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		// Resume strictly after the cursor's position in the sort order.
		start = sort.Search(len(rows), func(i int) bool {
			ri := pulseStatusRank(rows[i].Status)
			if ri != rank {
				return ri > rank
			}
			if !rows[i].UpdatedAt.Equal(updatedAt) {
				return rows[i].UpdatedAt.Before(updatedAt)
			}
			return rows[i].SessionID > sessionID
		})
	}
	if start >= len(rows) {
		return nil, "", nil
	}
	end := start + limit
	if end > len(rows) {
		end = len(rows)
	}
	page := rows[start:end]
	next := ""
	if end < len(rows) {
		next = encodePulseCursor(rows[end-1])
	}
	return page, next, nil
}
