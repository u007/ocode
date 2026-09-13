package computer

import (
	"fmt"
	"strconv"
)

// linuxKeyMap maps xdotool-style key names to Linux
// input event codes (linux/input-event-codes.h).
var linuxKeyMap = map[string]int{
	// modifiers
	"ctrl":  29,
	"shift": 42,
	"alt":   56,
	"super": 125,
	"cmd":   125,
	// return / enter
	"Return": 28,
	"Enter":  28,
	// navigation
	"Tab":       15,
	"Escape":    1,
	"space":     57,
	"BackSpace": 14,
	"Delete":    111,
	"Home":      102,
	"End":       107,
	"Page_Up":   104,
	"Page_Down": 109,
	"Up":        103,
	"Down":      108,
	"Left":      105,
	"Right":     106,
	// function keys
	"F1": 59, "F2": 60, "F3": 61, "F4": 62,
	"F5": 63, "F6": 64, "F7": 65, "F8": 66,
	"F9": 67, "F10": 68,
	"F11": 87, "F12": 88,
	// letter keys per linux/input-event-codes.h
	"a": 30, "b": 48, "c": 46, "d": 32, "e": 18,
	"f": 33, "g": 34, "h": 35, "i": 23, "j": 36,
	"k": 37, "l": 38, "m": 50, "n": 49, "o": 24,
	"p": 25, "q": 16, "r": 31, "s": 39, "t": 20,
	"u": 22, "v": 47, "w": 17, "x": 45, "y": 21,
	"z": 44,
	// digit keys
	"0": 2, "1": 3, "2": 4, "3": 5, "4": 6,
	"5": 7, "6": 8, "7": 9, "8": 10, "9": 11,
}

// linuxKeyCode looks up a key name in linuxKeyMap.
// Returns (code, isModifier, ok). isModifier is true for
// ctrl/shift/alt/super/cmd. Unknown names return ok=false.
func linuxKeyCode(name string) (code int, isModifier bool, ok bool) {
	code, ok = linuxKeyMap[name]
	if !ok {
		return 0, false, false
	}
	isModifier = name == "ctrl" || name == "shift" || name == "alt" || name == "super" || name == "cmd"
	return code, isModifier, true
}

// ydotoolKeyCombo parses an xdotool-style combo (e.g. "ctrl+c")
// into code:press/code:release pairs for ydotool:
// "ctrl+c" → ["29:1", "46:1", "46:0", "29:0"]
func ydotoolKeyCombo(combo string) ([]string, error) {
	parts := splitCombo(combo)
	var presses []string
	var releases []string
	nonModifiers := 0
	for _, p := range parts {
		code, isMod, ok := linuxKeyCode(p)
		if !ok {
			return nil, fmt.Errorf("computer key: unknown key %q", p)
		}
		if !isMod {
			nonModifiers++
		}
		presses = append(presses, strconv.Itoa(code)+":1")
		releases = append([]string{strconv.Itoa(code) + ":0"}, releases...)
	}
	if nonModifiers != 1 {
		return nil, fmt.Errorf("computer key: exactly one non-modifier required in %q", combo)
	}
	return append(presses, releases...), nil
}
