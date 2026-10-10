package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/secretfile"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// The Pulse assistant (docs/concepts/pulse-assistant.md) is ONE global chat
// session that lives on the Pulse dashboard and can read every local session
// and terminal. It is an ordinary server session, so persistence, SSE
// streaming, the message endpoint and the per-session model override all work
// unchanged; what differs is its identity, its project root, its tool set and
// its prompt, all selected in buildAgentSession by isPulseSession.

// pulseSessionPrefix marks the assistant's session id. Nothing else mints it.
const pulseSessionPrefix = "pulse_"

// pulseAssistantTitle is the transcript title of the assistant session.
const pulseAssistantTitle = "Pulse assistant"

// pulseBoardMaxRows caps the board snapshot injected every turn.
const pulseBoardMaxRows = 60

// pulseBoardTitleRunes / pulseBoardTaskRunes clip the free-text columns of a
// board line so one verbose session cannot dominate the injected block.
const (
	pulseBoardTitleRunes = 80
	pulseBoardTaskRunes  = 120
)

// isPulseSession reports whether id is the Pulse assistant's session id.
func isPulseSession(id string) bool {
	return strings.HasPrefix(id, pulseSessionPrefix)
}

// pulseChatIDPattern is the charset a client-supplied assistant chat id must
// match. The id becomes a path segment under the pulse root, so separators and
// dots are refused. It is looser than the minted format on purpose, so chats
// minted by an older build stay selectable.
var pulseChatIDPattern = regexp.MustCompile(`^pulse_[A-Za-z0-9_-]+$`)

// isValidPulseChatID reports whether id is a pulse chat id that is safe to join
// into a path. It is the request-boundary check; isPulseSession stays a prefix
// test, because the board filter relies on it to skip every pulse_ id.
func isValidPulseChatID(id string) bool {
	return pulseChatIDPattern.MatchString(id)
}

// pulseRootPath is the assistant's dedicated project root, WITHOUT creating it.
// It is not a registered project: it exists only so the assistant's transcript
// has a storage home that no real project's session list ever shows.
func pulseRootPath() (string, error) {
	base, err := paths.GlobalDataDir()
	if err != nil {
		return "", fmt.Errorf("resolve global data dir: %w", err)
	}
	return filepath.Join(base, "pulse"), nil
}

// pulseAssistantRoot returns the assistant's root, creating it on demand.
func pulseAssistantRoot() (string, error) {
	root, err := pulseRootPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create pulse assistant root %s: %w", root, err)
	}
	return root, nil
}

// sessionSearchRoots is the SessionManager's search space for sessions it has
// never seen: the allowed project roots plus the Pulse root. The Pulse root is
// deliberately NOT part of allowedProjectRoots, which is a security boundary
// (terminal history, file endpoints) the assistant must not widen. Without it
// a message sent to the assistant after a restart, before the dashboard
// refetched /api/pulse/assistant, could not find its own transcript.
func (h *Handler) sessionSearchRoots() []string {
	roots := h.allowedProjectRoots()
	root, err := pulseRootPath()
	if err != nil {
		log.Printf("serve: pulse root unavailable for session search: %v", err)
		return roots
	}
	return append(roots, root)
}

type pulseAssistantState struct {
	SessionID string `json:"session_id"`
}

// errPulseChatBusy marks a chat switch refused because the current chat is
// mid-turn or paused on an ask. The drawer would stop showing that chat, so the
// switch waits for it to settle.
var errPulseChatBusy = errors.New("pulse assistant chat is busy")

// readPulseState returns the persisted current chat id, or "" when no chat has
// been minted yet.
func readPulseState(root string) (string, error) {
	statePath := filepath.Join(root, "state.json")
	data, err := os.ReadFile(statePath)
	switch {
	case err == nil:
		var st pulseAssistantState
		if err := json.Unmarshal(data, &st); err != nil {
			return "", fmt.Errorf("parse %s: %w", statePath, err)
		}
		// Checked before any caller joins the id into a path (ExistsForDir), so a
		// malformed id persisted by an older build is rejected rather than probed.
		if !isValidPulseChatID(st.SessionID) {
			return "", fmt.Errorf("%s holds session_id %q, which is not a valid %q chat id", statePath, st.SessionID, pulseSessionPrefix)
		}
		return st.SessionID, nil
	case errors.Is(err, os.ErrNotExist):
		return "", nil
	default:
		return "", fmt.Errorf("read %s: %w", statePath, err)
	}
}

