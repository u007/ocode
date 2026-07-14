package server

import "sync"

// TUIStatus is a consolidated snapshot of the TUI's live state, exposed to the
// web UI so the browser can mirror the TUI's status bar (model, advisor, IDE,
// session title, cwd, context usage, spending, modified files, LSP servers,
// extra allowed paths). It is the payload of:
//   - the "status" SSE event pushed from TUI -> web on every change, and
//   - GET /api/tui-status for initial page loads.
type TUIStatus struct {
	// Main chat model (provider/model).
	MainModel string `json:"main_model,omitempty"`
	// Small model name + runtime on/off (the web should mirror both).
	SmallModel   string `json:"small_model,omitempty"`
	SmallModelOn bool   `json:"small_model_enabled"`
	// Advisor model + runtime on/off.
	AdvisorModel   string `json:"advisor_model,omitempty"`
	AdvisorEnabled bool   `json:"advisor_enabled"`
	// Recap model name + runtime on/off.
	RecapModel   string `json:"recap_model,omitempty"`
	RecapModelOn bool   `json:"recap_model_enabled"`
	// OCR tool model + runtime on/off.
	OcrBackend string `json:"ocr_backend,omitempty"`
	OcrModel   string `json:"ocr_model,omitempty"`
	OcrEnabled bool   `json:"ocr_enabled"`
	// Image generation tool: enabled state + selected provider/model.
	ImageGenEnabled  bool   `json:"image_gen_enabled"`
	ImageGenProvider string `json:"image_gen_provider,omitempty"`
	ImageGenModel    string `json:"image_gen_model,omitempty"`
	// IDE integration: mode is "off" | "claude" | ...; status is a short
	// human-readable string for the status bar.
	IDEMode   string `json:"ide_mode,omitempty"`
	IDEStatus string `json:"ide_status,omitempty"`
	// Subagent model currently active in the latest turn (empty when none).
	SubagentModel string `json:"subagent_model,omitempty"`
	// Session identity / metadata.
	SessionID    string `json:"session_id,omitempty"`
	SessionTitle string `json:"session_title,omitempty"`
	CWD          string `json:"cwd,omitempty"`
	// Context window usage.
	ContextCurrentTokens int    `json:"context_current_tokens,omitempty"`
	ContextMaxTokens     int    `json:"context_max_tokens,omitempty"`
	ContextModel         string `json:"context_model,omitempty"`
	// Spending (USD) accumulated for the current session / day. Sourced from
	// the usage package; nil if no usage has been recorded yet.
	SpendingUSD float64 `json:"spending_usd,omitempty"`
	// Files modified in the session (path, status). Status is the single-char
	// git status code (M/A/D/??/U etc.) when available, otherwise "".
	ModifiedFiles []FileStatus `json:"modified_files,omitempty"`
	// LSP servers currently active.
	LSPServers []LSPStatus `json:"lsp_servers,omitempty"`
	// Extra paths pre-authorized by the user (so the model knows about them
	// during permission checks; mirrored to the web for visibility).
	ExtraAllowedPaths []string `json:"extra_allowed_paths,omitempty"`
	// Last update time (server clock, RFC3339Nano). Lets the web show staleness.
	UpdatedAt string `json:"updated_at,omitempty"`
}

// FileStatus is one entry in TUIStatus.ModifiedFiles.
type FileStatus struct {
	Path   string `json:"path"`
	Status string `json:"status,omitempty"`
}

// LSPStatus mirrors lsp.ServerStatus plus a coarse state string the web can
// render without knowing LSP internals.
type LSPStatus struct {
	Cmd    string `json:"cmd"`
	LangID string `json:"lang_id,omitempty"`
	Root   string `json:"root,omitempty"`
	State  string `json:"state"` // "running" | "starting" | "failed"
	Detail string `json:"detail,omitempty"`
	// Aggregated diagnostic counts across the project, attributed to the
	// owning server by its binary name.
	DiagnosticsErrors   int `json:"diagnostics_errors"`
	DiagnosticsWarnings int `json:"diagnostics_warnings"`
}

// tuiStatusStore is a thread-safe holder for the latest snapshot. It is set
// on the RCBridge so the SSE path and the REST handler see the same value.
type tuiStatusStore struct {
	mu   sync.RWMutex
	snap TUIStatus
}

// Snapshot returns the most recent TUI status. Safe for concurrent callers.
func (s *tuiStatusStore) Snapshot() TUIStatus {
	if s == nil {
		return TUIStatus{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap
}

// Set replaces the stored snapshot. The TUI calls this whenever a tracked
// field changes (debounced 250ms). It also fires the SSE `status` event so
// connected browsers refresh their status bar live.
func (s *tuiStatusStore) Set(snap TUIStatus, bridge *RCBridge) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()
	if bridge != nil {
		bridge.Broadcast(SSEEvent{Event: "status", Data: snap})
	}
}
