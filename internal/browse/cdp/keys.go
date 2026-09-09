package cdp

import "strings"

// windowsVirtualKeyCodes maps KeyboardEvent.code → Windows virtual key code.
// Chrome's renderer keys its editing behaviour (delete backward, caret moves,
// form submit, focus traversal) off windowsVirtualKeyCode, not the `key`
// string: a rawKeyDown for Backspace without VK 8 is a no-op. Values mirror
// Puppeteer's USKeyboardLayout.
var windowsVirtualKeyCodes = map[string]int{
	"Backspace": 8, "Tab": 9, "Enter": 13, "NumpadEnter": 13,
	"ShiftLeft": 16, "ShiftRight": 16, "ControlLeft": 17, "ControlRight": 17,
	"AltLeft": 18, "AltRight": 18, "Pause": 19, "CapsLock": 20, "Escape": 27,
	"Space": 32, "PageUp": 33, "PageDown": 34, "End": 35, "Home": 36,
	"ArrowLeft": 37, "ArrowUp": 38, "ArrowRight": 39, "ArrowDown": 40,
	"PrintScreen": 44, "Insert": 45, "Delete": 46,
	"MetaLeft": 91, "MetaRight": 92, "ContextMenu": 93,
	"Numpad0": 96, "Numpad1": 97, "Numpad2": 98, "Numpad3": 99, "Numpad4": 100,
	"Numpad5": 101, "Numpad6": 102, "Numpad7": 103, "Numpad8": 104, "Numpad9": 105,
	"NumpadMultiply": 106, "NumpadAdd": 107, "NumpadSubtract": 109,
	"NumpadDecimal": 110, "NumpadDivide": 111,
	"F1": 112, "F2": 113, "F3": 114, "F4": 115, "F5": 116, "F6": 117,
	"F7": 118, "F8": 119, "F9": 120, "F10": 121, "F11": 122, "F12": 123,
	"NumLock": 144, "ScrollLock": 145,
	"Semicolon": 186, "Equal": 187, "Comma": 188, "Minus": 189, "Period": 190,
	"Slash": 191, "Backquote": 192, "BracketLeft": 219, "Backslash": 220,
	"BracketRight": 221, "Quote": 222,
}

// virtualKeyCode resolves the Windows VK for a physical key code. Letters and
// digits are derived (KeyA→65, Digit0→48) so Option+A ("å") still carries
// VK 65. Unknown codes yield 0 (omitted from the CDP call).
func virtualKeyCode(code string) int {
	if vk, ok := windowsVirtualKeyCodes[code]; ok {
		return vk
	}
	if len(code) == 4 && strings.HasPrefix(code, "Key") && code[3] >= 'A' && code[3] <= 'Z' {
		return int(code[3])
	}
	if len(code) == 6 && strings.HasPrefix(code, "Digit") && code[5] >= '0' && code[5] <= '9' {
		return int(code[5])
	}
	return 0
}