// writePulseState makes id the current chat.
func writePulseState(root, id string) error {
	out, err := json.Marshal(pulseAssistantState{SessionID: id})
	if err != nil {
		return fmt.Errorf("encode pulse assistant state: %w", err)
	}
	statePath := filepath.Join(root, "state.json")
	if err := secretfile.WriteFileAtomic(statePath, out, 0o600); err != nil {
		return fmt.Errorf("persist %s: %w", statePath, err)
	}
	return nil
}

// mintPulseChat creates a new chat id and persists its empty transcript. The
// transcript goes to disk BEFORE the id is published or the live agent
// registered: GET /api/sessions/{id} and the advisor pin both read from disk.
func mintPulseChat(root string) (string, error) {
	id := pulseSessionPrefix + strings.TrimPrefix(session.NewSessionID(), "ses_")
	if err := session.SaveForDir(root, id, pulseAssistantTitle, nil, nil); err != nil {
		return "", fmt.Errorf("persist empty pulse assistant transcript %s: %w", id, err)
	}
	return id, nil
}

// pulseAssistantID returns the current chat id, minting and persisting one on
// first use. Serialised by pulseMu so two concurrent first calls cannot mint two
// assistants.
func (h *Handler) pulseAssistantID(root string) (string, error) {
	h.pulseMu.Lock()
	defer h.pulseMu.Unlock()

	cur, err := readPulseState(root)
	if err != nil {
		return "", err
	}
	if cur != "" {
		// A state file naming an id whose transcript is gone would hand the
		// dashboard a dead id (GET /api/sessions/{id} 404s), so mint a new one.
		exists, lerr := session.ExistsForDir(root, cur)
		switch {
		case lerr != nil:
			return "", fmt.Errorf("check pulse assistant transcript %s: %w", cur, lerr)
		case exists:
			return cur, nil
		default:
			log.Printf("serve: pulse assistant transcript for %s is missing in %s; minting a new session", cur, root)
		}
	}

	id, err := mintPulseChat(root)
	if err != nil {
		return "", err
	}
	if err := writePulseState(root, id); err != nil {
		return "", err
	}
	return id, nil
}

