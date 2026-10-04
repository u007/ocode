package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/u007/ocode/internal/config"
)

// quickActionsMaxBodyBytes bounds the PUT body. Twenty chips of labels and
// messages is a few kilobytes, so this is generous while still refusing an
// unbounded read.
const quickActionsMaxBodyBytes = 1 << 20

// HandleGetQuickActionsConfig reports the composer's quick-action strip.
//
// When the key is absent the seeded starters are returned so an install that
// never opened settings still renders the pre-existing three pills, and
// nothing is persisted as a side effect of asking.
//
// The nil-vs-empty distinction is load-bearing: nil Chips means "no config yet"
// (seed), while a non-nil EMPTY list means the user deliberately deleted every
// chip and must keep seeing no strip. Branching on len(Chips) == 0 would
// resurrect all three starters for that user.
func (h *Handler) HandleGetQuickActionsConfig(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	cfg := config.QuickActionsConfig{}
	if h.cfg != nil {
		cfg = h.cfg.Ocode.QuickActions
	}
	h.mu.Unlock()

	// Read the nil-vs-empty state while still holding the snapshot. Do the
	// substitution before Normalize so a stored empty list is never re-seeded.
	if cfg.Chips == nil {
		cfg = config.SeedQuickActions()
	}
	normalized := config.NormalizeQuickActions(cfg)
	if err := normalized.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, normalized)
}

// HandleSetQuickActionsConfig validates and persists the strip, replacing only
// the quick_actions key so sibling keys such as chat_verbosity are never read,
// rewritten, or clobbered.
//
// The event payload is diagnostic only; clients re-fetch because the unified
// bus envelope carries no host identity.
func (h *Handler) HandleSetQuickActionsConfig(w http.ResponseWriter, r *http.Request) {
	req, err := decodeQuickActionsRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalized := config.NormalizeQuickActions(req)
	if err := normalized.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := config.SaveOcodeQuickActions(normalized); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save quick actions config: "+err.Error())
		return
	}
	h.mu.Lock()
	if h.cfg != nil {
		h.cfg.Ocode.QuickActions = normalized
	}
	h.mu.Unlock()
	h.bus.Publish("quick_actions_changed", "", "", map[string]any{
		"config": normalized,
	})
	writeJSON(w, http.StatusOK, normalized)
}

// decodeQuickActionsRequest is a strict read of the PUT body. readBodyJSON is a
// plain json.Decode and would silently drop an unknown top-level key, so a
// typo'd payload such as {"chip": [...]} would be stored as "the starters" with
// no error. The spec requires an unknown field anywhere in the block to be
// reported; per-chip strictness lives in QuickActionChip.UnmarshalJSON.
//
// A malformed body reports the generic house message, while a rejected unknown
// field carries its own text so the settings form can show what to fix.
func decodeQuickActionsRequest(w http.ResponseWriter, r *http.Request) (config.QuickActionsConfig, error) {
	r.Body = http.MaxBytesReader(w, r.Body, quickActionsMaxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return config.QuickActionsConfig{}, errors.New("invalid request body")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return config.QuickActionsConfig{}, errors.New("invalid request body")
	}
	for key := range fields {
		if key != "chips" {
			return config.QuickActionsConfig{}, fmt.Errorf("unknown quick_actions field %q", key)
		}
	}
	var req config.QuickActionsConfig
	if err := json.Unmarshal(body, &req); err != nil {
		return config.QuickActionsConfig{}, err
	}
	return req, nil
}