// macEditingCommands maps "<Shift+><Control+><Alt+><Meta+><code>" to the
// NSResponder editing selectors Chrome needs on macOS. On mac the editor
// commands come from the browser process, which Input.dispatchKeyEvent
// bypasses, so headless Chrome only deletes/selects/moves when told
// explicitly. Same table Playwright ships (minus paste: the SPA bridges the
// host clipboard through Input.insertText instead, so Chrome's own clipboard
// must never be pasted).
var macEditingCommands = map[string][]string{
	"Backspace":   {"deleteBackward"},
	"Enter":       {"insertNewline"},
	"NumpadEnter": {"insertNewline"},
	"Escape":      {"cancelOperation"},
	"ArrowUp":     {"moveUp"},
	"ArrowDown":   {"moveDown"},
	"ArrowLeft":   {"moveLeft"},
	"ArrowRight":  {"moveRight"},
	"Delete":      {"deleteForward"},
	"Home":        {"scrollToBeginningOfDocument"},
	"End":         {"scrollToEndOfDocument"},
	"PageUp":      {"scrollPageUp"},
	"PageDown":    {"scrollPageDown"},

	"Shift+Backspace":   {"deleteBackward"},
	"Shift+Enter":       {"insertNewline"},
	"Shift+NumpadEnter": {"insertNewline"},
	"Shift+Escape":      {"cancelOperation"},
	"Shift+ArrowUp":     {"moveUpAndModifySelection"},
	"Shift+ArrowDown":   {"moveDownAndModifySelection"},
	"Shift+ArrowLeft":   {"moveLeftAndModifySelection"},
	"Shift+ArrowRight":  {"moveRightAndModifySelection"},
	"Shift+Delete":      {"deleteForward"},
	"Shift+Home":        {"moveToBeginningOfDocumentAndModifySelection"},
	"Shift+End":         {"moveToEndOfDocumentAndModifySelection"},
	"Shift+PageUp":      {"pageUpAndModifySelection"},
	"Shift+PageDown":    {"pageDownAndModifySelection"},

	"Control+Tab":         {"selectNextKeyView"},
	"Control+Enter":       {"insertLineBreak"},
	"Control+NumpadEnter": {"insertLineBreak"},
	"Control+KeyA":        {"moveToBeginningOfParagraph"},
	"Control+KeyB":        {"moveBackward"},
	"Control+KeyD":        {"deleteForward"},
	"Control+KeyE":        {"moveToEndOfParagraph"},
	"Control+KeyF":        {"moveForward"},
	"Control+KeyH":        {"deleteBackward"},
	"Control+KeyK":        {"deleteToEndOfParagraph"},
	"Control+KeyN":        {"moveDown"},
	"Control+KeyO":        {"insertNewlineIgnoringFieldEditor", "moveBackward"},
	"Control+KeyP":        {"moveUp"},
	"Control+KeyT":        {"transpose"},
	"Control+KeyV":        {"pageDown"},
	"Control+KeyY":        {"yank"},
	"Control+Backspace":   {"deleteBackwardByDecomposingPreviousCharacter"},
	"Control+ArrowUp":     {"scrollPageUp"},
	"Control+ArrowDown":   {"scrollPageDown"},
	"Control+ArrowLeft":   {"moveToLeftEndOfLine"},
	"Control+ArrowRight":  {"moveToRightEndOfLine"},

	"Shift+Control+Enter":      {"insertLineBreak"},
	"Shift+Control+KeyA":       {"moveToBeginningOfParagraphAndModifySelection"},
	"Shift+Control+KeyB":       {"moveBackwardAndModifySelection"},
	"Shift+Control+KeyE":       {"moveToEndOfParagraphAndModifySelection"},
	"Shift+Control+KeyF":       {"moveForwardAndModifySelection"},
	"Shift+Control+KeyN":       {"moveDownAndModifySelection"},
	"Shift+Control+KeyP":       {"moveUpAndModifySelection"},
	"Shift+Control+ArrowUp":    {"scrollPageUp"},
	"Shift+Control+ArrowDown":  {"scrollPageDown"},
	"Shift+Control+ArrowLeft":  {"moveToLeftEndOfLineAndModifySelection"},
	"Shift+Control+ArrowRight": {"moveToRightEndOfLineAndModifySelection"},

	"Alt+Backspace":  {"deleteWordBackward"},
	"Alt+Enter":      {"insertNewlineIgnoringFieldEditor"},
	"Alt+Escape":     {"complete"},
	"Alt+ArrowUp":    {"moveBackward", "moveToBeginningOfParagraph"},
	"Alt+ArrowDown":  {"moveForward", "moveToEndOfParagraph"},
	"Alt+ArrowLeft":  {"moveWordLeft"},
	"Alt+ArrowRight": {"moveWordRight"},
	"Alt+Delete":     {"deleteWordForward"},
	"Alt+PageUp":     {"pageUp"},
	"Alt+PageDown":   {"pageDown"},

	"Shift+Alt+ArrowUp":    {"moveParagraphBackwardAndModifySelection"},
	"Shift+Alt+ArrowDown":  {"moveParagraphForwardAndModifySelection"},
	"Shift+Alt+ArrowLeft":  {"moveWordLeftAndModifySelection"},
	"Shift+Alt+ArrowRight": {"moveWordRightAndModifySelection"},

	"Meta+Backspace":  {"deleteToBeginningOfLine"},
	"Meta+ArrowUp":    {"moveToBeginningOfDocument"},
	"Meta+ArrowDown":  {"moveToEndOfDocument"},
	"Meta+ArrowLeft":  {"moveToLeftEndOfLine"},
	"Meta+ArrowRight": {"moveToRightEndOfLine"},
	"Meta+KeyA":       {"selectAll"},
	"Meta+KeyC":       {"copy"},
	"Meta+KeyX":       {"cut"},
	"Meta+KeyZ":       {"undo"},

	"Shift+Meta+ArrowUp":    {"moveToBeginningOfDocumentAndModifySelection"},
	"Shift+Meta+ArrowDown":  {"moveToEndOfDocumentAndModifySelection"},
	"Shift+Meta+ArrowLeft":  {"moveToLeftEndOfLineAndModifySelection"},
	"Shift+Meta+ArrowRight": {"moveToRightEndOfLineAndModifySelection"},
	"Shift+Meta+KeyZ":       {"redo"},
}

// editingCommands returns the mac editing selectors for a key chord, or nil
// on other platforms (their renderers resolve the chords natively).
func editingCommands(goos, code string, modifiers int) []string {
	if goos != "darwin" {
		return nil
	}
	var parts []string
	if modifiers&8 != 0 {
		parts = append(parts, "Shift")
	}
	if modifiers&2 != 0 {
		parts = append(parts, "Control")
	}
	if modifiers&1 != 0 {
		parts = append(parts, "Alt")
	}
	if modifiers&4 != 0 {
		parts = append(parts, "Meta")
	}
	parts = append(parts, code)
	return macEditingCommands[strings.Join(parts, "+")]
}

// keyEventParams builds the Input.dispatchKeyEvent params for ev. A "down"
// with text becomes keyDown (Chrome inserts the text itself); without text it
// is rawKeyDown. Editing commands ride only on the down event.
func keyEventParams(goos string, ev KeyEvent) map[string]any {
	var typ string
	switch ev.Kind {
	case "down":
		if ev.Text != "" {
			typ = "keyDown"
		} else {
			typ = "rawKeyDown"
		}
	case "up":
		typ = "keyUp"
	case "char":
		typ = "char"
	default:
		typ = ev.Kind
	}
	params := map[string]any{
		"type": typ, "key": ev.Key, "code": ev.Code, "text": ev.Text, "modifiers": ev.Modifiers,
	}
	if ev.Text != "" {
		params["unmodifiedText"] = ev.Text
	}
	if vk := virtualKeyCode(ev.Code); vk != 0 {
		params["windowsVirtualKeyCode"] = vk
		params["nativeVirtualKeyCode"] = vk
	}
	if ev.AutoRepeat {
		params["autoRepeat"] = true
	}
	if ev.Kind == "down" {
		if cmds := editingCommands(goos, ev.Code, ev.Modifiers); len(cmds) > 0 {
			params["commands"] = cmds
		}
	}
	return params
}
