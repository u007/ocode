package computer

// darwinKeyMap maps xdotool-style key names to macOS virtual keycodes
// (ANSI layout, from HIToolbox/Events.h).
var darwinKeyMap = map[string]int{
	// modifiers
	"ctrl": 59, "cmd": 55, "alt": 58, "shift": 56, "super": 55,
	// return / enter
	"Return": 36, "Enter": 36,
	// navigation
	"Tab": 48, "Escape": 53, "space": 49,
	"BackSpace": 51, "Delete": 117,
	"Home": 115, "End": 119, "Page_Up": 116, "Page_Down": 121,
	"Up": 126, "Down": 125, "Left": 123, "Right": 124,
	// function keys
	"F1": 122, "F2": 120, "F3": 99, "F4": 118, "F5": 96, "F6": 97,
	"F7": 98, "F8": 100, "F9": 101, "F10": 109, "F11": 103, "F12": 111,
	// letters
	"a": 0, "s": 1, "d": 2, "f": 3, "h": 4, "g": 5, "z": 6, "x": 7, "c": 8, "v": 9,
	"b": 11, "q": 12, "w": 13, "e": 14, "r": 15, "y": 16, "t": 17,
	"o": 31, "u": 32, "i": 34, "p": 35, "l": 37, "j": 38, "k": 40, "n": 45, "m": 46,
	// digits
	"1": 18, "2": 19, "3": 20, "4": 21, "6": 22, "5": 23, "9": 25, "7": 26, "8": 28, "0": 29,
	// punctuation
	"equal": 24, "minus": 27, "bracketright": 30, "bracketleft": 33, "apostrophe": 39,
	"semicolon": 41, "backslash": 42, "comma": 43, "slash": 44, "period": 47, "grave": 50,
}

// darwinKeyCode looks up a key name in darwinKeyMap.
// Returns (code, isModifier, ok). isModifier is true for ctrl/cmd/alt/shift/super.
// Unknown names return ok=false.
func darwinKeyCode(name string) (code int, isModifier bool, ok bool) {
	code, ok = darwinKeyMap[name]
	if !ok {
		return 0, false, false
	}
	isModifier = name == "ctrl" || name == "cmd" || name == "alt" || name == "shift" || name == "super"
	return code, isModifier, true
}
