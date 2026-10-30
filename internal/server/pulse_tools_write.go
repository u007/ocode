package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/session"
)

// Write tools of the Pulse assistant (docs/concepts/pulse-assistant.md). They
// act on OTHER sessions, so they are deliberately given NO allow rule in
// configurePulseAgent: every call surfaces as an ordinary permission ask in the
// drawer, and the operator may "always allow" from that dialog like any tool.
//
// Each tool reuses the existing HTTP handler's code path, in-process, through
// pulseCall. The handlers own the real rules (RC-bridge routing, profile
// reconcile, ask matching, harmful-operation guards) and several are hundreds
// of lines of response-writing code; calling them keeps one implementation
// instead of a second copy that could drift.

// pulseRecapTimeout bounds session_recap and the /recap command. A recap is an
// LLM call (33 s was observed on a slow model).
const pulseRecapTimeout = 90 * time.Second

// pulseCommands is the FIXED slash-command allowlist of session_command. There
// is no "send it as text" fallback for anything else.
var pulseCommands = []string{"/btw", "/cancel", "/compact", "/recap", "/title"}

// pulseRecorder is the minimal http.ResponseWriter pulseCall hands a handler.
type pulseRecorder struct {
	hdr  http.Header
	code int
	buf  bytes.Buffer
}

func (r *pulseRecorder) Header() http.Header { return r.hdr }
func (r *pulseRecorder) WriteHeader(c int) {
	if r.code == 0 {
		r.code = c
	}
}
func (r *pulseRecorder) Write(b []byte) (int, error) {
	r.WriteHeader(http.StatusOK)
	return r.buf.Write(b)
}

// pulseCall runs an existing handler in-process with a JSON body and returns
// its response body. A status of 400 or above becomes an error carrying the
// handler's own message, so a refusal is never mistaken for success.
func pulseCall(method, target string, body any, call func(w http.ResponseWriter, r *http.Request)) (json.RawMessage, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	ctx := context.WithValue(context.Background(), pulseOriginKey{}, true)
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := &pulseRecorder{hdr: http.Header{}}
	call(rec, req)
	out := bytes.TrimSpace(rec.buf.Bytes())
	if rec.code >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		msg := string(out)
		if json.Unmarshal(out, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		return nil, fmt.Errorf("%s (HTTP %d)", msg, rec.code)
	}
	if len(out) == 0 {
		return json.RawMessage("{}"), nil
	}
	if !json.Valid(out) {
		return nil, fmt.Errorf("handler returned a non-JSON body: %.200s", out)
	}
	return json.RawMessage(out), nil
}

// pulseWriteTarget validates a session id a write tool is about to act on:
// never the assistant itself, never a child session, and it must exist.
func (h *Handler) pulseWriteTarget(id string) error {
	if id == "" {
		return errors.New("session_id is required")
	}
	if isPulseSession(id) {
		return fmt.Errorf("refusing to act on the assistant's own session %q", id)
	}
	if strings.Contains(id, childSessionInfix) {
		return fmt.Errorf("refusing to act on child session %q; act on its parent", id)
	}
	if _, err := h.sessions.Resolve(id); err != nil {
		if host := h.pulseRemoteSessionHost(id); host != "" {
			return fmt.Errorf("session %q runs on remote host %s; acting on a remote session is not supported from the Pulse assistant yet", id, host)
		}
		return fmt.Errorf("session %q not found", id)
	}
	return nil
}

// pulseSessionBusy reports why a message cannot be sent to id right now, or
// nil. The message endpoint would queue a message into a running turn; the
// assistant must not, so busy is an error instead. This is the friendly
// pre-check; HandleSendMessage makes the same refusal atomic at dispatch.
func (h *Handler) pulseSessionBusy(id string) error {
	if as := h.lookupAgentSession(id); as != nil {
		if !as.mu.TryLock() {
			// The turn lock is held by a running turn or briefly by a reader.
			// The turn signals say which; the pending-ask state cannot be read
			// either way, so the send is refused in both cases.
			if err := h.pulseTurnSignalBusy(id); err != nil {
				return err
			}
			return fmt.Errorf("session %s state is briefly locked; try again shortly", id)
		}
		ask := pulseTailAsk(as.messages)
		as.mu.Unlock()
		if ask != nil {
			return fmt.Errorf("session %s is paused on a pending %s ask (%s); resolve it with permission_resolve or question_answer first", id, ask.Kind, ask.Summary)
		}
	}
	return h.pulseTurnSignalBusy(id)
}

