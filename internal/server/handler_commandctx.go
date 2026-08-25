package server

import (
	"fmt"
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
	// A ?project= query selects which registered project root the command
	// runs against (same trust boundary as the git endpoints); absent means
	// the server workdir.
	workDir, ok := h.gitProjectDir(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown project")
		return
	}
	if workDir == "" {
		workDir = "."
	}

	var prompt string
	var err error
	switch name {
	case "standup":
		prompt, err = commandctx.Standup(workDir, h.lspManagerFor(workDir))
	case "changes":
		prompt, err = commandctx.Changes(workDir, h.lspManagerFor(workDir))
	case "review":
		prompt, err = commandctx.Review(workDir, args)
	case "learn":
		prompt, err = commandctx.Learn(workDir, args)
	case "doc-sync":
		mode := "session"
		if len(args) > 0 {
			switch strings.ToLower(args[0]) {
			case "all", "session":
				mode = strings.ToLower(args[0])
			default:
				writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown mode %q — use 'session' or 'all'", args[0]))
				return
			}
		}
		prompt, err = commandctx.DocSync(workDir, mode)
	case "mem-update":
		prompt, err = commandctx.MemUpdate(workDir, args)
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
