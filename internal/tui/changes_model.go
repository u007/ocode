// Package tui provides the Bubble Tea TUI for ocode.
//
// The changesModel renders the per-session file-changes tab, showing files
// the current chat session has added or edited, with per-file diff preview
// and undo (whole-file and block-level).
package tui

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/u007/ocode/internal/changes"
)

// confirmState tracks a pending undo confirmation dialog.
type confirmState struct {
	action string // "undo-file" or "undo-block"
	path   string
}

// changesModel is the per-session file-changes tab model. It shows files the
// current chat session has added or edited, and provides per-file undo.
type changesModel struct {
	files       []changes.FileChange
	showDetails bool
	statusMsg   string
	width       int
	height      int

	// getRegistry is the source of truth for file changes. Set externally
	// during Phase 11 (agent wiring); nil until then.
	getRegistry func() *changes.Registry

	// confirm is set when u/U is pressed and waiting for y/n confirmation.
	confirm *confirmState

	// diffCache caches rendered diffs keyed by OriginalPath. Populated on
	// "enter" via changes.RenderDiff; cleared when the file list refreshes.
	diffCache map[string]string

	// list is the ListBox for the scrollable file list.
	list *ListBox
}

// withRegistry returns a copy of the model with getRegistry set to fn.
// Used to wire (or clear) the agent registry without replacing the full model.
func (m changesModel) withRegistry(fn func() *changes.Registry) changesModel {
	m.getRegistry = fn
	return m
}

// NewChangesModel returns an empty changes tab model.
func NewChangesModel() changesModel {
	list := NewListBox(0, 0)
	list.SetWrapEnabled(true)
	return changesModel{
		list:      list,
		diffCache: make(map[string]string),
	}
}

// refreshFiles reloads the file list from the registry.
func (m changesModel) refreshFiles() changesModel {
	if m.getRegistry != nil {
		reg := m.getRegistry()
		if reg != nil {
			m.files = reg.List()
		}
	}
	return m
}

