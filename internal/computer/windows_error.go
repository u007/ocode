package computer

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/u007/ocode/internal/tool"
)

// windowsError wraps runner errors for the Windows driver.
// exec.ErrNotFound (powershell missing) becomes a NoticedError
// pointing the user to install PowerShell; other errors pass through.
func windowsError(err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return &tool.NoticedError{
			Err:    err,
			Notice: "PowerShell not found on PATH",
		}
	}
	return err
}

// windowsKeyCombo parses an xdotool-style combo (e.g. "ctrl+s") into
// virtual-key codes: the modifiers in the order given, then the single
// main key last. The script presses them in that order and releases in
// reverse, so a combo with no main key or more than one is an error.
func windowsKeyCombo(combo string) ([]int, error) {
	parts := splitCombo(combo)
	var modifiers []int
	mainKey := 0
	mainCount := 0
	for _, p := range parts {
		code, isMod, ok := windowsVirtualKey(p)
		if !ok {
			return nil, fmt.Errorf("computer key: unknown key %q", p)
		}
		if isMod {
			modifiers = append(modifiers, code)
			continue
		}
		mainKey = code
		mainCount++
	}
	if mainCount != 1 {
		return nil, fmt.Errorf("computer key: exactly one non-modifier required in %q", combo)
	}
	return append(modifiers, mainKey), nil
}

// splitCombo splits an xdotool-style combo on "+" into its key names.
func splitCombo(combo string) []string {
	var parts []string
	current := ""
	for _, c := range combo {
		if c == '+' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