// pulseTurnSignalBusy reports a running or compacting turn on id, or nil.
func (h *Handler) pulseTurnSignalBusy(id string) error {
	h.cancelMu.Lock()
	inFlight := h.turnInFlight[id] > 0
	h.cancelMu.Unlock()
	if inFlight || h.sessions.IsTurnActive(id) {
		return fmt.Errorf("session %s is mid-turn; try again when it finishes", id)
	}
	if h.sessions.IsCompacting(id) {
		return fmt.Errorf("session %s is compacting; try again when it finishes", id)
	}
	return nil
}

// pulseOriginKey marks a request made in-process by a pulse write tool. The
// send path refuses such a request when its session is busy instead of queueing
// it, because the assistant's writes never queue.
type pulseOriginKey struct{}

func isPulseOrigin(r *http.Request) bool {
	v, _ := r.Context().Value(pulseOriginKey{}).(bool)
	return v
}

func (h *Handler) pulseWriteTools() []*pulseTool {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	return []*pulseTool{
		{
			name: "session_send",
			desc: "Send a message to another session, starting a turn on it. Refused when that session is mid-turn or paused on an ask. Needs the operator's approval. Use only when the operator's current message asks you to message that session.",
			ask:  true,
			props: map[string]any{
				"session_id": str("Target session id"),
				"content":    str("The message to send, as the operator would type it"),
			},
			run: h.pulseSessionSendTool,
		},
		{
			name: "session_new",
			desc: "Start a new chat session in a project, optionally with an initial message, and return its session id. Local projects only for now (remote SSH/WSL hosts are refused here; use session_send on a session created on the host). Needs the operator's approval.",
			ask:  true,
			props: map[string]any{
				"project_path": str("Project root of the new session (local server project root)"),
				"host":         str("Remote host ([user@]host[:port] or wsl:<distro>); currently unsupported, refused when set"),
				"content":      str("Initial message to send in the new session (required)"),
			},
			run: h.pulseSessionNewTool,
		},
		{
			name: "terminal_new",
			desc: "Start a new terminal (pty shell) in a project and return its terminal id. For a project on a remote SSH/WSL host pass host and the project's path as project_list shows it; the host must be connected. Needs the operator's approval.",
			ask:  true,
			props: map[string]any{
				"project_path": str("Project root of the new terminal: a local project root, or for a remote host the saved path from project_list"),
				"host":         str("The project's host from project_list, for a remote (SSH/WSL) project; omit for a local one"),
				"title":        str("Optional tab title"),
			},
			run: h.pulseTerminalNewTool,
		},
		{
			name:  "session_close",
			desc:  "Close (tear down the resident agent for) an existing session. Safe on idle sessions. Needs the operator's approval.",
			ask:   true,
			props: map[string]any{"session_id": str("Session id to close")},
			run:   h.pulseSessionCloseTool,
		},
		{
			name: "terminal_kill",
			desc: "Kill a terminal's shell and remove its tab/history. For a remote project's terminal pass its host. Needs the operator's approval.",
			ask:  true,
			props: map[string]any{
				"terminal_id": str("Terminal id from terminal_tabs"),
				"host":        str("The project's host from terminal_tabs, for a remote (SSH/WSL) terminal; omit for a local one"),
			},
			run: h.pulseTerminalKillTool,
		},
		{
			name: "session_command",
			desc: "Run one slash command on a session. Supported: " + strings.Join(pulseCommands, ", ") + ". /btw needs a question in args, /title needs the new title, /compact may take a focus. Anything else is rejected. Needs the operator's approval.",
			ask:  true,
			props: map[string]any{
				"session_id": str("Target session id"),
				"command":    str("One of " + strings.Join(pulseCommands, ", ")),
				"args":       str("Command argument (question for /btw, title for /title, optional focus for /compact)"),
			},
			run: h.pulseSessionCommandTool,
		},
		{
			name: "permission_resolve",
			desc: "Allow or deny a permission ask that another session is waiting on. The request_id is on the board row's pending ask. Needs the operator's approval.",
			ask:  true,
			props: map[string]any{
				"session_id": str("Session that raised the ask"),
				"request_id": str("The ask's request id (tool call id)"),
				"decision":   map[string]any{"type": "string", "enum": []string{"allow", "deny"}, "description": "allow or deny"},
			},
			run: h.pulsePermissionResolveTool,
		},
		{
			name: "question_answer",
			desc: "Answer a question another session is waiting on. answers uses the same payload as POST /api/questions: a list of {header, question, answers: [{label, text, custom}]}. Needs the operator's approval.",
			ask:  true,
			props: map[string]any{
				"session_id": str("Session that asked the question"),
				"request_id": str("The question's request id (tool call id)"),
				"answers":    map[string]any{"type": "array", "description": "Answer sets, one per question", "items": map[string]any{"type": "object"}},
			},
			run: h.pulseQuestionAnswerTool,
		},
		{
			name: "question_dismiss",
			desc: "Dismiss (cancel) a pending question another session is waiting on, without answering it. The request_id is on the board row's pending ask (or session_ask). Needs the operator's approval.",
			ask:  true,
			props: map[string]any{
				"session_id": str("Session that asked the question"),
				"request_id": str("The question's request id (tool call id)"),
			},
			run: h.pulseQuestionDismissTool,
		},
	}
}

