// Package ide connects ocode to a running VS Code instance via the Claude Code
// extension's WebSocket + MCP protocol. It discovers the extension's lock file
// under ~/.claude/ide, opens a WebSocket, performs the MCP handshake, and then
// streams the editor's current selection, open tabs, and @-mentions back to the
// TUI as Update values.
//
// The protocol mirrors opencode's editor context client
// (packages/opencode/src/cli/cmd/tui/context/editor.ts): MCP protocol version
// "2025-11-25", `selection_changed` / `at_mentioned` push notifications, and
// `tools/call` for getOpenEditors / getCurrentSelection.
package ide

import "strconv"

// MCPProtocolVersion is the MCP handshake version the Claude Code extension speaks.
const MCPProtocolVersion = "2025-11-25"

// UpdateKind tags an Update delivered to the TUI.
type UpdateKind int

const (
	// UpdateConnected is emitted once the WebSocket + MCP handshake completes.
	UpdateConnected UpdateKind = iota
	// UpdateDisconnected is emitted when the connection drops (a reconnect is pending).
	UpdateDisconnected
	// UpdateSelection carries the editor's current text selection.
	UpdateSelection
	// UpdateOpenEditors carries the list of open editor tabs.
	UpdateOpenEditors
	// UpdateMention carries an at-mention (Cmd+Alt+K / explicit @-reference).
	UpdateMention
)

// Update is a tagged event delivered from the IDE client goroutine to the TUI.
// Only the field matching Kind is populated.
type Update struct {
	Kind        UpdateKind
	Selection   *Selection
	OpenEditors []Editor
	Mention     *Mention
}

// Range is a single highlighted span. Lines and characters are 0-based on the
// wire (LSP convention); callers add 1 for display.
type Range struct {
	StartLine int
	StartChar int
	EndLine   int
	EndChar   int
	Text      string
}

// Selection is the editor's current selection in one file. A selection with no
// non-empty range (cursor only) still reports FilePath.
type Selection struct {
	FilePath string
	Ranges   []Range
}

// Editor is a single open tab in the IDE.
type Editor struct {
	FilePath string
	Label    string
	Active   bool
	Dirty    bool
}

// Mention is an explicit @-reference pushed by the extension.
type Mention struct {
	FilePath  string
	LineStart int
	LineEnd   int
}

// LineSpan returns the 1-based inclusive line range of the selection's first
// range, for display. ok is false when there is no range (cursor only).
func (s *Selection) LineSpan() (start, end int, ok bool) {
	if s == nil || len(s.Ranges) == 0 {
		return 0, 0, false
	}
	r := s.Ranges[0]
	return r.StartLine + 1, r.EndLine + 1, true
}

// SelectionKey produces a stable identity for a selection so the TUI can tell
// whether the selection actually changed (mirrors editor.ts editorSelectionKey).
func SelectionKey(s *Selection) string {
	if s == nil {
		return ""
	}
	out := s.FilePath
	for _, r := range s.Ranges {
		out += "\x00" + strconv.Itoa(r.StartLine) + "\x00" + strconv.Itoa(r.StartChar) +
			"\x00" + strconv.Itoa(r.EndLine) + "\x00" + strconv.Itoa(r.EndChar) + "\x00" + r.Text
	}
	return out
}