// Update dispatches messages to the changes sub-model.
func (m changesModel) Update(msg tea.Msg, w, h int) (changesModel, tea.Cmd) {
	m.width = w
	m.height = h

	if m.confirm != nil {
		return m.updateConfirm(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey processes keyboard events when no confirm dialog is active.
func (m changesModel) handleKey(msg tea.KeyPressMsg) (changesModel, tea.Cmd) {
	// Clear status message on any navigation keypress.
	m.statusMsg = ""
	// View() also calls refreshFiles(), but View has a value receiver, so
	// its refreshed copy is discarded after rendering and never reaches
	// the model Update() persists. Refresh here so m.files is populated
	// on the model bubbletea actually keeps between messages.
	m = m.refreshFiles()

	if len(m.files) == 0 {
		return m, nil
	}

	switch msg.String() {
	case "j", "down":
		next := (m.list.Selected() + 1) % len(m.files)
		m.list.SetSelected(next)
	case "k", "up":
		n := len(m.files)
		next := (m.list.Selected() - 1 + n) % n
		m.list.SetSelected(next)
	case "g", "home":
		m.list.SetSelected(0)
	case "G", "end":
		m.list.SetSelected(len(m.files) - 1)
	case "enter":
		// Compute and cache the diff for the selected file.
		sel := m.list.Selected()
		if sel >= 0 && sel < len(m.files) {
			f := m.files[sel]
			if _, cached := m.diffCache[f.OriginalPath]; !cached {
				diff, err := changes.RenderDiff(f.FirstBackupPath, f.OriginalPath)
				if err != nil {
					log.Printf("changes: RenderDiff(%q, %q): %v", f.FirstBackupPath, f.OriginalPath, err)
					m.diffCache[f.OriginalPath] = "(error rendering diff)"
				} else {
					m.diffCache[f.OriginalPath] = diff
				}
			}
		}
	case "?":
		m.showDetails = !m.showDetails
	case "u":
		m = m.initUndo("undo-file")
	case "U":
		m = m.initUndo("undo-block")
	case "esc":
		if m.showDetails {
			m.showDetails = false
		}
	}
	return m, nil
}

// initUndo begins the undo confirmation flow.
func (m changesModel) initUndo(action string) changesModel {
	sel := m.list.Selected()
	if sel < 0 || sel >= len(m.files) {
		return m
	}
	f := m.files[sel]
	if !f.Undoable {
		m.statusMsg = "this file's only change came from a bash command and cannot be undone from the changes tab"
		return m
	}
	m.confirm = &confirmState{action: action, path: f.OriginalPath}
	return m
}

// updateConfirm handles key events while the confirm dialog is active.
func (m changesModel) updateConfirm(msg tea.Msg) (changesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "y", "Y":
			return m.executeUndo()
		case "n", "N", "esc":
			m.confirm = nil
		}
	}
	return m, nil
}

// executeUndo performs the confirmed undo action.
func (m changesModel) executeUndo() (changesModel, tea.Cmd) {
	if m.getRegistry == nil || m.confirm == nil {
		m.confirm = nil
		return m, nil
	}
	reg := m.getRegistry()
	if reg == nil {
		m.confirm = nil
		return m, nil
	}

	var err error
	switch m.confirm.action {
	case "undo-file":
		err = reg.UndoFile(m.confirm.path)
	case "undo-block":
		tcid, tcidErr := reg.LatestToolCall(m.confirm.path)
		if tcidErr == nil {
			err = reg.UndoBlock(m.confirm.path, tcid)
		} else {
			err = tcidErr
		}
	}
	if err != nil {
		m.statusMsg = err.Error()
	} else {
		m.statusMsg = "undo complete"
	}
	m.confirm = nil
	m = m.refreshFiles()
	return m, nil
}

// View renders the changes tab content. Includes the tab bar header like other
// non-chat tabs (files, git, log).
func (m changesModel) View(w, h int, styles Styles) string {
	m.width = w
	m.height = h
	m = m.refreshFiles()

	// --- Build header row ---
	tabBar := renderTabBar(tabChanges, false)
	exitBtn := hintStyle.Padding(0, 1).Render("\u2715 exit")
	headerLeft := appHeaderLeftPad + styles.Header.Render("\u25c6 ocode  Changes") + appHeaderHintGap + hintStyle.Render("opencode clone")
	headerPad := w - lipgloss.Width(headerLeft) - lipgloss.Width(tabBar) - lipgloss.Width(exitBtn)
	if headerPad < 0 {
		headerPad = 0
	}
	renderedHeader := appHeaderTopPad + headerLeft + strings.Repeat(" ", headerPad) + tabBar + exitBtn

	// --- Compute content area ---
	var contentH int
	if m.confirm != nil {
		contentH = h - appHeaderHeight - 2 // header + status bar, dim dialog backdrop
	} else {
		contentH = h - appHeaderHeight - 1 // header + status bar
	}
	if contentH < 1 {
		contentH = 1
	}

	leftW := w * 35 / 100
	if leftW < 10 {
		leftW = 10
	}
	rightW := w - leftW
	if rightW < 2 {
		rightW = 2
	}

	// --- Left pane: file list via ListBox ---
	leftPaneW := leftW - 4 // account for border (2) + padding (2)
	leftPaneH := contentH - 2
	if leftPaneW < 1 {
		leftPaneW = 1
	}
	if leftPaneH < 1 {
		leftPaneH = 1
	}

	m.list.SetSize(leftPaneW, leftPaneH)
	sel := m.list.Selected()

	var leftContent string
	if len(m.files) == 0 {
		leftContent = styles.Hint.Render(truncateToWidth("no changes in this session yet.", leftPaneW))
	} else {
		m.list.SetData(len(m.files), func(idx, width int, selected bool) string {
			f := m.files[idx]
			return m.renderFileRow(f, width, selected, styles, idx)
		})
		// Sync selection without forcing scroll.
		m.list.SetSelectedForRender(sel)

		leftContent = m.list.Render()
	}
	leftPane := styles.Border.Width(leftW).Height(contentH).Render(leftContent)

	// --- Right pane: diff preview ---
	var rightContent string
	if len(m.files) == 0 {
		rightContent = styles.Hint.Render(truncateToWidth("files the agent edits will appear here.", rightW-4))
		// Pad to full pane height to keep layout stable.
		spacer := strings.Repeat("\n", contentH-3)
		rightContent += spacer
	} else if sel >= 0 && sel < len(m.files) {
		f := m.files[sel]
		fname := filepath.Base(f.OriginalPath)

		if cachedDiff, ok := m.diffCache[f.OriginalPath]; ok && cachedDiff != "" {
			// Render the cached diff using the unified diff renderer.
			rendered := renderUnifiedDiff(cachedDiff, styles)
			rightContent = lipgloss.NewStyle().Width(rightW - 4).Render(rendered)
		} else {
			rightContent = lipgloss.NewStyle().Width(rightW - 4).Render(styles.Hint.Render("<diff for " + fname + ">"))
		}
	}
	rightPane := styles.Border.Width(rightW).Height(contentH).Render(rightContent)

	mainRow := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)

	// --- Status bar ---
	var statusBar string
	if m.statusMsg != "" {
		statusBar = styles.Error.Render(m.statusMsg)
	} else {
		statusBar = styles.Hint.Render(m.renderHints())
	}
	statusBar = lipgloss.NewStyle().Width(w).MaxHeight(1).Render(statusBar)

	body := renderedHeader + "\n" + mainRow + "\n" + statusBar

	// If confirm dialog active, overlay it.
	if m.confirm != nil {
		body = m.renderConfirmOverlay(w, h, styles, body)
	}

	return body
}

