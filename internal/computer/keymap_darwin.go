package computer

// darwinKeyMap maps xdotool-style key names to macOS virtual keycodes.
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
	"F1": 122, "F2": 123, "F3": 124, "F4": 125, "F5": 126,
	"F6": 127, "F7": 128, "F8": 129, "F9": 130, "F10": 131,
	"F11": 132, "F12": 133,
	// letter keys (resolved at runtime via unicode)
	"a": 0, "b": 0, "c": 0, "d": 0, "e": 0, "f": 0, "g": 0,
	"h": 0, "i": 0, "j": 0, "k": 0, "l": 0, "m": 0, "n": 0,
	"o": 0, "p": 0, "q": 0, "r": 0, "s": 0, "t": 0, "u": 0,
	"v": 0, "w": 0, "x": 0, "y": 0, "z": 0,
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
