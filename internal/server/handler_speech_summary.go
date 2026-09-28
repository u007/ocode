package server

import (
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/config"
)

// speechSummaryConfigResponse is the GET/PUT body for the speech-summary
// settings. Only two fields, but they are the pair the web sidebar row, the
// Settings field and the speak toolbar checkbox all read and write.
type speechSummaryConfigResponse struct {
	Model   string `json:"model"`
	Enabled bool   `json:"enabled"`
}

// HandleGetSpeechSummaryConfig reports the speech-summary model and its gate.
// The gate always reports a concrete boolean (never omitted) so the UI can
// tell "off" from "unknown" — it defaults TRUE, unlike every other model gate.
func (h *Handler) HandleGetSpeechSummaryConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := config.OcodeConfig{}
	if h.cfg != nil {
		cfg = h.cfg.Ocode
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, speechSummaryConfigResponse{
		Model:   cfg.SpeechSummaryModel,
		Enabled: cfg.SpeechSummaryEnabled,
	})
}

// HandleSetSpeechSummaryConfig persists the speech-summary settings. Both fields
// are POINTERS so an absent key means "leave alone": the sidebar's on/off
// checkbox must not clear the model, and the model picker must not flip the
// gate. A body with neither key is rejected rather than accepted as a no-op
// write that would still rewrite the config file.
func (h *Handler) HandleSetSpeechSummaryConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model   *string `json:"model"`
		Enabled *bool   `json:"enabled"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Model == nil && req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "model or enabled is required")
		return
	}

	// Pass ONLY the keys the request carried. SaveOcodeSpeechSummary merges
	// them onto the config it re-reads from disk under the cross-process lock,
	// so an untouched field keeps whatever is actually persisted rather than
	// this handler's possibly-stale in-memory copy — and two quick PUTs (the
	// sidebar toggle plus a model pick) cannot undo each other.
	//
	// The disk write runs a cross-process read-modify-write that can wait up to
	// ~5s on a contended lock file, and h.mu is the short-lived map lock every
	// other session's send and the run-state polls take, so h.mu is not held
	// across it. The write is itself serialized and made atomic by the config
	// file lock.
	model, enabled, err := config.SaveOcodeSpeechSummary(req.Model, req.Enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.SpeechSummaryModel = model
		h.cfg.Ocode.SpeechSummaryEnabled = enabled
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, speechSummaryConfigResponse{Model: model, Enabled: enabled})
}

// HandleSessionSpeechSummary rewrites one message into spoken prose for the TTS
// pipeline. It is session-scoped so the summariser resolves the SAME agent (and
// therefore the same credentials, active profile and usage attribution) the
// session's turns use.
//
// It deliberately never fails the caller on a summariser problem: the web layer
// falls back to speaking the full text, so a provider outage degrades the
// feature instead of breaking speech. An empty summary in a 200 is that signal.
// Only "this session does not exist" is a real error.
func (h *Handler) HandleSessionSpeechSummary(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Text string `json:"text"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}

	as, err := h.getOrCreateAgentSession(id)
	if err != nil || as == nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	// A running turn holds as.mu for its ENTIRE duration (runTurn does
	// as.mu.Lock(); defer as.mu.Unlock()), so waiting on that lock below would
	// stall this request for the whole turn and only then start the summariser.
	// Summarising is a side task, so when the session is mid-turn degrade
	// immediately to "no summary" — the web layer then speaks the full text —
	// instead of queueing behind the turn. Checked on the registry, which never
	// blocks, so this guard itself cannot stall.
	if h.sessions != nil && h.sessions.IsTurnActive(id) {
		writeJSON(w, http.StatusOK, map[string]string{"summary": ""})
		return
	}
	as.mu.Lock()
	a := as.agent
	as.mu.Unlock()
	if a == nil {
		// Mirrors title generation: a session with no buildable agent cannot run
		// a side LLM task. The caller speaks the full text instead.
		writeJSON(w, http.StatusOK, map[string]string{"summary": ""})
		return
	}

	summary := a.SummarizeForSpeech(text)
	writeJSON(w, http.StatusOK, map[string]string{"summary": summary})
}
