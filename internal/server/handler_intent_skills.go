package server

import (
	"fmt"
	"net/http"

	"github.com/u007/ocode/internal/skill"
)

// HandleListIntentSkills serves GET /api/skills/tanstack-intent: the skills of
// the latest @tanstack/intent on npm, for the web checklist. Every call fetches
// the registry again, so the list is never older than the last request.
func (h *Handler) HandleListIntentSkills(w http.ResponseWriter, r *http.Request) {
	pkg, err := skill.FetchIntentPackage(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("%s: %v", skill.IntentPackageName, err))
		return
	}
	type entry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	skills := make([]entry, 0, len(pkg.Skills))
	for _, s := range pkg.Skills {
		skills = append(skills, entry{Name: s.Name, Description: s.Description})
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": pkg.Version, "skills": skills})
}

// HandleInstallIntentSkills serves POST /api/skills/tanstack-intent/install with
// {"names": [...]}. It fetches the latest package again and installs the named
// skills into ~/.claude/skills, the same as the TUI picker.
func (h *Handler) HandleInstallIntentSkills(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Names []string `json:"names"`
	}
	if err := readBodyJSON(r, &req); err != nil || len(req.Names) == 0 {
		writeError(w, http.StatusBadRequest, "names is required")
		return
	}
	pkg, err := skill.FetchIntentPackage(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("%s: %v", skill.IntentPackageName, err))
		return
	}
	report, err := skill.InstallIntentSkillsToClaude(pkg, req.Names)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"report": report})
}
