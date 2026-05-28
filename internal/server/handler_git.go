package server

import (
	"net/http"
	"os/exec"
	"strings"
)

type GitStatus struct {
	Branch       string   `json:"branch"`
	StagedFiles  []string `json:"staged_files"`
	ChangedFiles []string `json:"changed_files"`
	HasChanges   bool     `json:"has_changes"`
}

func (h *Handler) HandleGitStatus(w http.ResponseWriter, r *http.Request) {
	branch := ""
	if out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}

	var staged, changed []string
	if out, err := exec.Command("git", "diff", "--name-only", "--cached").Output(); err == nil {
		for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if f != "" {
				staged = append(staged, f)
			}
		}
	}
	if out, err := exec.Command("git", "diff", "--name-only").Output(); err == nil {
		for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if f != "" {
				changed = append(changed, f)
			}
		}
	}

	writeJSON(w, http.StatusOK, GitStatus{
		Branch:       branch,
		StagedFiles:  staged,
		ChangedFiles: changed,
		HasChanges:   len(staged) > 0 || len(changed) > 0,
	})
}