// writePulseAssistantInfo writes the {session_id, model} body shared by every
// endpoint that makes a chat current.
func (h *Handler) writePulseAssistantInfo(w http.ResponseWriter, id string) {
	model := h.effectiveSessionModel(id)
	if model == "" {
		writeError(w, http.StatusBadRequest, "no model configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"session_id": id, "model": model})
}

// HandlePulseAssistant serves GET /api/pulse/assistant:
// {"session_id": "pulse_...", "model": "<effective model id>"}. It mints the
// id on first call and persists an empty transcript; the agent is built lazily
// by the first message.
func (h *Handler) HandlePulseAssistant(w http.ResponseWriter, r *http.Request) {
	root, err := pulseAssistantRoot()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, err := h.pulseAssistantID(root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Register the registry entry only. The live agent is NOT pre-built here:
	// a build under this request's profile resolution can differ from the one
	// the first message resolves, forcing a rebuild and an illegal bootstrap
	// transition. The first message builds it through the normal path (which
	// has the isPulseSession branch), from the persisted transcript.
	h.sessions.Register(id, root)
	h.writePulseAssistantInfo(w, id)
}

// pulseChatBusyOrInternal maps a chat-switch failure to its status: a busy
// current chat is a conflict, anything else an internal error.
func pulseChatBusyOrInternal(w http.ResponseWriter, err error) {
	if errors.Is(err, errPulseChatBusy) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// refuseIfPulseChatBusy returns errPulseChatBusy when the current chat cannot be
// switched away from right now. An empty cur (no chat yet) is never busy.
func (h *Handler) refuseIfPulseChatBusy(cur string) error {
	if cur == "" {
		return nil
	}
	if err := h.pulseSessionBusy(cur); err != nil {
		return fmt.Errorf("%w: %v", errPulseChatBusy, err)
	}
	return nil
}

// HandleNewPulseChat serves POST /api/pulse/assistant/new: starts an empty chat
// and makes it current. The previous transcript stays on disk and is listed by
// HandleListPulseChats. Refused (409) while the current chat is mid-turn or
// paused on an ask.
func (h *Handler) HandleNewPulseChat(w http.ResponseWriter, r *http.Request) {
	root, err := pulseAssistantRoot()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.pulseMu.Lock()
	cur, err := readPulseState(root)
	if err == nil {
		err = h.refuseIfPulseChatBusy(cur)
	}
	var id string
	if err == nil {
		id, err = mintPulseChat(root)
	}
	if err == nil {
		err = writePulseState(root, id)
	}
	h.pulseMu.Unlock()
	if err != nil {
		pulseChatBusyOrInternal(w, err)
		return
	}
	h.sessions.Register(id, root)
	h.writePulseAssistantInfo(w, id)
}

// HandleSelectPulseChat serves PUT /api/pulse/assistant with
// {"session_id": "pulse_..."}: makes an earlier chat current. Refused (409)
// while the current chat is mid-turn or paused on an ask; 404 when the target
// transcript is gone.
func (h *Handler) HandleSelectPulseChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID *string `json:"session_id"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.SessionID == nil || !isValidPulseChatID(*req.SessionID) {
		writeError(w, http.StatusBadRequest, `invalid body (want {"session_id": "pulse_..."})`)
		return
	}
	target := *req.SessionID
	root, err := pulseAssistantRoot()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.pulseMu.Lock()
	cur, err := readPulseState(root)
	if err == nil && cur != target {
		err = h.refuseIfPulseChatBusy(cur)
	}
	var exists bool
	if err == nil {
		exists, err = session.ExistsForDir(root, target)
	}
	if err == nil && exists {
		err = writePulseState(root, target)
	}
	h.pulseMu.Unlock()
	if err != nil {
		pulseChatBusyOrInternal(w, err)
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "pulse assistant chat not found")
		return
	}
	h.sessions.Register(target, root)
	h.writePulseAssistantInfo(w, target)
}

// pulseChatSummary is one row of GET /api/pulse/assistant/chats.
type pulseChatSummary struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// pulsePageParams reads ?limit (default def, capped at maxLimit) and ?offset
// from a listing request. A malformed or non-positive limit, or a negative
// offset, is an error the caller answers with 400.
func pulsePageParams(r *http.Request, def, maxLimit int) (limit, offset int, err error) {
	limit = def
	if v := r.URL.Query().Get("limit"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 1 {
			return 0, 0, fmt.Errorf("limit must be a positive integer")
		}
		limit = min(n, maxLimit)
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 0 {
			return 0, 0, fmt.Errorf("offset must be a non-negative integer")
		}
		offset = n
	}
	return limit, offset, nil
}

// pulsePage cuts the [offset, offset+limit) window out of all and reports
// len(all). The page is a fresh slice, so it stays valid after all is dropped.
// Callers sort before paging so that pages do not overlap or skip rows.
func pulsePage[T any](all []T, limit, offset int) ([]T, int) {
	start := min(offset, len(all))
	end := min(start+limit, len(all))
	return append([]T{}, all[start:end]...), len(all)
}

// HandleListPulseChats serves GET /api/pulse/assistant/chats?limit=&offset=:
// the assistant's chats, most recently updated first. limit defaults to 20 and
// is capped at 100.
func (h *Handler) HandleListPulseChats(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := pulsePageParams(r, 20, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	root, err := pulseAssistantRoot()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	refs, err := session.ListRefsForDir(root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list pulse assistant chats: %v", err))
		return
	}
	chats := make([]session.Ref, 0, len(refs))
	for _, ref := range refs {
		if isPulseSession(ref.ID) {
			chats = append(chats, ref)
		}
	}
	sort.SliceStable(chats, func(i, j int) bool {
		if !chats[i].UpdatedAt.Equal(chats[j].UpdatedAt) {
			return chats[i].UpdatedAt.After(chats[j].UpdatedAt)
		}
		return chats[i].ID > chats[j].ID
	})
	refsPage, total := pulsePage(chats, limit, offset)
	page := make([]pulseChatSummary, 0, len(refsPage))
	for _, ref := range refsPage {
		page = append(page, pulseChatSummary{
			SessionID: ref.ID,
			Title:     ref.Title,
			CreatedAt: ref.CreatedAt.Format(time.RFC3339),
			UpdatedAt: ref.UpdatedAt.Format(time.RFC3339),
		})
	}
	current, err := readPulseState(root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"chats":   page,
		"total":   total,
		"offset":  offset,
		"limit":   limit,
		"current": current,
	})
}

// HandleGetPulseModel serves GET /api/config/pulse-model: {"model": "..."},
// empty when the slot is unset (the assistant then follows the chat model).
func (h *Handler) HandleGetPulseModel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"model": h.pulseModelSlot()})
}

// HandleSetPulseModel serves PUT /api/config/pulse-model with {"model": "..."};
// the empty string clears the slot. The change reaches the live assistant on
// its next turn (reconcileProfileAgent compares effectiveSessionModel).
func (h *Handler) HandleSetPulseModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model *string `json:"model"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Model == nil {
		writeError(w, http.StatusBadRequest, `invalid body (want {"model": "<id>"}; "" clears)`)
		return
	}
	model := strings.TrimSpace(*req.Model)
	if err := config.SavePulseModel(model); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("persist pulse model: %v", err))
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.PulseModel = model
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"model": model})
}

