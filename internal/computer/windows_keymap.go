package computer

// windowsVirtualKeyMap maps xdotool-style key names to Windows
// virtual-key codes.
var windowsVirtualKeyMap = map[string]int{
	// modifiers
	"ctrl":  0x11,
	"alt":   0x12,
	"shift": 0x10,
	"super": 0x5B,
	"cmd":   0x5B,
	// return / enter
	"Return": 0x0D,
	"Enter":  0x0D,
	// navigation
	"Tab":       0x09,
	"Escape":    0x1B,
	"space":     0x20,
	"BackSpace": 0x08,
	"Delete":    0x2E,
	"Home":      0x24,
	"End":       0x23,
	"Page_Up":   0x21,
	"Page_Down": 0x22,
	"Left":      0x25,
	"Up":        0x26,
	"Right":     0x27,
	"Down":      0x28,
	// function keys
	"F1": 0x70, "F2": 0x71, "F3": 0x72, "F4": 0x73,
	"F5": 0x74, "F6": 0x75, "F7": 0x76, "F8": 0x77,
	"F9": 0x78, "F10": 0x79, "F11": 0x7A, "F12": 0x7B,
}

// windowsModifiers is the set of key names that are held down around the
// main key of a combo.
var windowsModifiers = map[string]bool{
	"ctrl":  true,
	"alt":   true,
	"shift": true,
	"super": true,
	"cmd":   true,
}

// windowsVirtualKey looks up a key name in windowsVirtualKeyMap.
// Returns (vk, isModifier, ok). Unknown names return ok=false.
func windowsVirtualKey(name string) (vk int, isModifier bool, ok bool) {
	vk, ok = windowsVirtualKeyMap[name]
	if !ok {
		// Letters and digits are not in the table: their virtual-key code
		// is the ASCII uppercase value.
		if len(name) == 1 {
			c := name[0]
			if c >= 'a' && c <= 'z' {
				return int(c - 'a' + 'A'), false, true
			}
			if c >= 'A' && c <= 'Z' {
				return int(c), false, true
			}
			if c >= '0' && c <= '9' {
				return int(c), false, true
			}
		}
		return 0, false, false
	}
	return vk, windowsModifiers[name], true
}
