package commandctx

import (
	_ "embed"
	"strings"
)

// initializePromptTemplate is the /init analysis prompt. It lives here rather
// than in internal/tui so the TUI and the server's /api/command-context/init
// endpoint emit a byte-identical prompt — the same reason Learn/DocSync/Paths
// were moved into this package.
//
//go:embed initialize_prompt.txt
var initializePromptTemplate string

// Init builds the /init prompt: create or update AGENTS.md for the repo.
// args is the optional free-text focus (TUI: `/init <focus>`), interpolated
// where the template has its $ARGUMENTS placeholder.
func Init(args []string) string {
	return strings.ReplaceAll(initializePromptTemplate, "$ARGUMENTS", strings.Join(args, " "))
}
