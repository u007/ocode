package server

import (
	"fmt"
	"net/http"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// promptOverrideRow is one entry of the System Prompts settings group: a
// catalog prompt joined with the user's override for it. Builtin is the exact
// text the runtime sends, so the UI can show it for review. ReadOnly entries
// are never overridable: the UI shows Builtin and no controls for them.
type promptOverrideRow struct {
	Key         string           `json:"key"`
	Label       string           `json:"label"`
	Description string           `json:"description"`
	Kind        agent.PromptKind `json:"kind"`
	Builtin     string           `json:"builtin"`
	ReadOnly    bool             `json:"read_only"`
	Enabled     bool             `json:"enabled"`
	Text        string           `json:"text"`
}

// newPromptOverrideRow reads the live override store. A key with no stored
// override is an enabled prompt with no add-on text, which is the default.
func newPromptOverrideRow(e agent.PromptCatalogEntry) promptOverrideRow {
	o, _ := config.PromptOverrideFor(e.Key)
	return promptOverrideRow{
		Key:         e.Key,
		Label:       e.Label,
		Description: e.Description,
		Kind:        e.Kind,
		Builtin:     e.Builtin,
		ReadOnly:    e.ReadOnly,
		Enabled:     !o.Disabled,
		Text:        o.Text,
	}
}

// HandleListPromptOverrides lists every built-in prompt in the catalog in
// catalog order, each joined with its override.
func (h *Handler) HandleListPromptOverrides(w http.ResponseWriter, r *http.Request) {
	catalog := agent.PromptCatalog()
	rows := make([]promptOverrideRow, 0, len(catalog))
	for _, e := range catalog {
		rows = append(rows, newPromptOverrideRow(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"prompts": rows})
}

// HandleSetPromptOverride saves the override for the catalog prompt named by
// the path key and responds with the updated entry.
func (h *Handler) HandleSetPromptOverride(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	entry, ok := agent.PromptCatalogEntryFor(key)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown prompt key: "+key)
		return
	}
	if entry.ReadOnly {
		writeError(w, http.StatusForbidden, "prompt is read-only and cannot be overridden: "+key)
		return
	}
	var req struct {
		Enabled bool   `json:"enabled"`
		Text    string `json:"text"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Text) > config.MaxPromptOverrideBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("prompt override text exceeds the %d byte limit", config.MaxPromptOverrideBytes))
		return
	}
	if err := config.SavePromptOverride(key, config.PromptOverride{Disabled: !req.Enabled, Text: req.Text}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save prompt override: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, newPromptOverrideRow(entry))
}
