package server

import (
	"log"
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/commandctx"
)

// HandleCommandContext builds the LLM prompt behind a repo-analysis slash
// command (/standup, /changes, /review) using the same shared commandctx
// package the TUI calls, so web/desktop get a byte-identical prompt.
//
// Response: {"prompt": "..."} on success, 4xx with an error message when the
// command is unknown or there is nothing to summarise (e.g. empty repo).
func (h *Handler) HandleCommandContext(w http.ResponseWriter, r *http.Request, name string) {
	args := parseCommandArgs(r.URL.Query().Get("args"))
	workDir := h.workDir
	if workDir == "" {
		workDir = "."
	}

	var prompt string
	var err error
	switch name {
	case "standup":
		prompt, err = commandctx.Standup(workDir, h.sharedLSPManager())
	case "changes":
		prompt, err = commandctx.Changes(workDir, h.sharedLSPManager())
	case "review":
		prompt, err = commandctx.Review(workDir, args)
	default:
		writeError(w, http.StatusNotFound, "unknown command-context: "+name)
		return
	}
	if err != nil {
		log.Printf("command-context %s: %v", name, err)
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"prompt": prompt})
}

// parseCommandArgs splits a command's argument string the same way the TUI
// does (fields split on whitespace), so /review web/src/App.tsx behaves like
// the TUI /review web/src/App.tsx.
func parseCommandArgs(args string) []string {
	if args == "" {
		return nil
	}
	return strings.Fields(args)
}
