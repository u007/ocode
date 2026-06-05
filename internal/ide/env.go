package ide

import "os"

// InVSCode reports whether ocode is running inside a VS Code integrated
// terminal. VS Code (and forks like Cursor/VSCodium/Windsurf) set
// TERM_PROGRAM=vscode for their integrated terminals.
func InVSCode() bool {
	return os.Getenv("TERM_PROGRAM") == "vscode"
}
