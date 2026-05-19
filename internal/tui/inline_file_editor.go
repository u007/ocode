package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

type inlineEditorMode int

const (
	inlineEditorNormal inlineEditorMode = iota
	inlineEditorInsert
	inlineEditorCommand
)

type inlineFileEditor struct {
	lines       []string
	trailingNL  bool
	cursorRow   int
	cursorCol   int
	mode        inlineEditorMode
	command     string
	lastCommand string
	dirty       bool
}

func newInlineFileEditor(content string) inlineFileEditor {
	ed := inlineFileEditor{mode: inlineEditorNormal, trailingNL: strings.HasSuffix(content, "\n")}
	trimmed := strings.TrimSuffix(content, "\n")
	ed.lines = strings.Split(trimmed, "\n")
	if len(ed.lines) == 0 {
		ed.lines = []string{""}
	}
	return ed
}

func (e inlineFileEditor) update(msg tea.KeyPressMsg) inlineFileEditor {
	e.lastCommand = ""
	switch e.mode {
	case inlineEditorInsert:
		return e.updateInsert(msg)
	case inlineEditorCommand:
		return e.updateCommand(msg)
	default:
		return e.updateNormal(msg)
	}
}

func (e inlineFileEditor) updateNormal(msg tea.KeyPressMsg) inlineFileEditor {
	switch msg.String() {
	case "i":
		e.mode = inlineEditorInsert
	case "a":
		e.cursorCol = len(e.currentLine())
		e.mode = inlineEditorInsert
	case ":":
		e.mode = inlineEditorCommand
		e.command = ""
	case "h", "left":
		if e.cursorCol > 0 {
			e.cursorCol--
		}
	case "l", "right":
		if e.cursorCol < len(e.currentLine())-1 {
			e.cursorCol++
		}
	case "j", "down":
		if e.cursorRow < len(e.lines)-1 {
			e.cursorRow++
			e.clampCursorCol()
		}
	case "k", "up":
		if e.cursorRow > 0 {
			e.cursorRow--
			e.clampCursorCol()
		}
	case "0":
		e.cursorCol = 0
	case "$":
		lineLen := len(e.currentLine())
		if lineLen > 0 {
			e.cursorCol = lineLen - 1
		}
	}
	return e
}

func (e inlineFileEditor) updateInsert(msg tea.KeyPressMsg) inlineFileEditor {
	switch msg.String() {
	case "esc":
		e.mode = inlineEditorNormal
		if e.cursorCol > 0 {
			e.cursorCol--
		}
		return e
	case "enter", "ctrl+j", "ctrl+m":
		line := e.currentLine()
		before := line[:e.cursorCol]
		after := line[e.cursorCol:]
		e.lines[e.cursorRow] = before
		e.lines = append(e.lines[:e.cursorRow+1], append([]string{after}, e.lines[e.cursorRow+1:]...)...)
		e.cursorRow++
		e.cursorCol = 0
		e.dirty = true
		return e
	case "backspace":
		if e.cursorCol > 0 {
			line := e.currentLine()
			e.lines[e.cursorRow] = line[:e.cursorCol-1] + line[e.cursorCol:]
			e.cursorCol--
			e.dirty = true
		}
		return e
	}
	if msg.Text != "" {
		line := e.currentLine()
		e.lines[e.cursorRow] = line[:e.cursorCol] + msg.Text + line[e.cursorCol:]
		e.cursorCol += len(msg.Text)
		e.dirty = true
	}
	return e
}

func (e inlineFileEditor) updateCommand(msg tea.KeyPressMsg) inlineFileEditor {
	switch msg.String() {
	case "esc":
		e.mode = inlineEditorNormal
		e.command = ""
	case "enter", "ctrl+j", "ctrl+m":
		e.lastCommand = e.command
		e.command = ""
		e.mode = inlineEditorNormal
	case "backspace":
		if len(e.command) > 0 {
			e.command = e.command[:len(e.command)-1]
		}
	default:
		if msg.Text != "" {
			e.command += msg.Text
		}
	}
	return e
}

func (e inlineFileEditor) content() string {
	content := strings.Join(e.lines, "\n")
	if e.trailingNL {
		content += "\n"
	}
	return content
}

func (e inlineFileEditor) view(width int, height int) string {
	if height < 1 {
		height = 1
	}
	visible := e.lines
	if len(visible) > height-1 {
		visible = visible[:height-1]
	}
	status := "-- NORMAL --"
	if e.mode == inlineEditorInsert {
		status = "-- INSERT --"
	}
	if e.mode == inlineEditorCommand {
		status = "-- COMMAND -- :" + e.command
	}
	if e.dirty {
		status += " [+]"
	}
	return strings.Join(append(visible, status), "\n")
}

func (e inlineFileEditor) currentLine() string {
	if e.cursorRow < 0 || e.cursorRow >= len(e.lines) {
		return ""
	}
	return e.lines[e.cursorRow]
}

func (e *inlineFileEditor) clampCursorCol() {
	lineLen := len(e.currentLine())
	if lineLen == 0 {
		e.cursorCol = 0
		return
	}
	if e.cursorCol >= lineLen {
		e.cursorCol = lineLen - 1
	}
}

func (e *inlineFileEditor) markClean() {
	e.dirty = false
}