// renderFileRow renders a single row in the file list.
func (m changesModel) renderFileRow(f changes.FileChange, width int, selected bool, styles Styles, idx int) string {
	status := f.Status.String()
	relPath := filepath.Base(f.OriginalPath)

	var b strings.Builder
	b.WriteString(status)
	b.WriteString(" ")
	b.WriteString(relPath)

	// Show author details if toggled.
	if m.showDetails && len(f.Authors) > 0 {
		b.WriteString("  (")
		for i, author := range f.Authors {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fmt.Sprintf("%s · %d edits", author.AgentName, author.Changes))
		}
		b.WriteString(")")
	}

	// Show bash marker.
	if f.LastBashCommand != "" {
		b.WriteString(" (bash)")
	}

	line := b.String()
	if selected {
		return selectedStyle.Width(width).Render(line)
	}
	return lipgloss.NewStyle().Width(width).Render(line)
}

// renderHints returns the keybinding hint line.
func (m changesModel) renderHints() string {
	hints := []string{
		"j/k navigate",
		"g top G bottom",
		"enter diff",
		"? details",
		"u undo-file",
		"U undo-block",
	}
	return strings.Join(hints, "  ·  ")
}

// renderConfirmOverlay renders the undo confirmation dialog on top of the
// current tab body using the Dialog component from component_dialog.go.
func (m changesModel) renderConfirmOverlay(w, h int, styles Styles, body string) string {
	if m.confirm == nil {
		return body
	}

	action := "Undo File"
	if m.confirm.action == "undo-block" {
		action = "Undo Block"
	}

	// Build a simple text body with the path and y/n hint.
	dialogBody := filepath.Base(m.confirm.path) + "\n\n" + styles.Hint.Render("y/yes confirm  n/no/esc cancel")

	btnConfigs := []ButtonConfig{
		{Label: "Yes", Variant: ButtonPrimary},
		{Label: "No", Variant: ButtonNormal},
	}
	dlg := NewDialog(action, dialogBody, btnConfigs, 50, 7)
	dlg.TermWidth = w
	dlg.TermHeight = h

	dialogStr := dlg.Render()

	// Center on screen.
	x := (w - dlg.width) / 2
	y := (h - dlg.height) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	return compositeOverlay(body, dialogStr, x, y)
}