func (h *Handler) pulseModelSlot() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return ""
	}
	return h.cfg.Ocode.PulseModel
}

// defaultPulseSystemPrompt is the built-in assistant prompt. {{TOOLS}} is
// replaced with the registered tool names and descriptions, so the prompt can
// never list a tool that is not installed (or miss one that is).
const defaultPulseSystemPrompt = `You are the Pulse assistant, the operator's overview chat on the ocode Pulse dashboard.

Pulse shows every coding session running on this machine, across all projects. You can read all of them, plus the open terminals, to answer questions such as what is going on, which sessions need attention, or what a given session did.

Each turn begins with a [ocode:pulse] block: a snapshot of the live sessions as "status | project | session_id | title | current task or pending ask", plus an [ocode:pulse-memory] section with your saved notes. Treat the snapshot as a point-in-time summary and call the tools below for detail or fresher data.

Tools:
{{TOOLS}}

Rules:
- Write tools (the ones that message a session, run a command on it, or answer its permission or question asks) act on the operator's other work. Use them ONLY when the operator's CURRENT message explicitly asks for that action on that session. Otherwise describe what is pending and ask what they want. Each call needs the operator's approval; if it is denied or errors, say so and do not retry around it.
- You cannot edit files or run shell commands. Never claim to have changed, sent, approved or started anything unless the write tool result confirmed it.
- Your memory files (memory_read, memory_write) are yours alone. Keep them minimal: save only important, durable notes, such as standing preferences, ongoing goals and decisions the operator asks you to keep. Never save transient status, copies of the board, or task progress. Each file is capped at about 30k tokens, so condense on every write and drop stale entries instead of appending. Read before you write, because a write replaces the whole file.
- You only see sessions of this ocode server process. Sessions in other ocode processes are invisible to you.
- Cite session ids when you refer to a session. Say when a tool result was truncated.
- Be concise and factual. If you do not know, say so and name the tool call that would find out.`

// pulseSystemPrompt returns the configured prompt verbatim when set, else the
// built-in one with its tool list filled from the registered tools.
func pulseSystemPrompt(custom string, tools []*pulseTool) string {
	if strings.TrimSpace(custom) != "" {
		return custom
	}
	names := make([]string, 0, len(tools))
	byName := make(map[string]string, len(tools))
	for _, t := range tools {
		names = append(names, t.Name())
		byName[t.Name()] = t.Description()
	}
	sort.Strings(names)
	var b strings.Builder
	for i, n := range names {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "- %s: %s", n, byName[n])
	}
	return strings.Replace(defaultPulseSystemPrompt, "{{TOOLS}}", b.String(), 1)
}

// configurePulseAgent turns a freshly built agent into the Pulse assistant:
// only the pulse tools, the pulse prompt, and the board as a per-turn
// user-role tail. effCfg supplies the optional prompt override.
func (h *Handler) configurePulseAgent(ag *agent.Agent, effCfg *config.Config) {
	tools := h.pulseTools()
	asTools := make([]tool.Tool, len(tools))
	for i, t := range tools {
		asTools[i] = t
	}
	ag.RestrictToTools(asTools)
	custom := ""
	if effCfg != nil {
		custom = effCfg.Ocode.PulseSystemPrompt
	}
	ag.SetSystemPromptOverride(pulseSystemPrompt(custom, tools))
	ag.SetPulseSnapshot(h.pulseBoardSnapshot)
	// Read tools take no paths and change nothing, and memory_write only touches
	// the assistant's own notes, so a default "ask" would only make the assistant
	// unusable. The write tools (t.ask) deliberately get NO rule: each call
	// raises a normal permission ask in the drawer.
	if pm := ag.Permissions(); pm != nil {
		for _, t := range tools {
			if !t.ask {
				pm.SetRule(t.Name(), agent.PermissionAllow)
			}
		}
	}
}