func (h *Handler) pulseSessionSendTool(raw json.RawMessage) (string, error) {
	var args struct {
		SessionID string `json:"session_id"`
		Content   string `json:"content"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Content) == "" {
		return "", errors.New("content is required")
	}
	if err := h.pulseWriteTarget(args.SessionID); err != nil {
		return "", err
	}
	if err := h.pulseSessionBusy(args.SessionID); err != nil {
		return "", err
	}
	if _, err := pulseCall(http.MethodPost, "/api/sessions/"+args.SessionID+"/message",
		map[string]any{"content": args.Content, "async": true},
		func(w http.ResponseWriter, r *http.Request) { h.HandleSendMessage(w, r, args.SessionID) }); err != nil {
		return "", fmt.Errorf("send to %s: %w", args.SessionID, err)
	}
	return marshalPulseString(map[string]any{"session_id": args.SessionID, "accepted": true})
}

func (h *Handler) pulseSessionCommandTool(raw json.RawMessage) (string, error) {
	var args struct {
		SessionID string `json:"session_id"`
		Command   string `json:"command"`
		Args      string `json:"args"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	cmd := strings.ToLower(strings.TrimSpace(args.Command))
	if !strings.HasPrefix(cmd, "/") {
		cmd = "/" + cmd
	}
	supported := false
	for _, c := range pulseCommands {
		if c == cmd {
			supported = true
		}
	}
	if !supported {
		return "", fmt.Errorf("unsupported command %q; supported commands: %s", args.Command, strings.Join(pulseCommands, ", "))
	}
	text := strings.TrimSpace(args.Args)
	if (cmd == "/btw" || cmd == "/title") && text == "" {
		return "", fmt.Errorf("%s needs args", cmd)
	}
	if err := h.pulseWriteTarget(args.SessionID); err != nil {
		return "", err
	}
	id := args.SessionID

	var (
		result json.RawMessage
		err    error
	)
	switch cmd {
	case "/compact":
		result, err = pulseCall(http.MethodPost, "/api/sessions/"+id+"/compact", map[string]any{"focus": text},
			func(w http.ResponseWriter, r *http.Request) { h.HandleCompactSession(w, r, id) })
	case "/btw":
		result, err = pulseCall(http.MethodPost, "/api/sessions/"+id+"/btw", map[string]any{"content": text},
			func(w http.ResponseWriter, r *http.Request) { h.HandleBtw(w, r, id) })
	case "/title":
		result, err = pulseCall(http.MethodPut, "/api/sessions/"+id+"/title", map[string]any{"title": text},
			func(w http.ResponseWriter, r *http.Request) { h.HandleSetSessionTitle(w, r, id) })
	case "/cancel":
		result, err = pulseCall(http.MethodPost, "/api/sessions/"+id+"/cancel", map[string]any{},
			func(w http.ResponseWriter, r *http.Request) { h.HandleCancelSession(w, r, id) })
	case "/recap":
		var recap string
		recap, err = pulseRecapWithTimeout(pulseRecapTimeout, func(ctx context.Context) (string, error) {
			return h.recapSessionCtx(ctx, id)
		})
		if err == nil {
			result, err = json.Marshal(map[string]string{"recap": recap})
		}
	}
	if err != nil {
		return "", fmt.Errorf("%s on %s: %w", cmd, id, err)
	}
	return marshalPulseString(map[string]any{"session_id": id, "command": cmd, "result": result})
}

func (h *Handler) pulsePermissionResolveTool(raw json.RawMessage) (string, error) {
	var args struct {
		SessionID string `json:"session_id"`
		RequestID string `json:"request_id"`
		Decision  string `json:"decision"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	if args.RequestID == "" {
		return "", errors.New("request_id is required")
	}
	if args.Decision != PermDecisionAllow && args.Decision != PermDecisionDeny {
		return "", fmt.Errorf("decision must be %q or %q, got %q", PermDecisionAllow, PermDecisionDeny, args.Decision)
	}
	if err := h.pulseWriteTarget(args.SessionID); err != nil {
		return "", err
	}
	if _, err := pulseCall(http.MethodPost, "/api/permissions/resolve",
		map[string]any{"request_id": args.RequestID, "session_id": args.SessionID, "decision": args.Decision},
		h.HandleResolvePermission); err != nil {
		return "", fmt.Errorf("resolve permission %s on %s: %w", args.RequestID, args.SessionID, err)
	}
	return marshalPulseString(map[string]any{"session_id": args.SessionID, "request_id": args.RequestID, "decision": args.Decision, "accepted": true})
}

func (h *Handler) pulseQuestionAnswerTool(raw json.RawMessage) (string, error) {
	var args struct {
		SessionID string          `json:"session_id"`
		RequestID string          `json:"request_id"`
		Answers   json.RawMessage `json:"answers"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	if args.RequestID == "" {
		return "", errors.New("request_id is required")
	}
	if t := bytes.TrimSpace(args.Answers); len(t) == 0 || t[0] != '[' {
		return "", errors.New("answers must be a JSON array")
	}
	if err := h.pulseWriteTarget(args.SessionID); err != nil {
		return "", err
	}
	if _, err := pulseCall(http.MethodPost, "/api/questions",
		map[string]any{"request_id": args.RequestID, "session_id": args.SessionID, "answers": args.Answers},
		h.HandleAnswerQuestion); err != nil {
		return "", fmt.Errorf("answer question %s on %s: %w", args.RequestID, args.SessionID, err)
	}
	return marshalPulseString(map[string]any{"session_id": args.SessionID, "request_id": args.RequestID, "accepted": true})
}

func marshalPulseString(v any) (string, error) {
	b, err := marshalPulse(v)
	if err != nil {
		return "", fmt.Errorf("encode result: %w", err)
	}
	return string(b), nil
}

// pulseRecapWithTimeout runs fn under a deadline and names the timeout in the
// error, so a slow model is never reported as an empty or partial recap.
func pulseRecapWithTimeout(timeout time.Duration, fn func(ctx context.Context) (string, error)) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	text, err := fn(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		return "", fmt.Errorf("recap timed out after %s", timeout)
	}
	return text, err
}

// pulseSessionNewTool starts a new local chat session (with an initial message)
// and returns its id. Remote/WSL hosts are refused here because creating the
// session must happen where the agent runs; ask the operator or use
// session_send on one that already exists.
func (h *Handler) pulseSessionNewTool(raw json.RawMessage) (string, error) {
	var args struct {
		ProjectPath string `json:"project_path"`
		Host        string `json:"host"`
		Content     string `json:"content"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Host) != "" {
		return "", fmt.Errorf("creating sessions on remote hosts is not supported from the Pulse assistant yet; create it on the host or use session_send on an existing session")
	}
	projectRoot := strings.TrimSpace(args.ProjectPath)
	if projectRoot == "" {
		return "", errors.New("project_path is required")
	}
	if args.Content == "" {
		return "", errors.New("content is required")
	}
	if h.projectHostFor(projectRoot) != "" {
		return "", fmt.Errorf("project_path %q looks like a remote path; pass host= for remotes (not yet supported)", projectRoot)
	}
	expanded, err := projects.ExpandHome(projectRoot)
	if err != nil {
		return "", fmt.Errorf("expand project path %q: %w", projectRoot, err)
	}
	projectRoot = expanded
	if projectRoot == "" {
		return "", errors.New("project_path is required")
	}
	allowed := false
	for _, root := range h.allowedProjectRoots() {
		if projectRoot == root {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("project %q is not a registered project root", projectRoot)
	}
	sid := session.NewSessionID()
	// async: true → HandleChat persists the first message and returns 202
	// without running the child's turn inline. Without it the sync path runs
	// the whole turn inside this tool call (and a mid-turn pause would be
	// reported as a success with no reply), blocking the Pulse assistant.
	out, err := pulseCall("POST", "/api/chat", map[string]any{
		"session_id":   sid,
		"project_path": projectRoot,
		"content":      args.Content,
		"async":        true,
	}, h.HandleChat)
	if err != nil {
		return "", fmt.Errorf("create session in %s: %w", projectRoot, err)
	}
	createdID, err := pulseCreatedSessionID(out)
	if err != nil {
		return "", fmt.Errorf("create session in %s: %w", projectRoot, err)
	}
	return marshalPulseString(map[string]any{"session_id": createdID, "project_path": projectRoot})
}

// pulseCreatedSessionID reads the session id from the async POST /api/chat
// reply. The server always names the session it bound; an empty id means it
// was not bound, so the tool must fail rather than report the id it generated.
func pulseCreatedSessionID(out json.RawMessage) (string, error) {
	var resp ChatResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", fmt.Errorf("decode create-session response: %w", err)
	}
	if resp.SessionID == "" {
		return "", errors.New("create-session response carried no session id")
	}
	return resp.SessionID, nil
}

// pulseSessionCloseTool closes a session's resident agent.
func (h *Handler) pulseSessionCloseTool(raw json.RawMessage) (string, error) {
	var args struct {
		SessionID string `json:"session_id"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	if err := h.pulseWriteTarget(args.SessionID); err != nil {
		return "", err
	}
	if _, err := pulseCall(http.MethodPost, "/api/sessions/"+args.SessionID+"/close", nil,
		func(w http.ResponseWriter, r *http.Request) { h.HandleCloseSession(w, r, args.SessionID) }); err != nil {
		return "", fmt.Errorf("close session %s: %w", args.SessionID, err)
	}
	return marshalPulseString(map[string]any{"session_id": args.SessionID, "closed": true})
}

// pulseTerminalKillTool kills a terminal shell, drops its history and removes
// its shared tab entry. It refuses a terminal whose project this server is not
// allowed to serve.
func (h *Handler) pulseTerminalKillTool(raw json.RawMessage) (string, error) {
	var args struct {
		TerminalID string `json:"terminal_id"`
		Host       string `json:"host"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	id := strings.TrimSpace(args.TerminalID)
	if id == "" {
		return "", errors.New("terminal_id is required")
	}
	if host := strings.TrimSpace(args.Host); host != "" {
		return h.pulseRemoteTerminalKill(host, id)
	}
	// Only a live terminal can be killed, and only when its project is one
	// this server may serve. This stops the assistant from tearing down a
	// shell in a project the operator never opened here.
	sess := h.terminalSessions.lookup(id)
	if sess == nil {
		return "", fmt.Errorf("terminal %s is not live on this server", id)
	}
	allowed := false
	for _, root := range h.allowedProjectRoots() {
		if sess.project == root {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("terminal %s belongs to project %q, which is not a registered project root", id, sess.project)
	}
	project := sess.project
	if _, err := pulseCall(http.MethodDelete, "/api/terminal/"+id, nil,
		func(w http.ResponseWriter, r *http.Request) {
			r.SetPathValue("id", id)
			h.HandleTerminalKill(w, r)
		}); err != nil {
		return "", fmt.Errorf("kill terminal %s: %w", id, err)
	}
	// HandleTerminalKill only tears down the shell and its disk history; the
	// shared tab list is owned by the client. Drop the entry here so every
	// connected client stops showing the dead terminal.
	if h.termTabsStore != nil {
		if err := h.termTabsStore.RemoveTerminal(project, id); err != nil {
			return "", fmt.Errorf("terminal %s killed but removing its tab entry failed: %w", id, err)
		}
		h.bus.Publish("terminal_tabs_changed", "", "", nil)
	}
	return marshalPulseString(map[string]any{"terminal_id": id, "killed": true})
}

func (h *Handler) pulseQuestionDismissTool(raw json.RawMessage) (string, error) {
	var args struct {
		SessionID string `json:"session_id"`
		RequestID string `json:"request_id"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	if args.RequestID == "" {
		return "", errors.New("request_id is required")
	}
	if err := h.pulseWriteTarget(args.SessionID); err != nil {
		return "", err
	}
	if _, err := pulseCall(http.MethodPost, "/api/questions/cancel",
		map[string]any{"request_id": args.RequestID, "session_id": args.SessionID},
		h.HandleDismissQuestion); err != nil {
		return "", fmt.Errorf("dismiss question %s on %s: %w", args.RequestID, args.SessionID, err)
	}
	return marshalPulseString(map[string]any{"session_id": args.SessionID, "request_id": args.RequestID, "dismissed": true})
}