// pulseBoardSnapshot renders the live board for the per-turn tail block. The
// assistant is already excluded by buildPulseRows.
func (h *Handler) pulseBoardSnapshot() string {
	now := time.Now()
	rows := buildPulseRows(h.gatherPulseInputs(pulseScopeLive, now), pulseScopeLive, now)
	board := renderPulseBoard(rows, now)
	if h.terminalAccessAllowed() {
		board += "\n" + renderPulseTerminals(h.pulseTerminalRows())
	}
	mem, err := h.pulseMemorySection(rows)
	if err != nil {
		log.Printf("serve: pulse memory for the board: %v", err)
		mem = pulseMemoryOpen + "\n(memory unavailable: " + err.Error() + ")\n" + pulseMemoryClose
	}
	if mem == "" {
		return board
	}
	return board + "\n" + mem
}

// renderPulseBoard is the pure half of pulseBoardSnapshot. Rows keep the order
// the board sorts them in; only the first pulseBoardMaxRows are listed.
func renderPulseBoard(rows []PulseRow, now time.Time) string {
	if len(rows) == 0 {
		return "No live sessions."
	}
	shown := rows
	if len(shown) > pulseBoardMaxRows {
		shown = shown[:pulseBoardMaxRows]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Live sessions as of %s (%d of %d shown): status | project | session_id | title | task or ask", now.UTC().Format(time.RFC3339), len(shown), len(rows))
	for _, r := range shown {
		b.WriteByte('\n')
		fmt.Fprintf(&b, "%s | %s | %s | %s | %s",
			r.Status,
			filepath.Base(r.ProjectPath),
			r.SessionID,
			pulseOneLine(r.Title, pulseBoardTitleRunes),
			pulseOneLine(pulseRowDetail(r), pulseBoardTaskRunes))
	}
	return b.String()
}

// pulseRowDetail is the last board column: the blocking ask when there is one,
// else the current task.
func pulseRowDetail(r PulseRow) string {
	if r.PendingAsk != nil {
		return "ask(" + r.PendingAsk.Kind + "): " + r.PendingAsk.Summary
	}
	if r.CurrentTask != nil {
		return r.CurrentTask.Text
	}
	return ""
}

// pulseOneLine collapses whitespace runs (newlines included) to single spaces
// so a field cannot break the one-row-per-line format, then clips by runes.
func pulseOneLine(s string, budget int) string {
	return truncateRunes(strings.Join(strings.Fields(s), " "), budget)
}

// HandleGetPulseSystemPrompt serves GET /api/config/pulse-system-prompt:
// {"prompt": "<configured or empty>", "default": "<built-in prompt with its tool
// list rendered>"}.
func (h *Handler) HandleGetPulseSystemPrompt(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"prompt":  h.pulseSystemPromptSlot(),
		"default": pulseSystemPrompt("", h.pulseTools()),
	})
}

// HandleSetPulseSystemPrompt serves PUT /api/config/pulse-system-prompt with
// {"prompt": "..."}; the empty string clears the override. The new prompt is
// re-applied to the live assistant, so it takes effect on its next model call.
func (h *Handler) HandleSetPulseSystemPrompt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt *string `json:"prompt"`
	}
	if err := readBodyJSON(r, &req); err != nil || req.Prompt == nil {
		writeError(w, http.StatusBadRequest, `invalid body (want {"prompt": "<text>"}; "" clears)`)
		return
	}
	prompt := *req.Prompt
	if err := config.SavePulseSystemPrompt(prompt); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("persist pulse system prompt: %v", err))
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.PulseSystemPrompt = prompt
	}
	var live []*agentSession
	for id, as := range h.agents {
		if isPulseSession(id) && as != nil && as.agent != nil {
			live = append(live, as)
		}
	}
	h.mu.Unlock()
	effective := pulseSystemPrompt(prompt, h.pulseTools())
	for _, as := range live {
		as.agent.SetSystemPromptOverride(effective)
	}
	writeJSON(w, http.StatusOK, map[string]string{"prompt": prompt})
}

func (h *Handler) pulseSystemPromptSlot() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return ""
	}
	return h.cfg.Ocode.PulseSystemPrompt
}
