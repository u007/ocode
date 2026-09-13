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

// windowsKeyCombo parses an xdotool-style combo (e.g. "ctrl+s")
// into a list of virtual-key codes, checking that exactly one
// non-modifier is present.
func windowsKeyCombo(combo string) ([]int, error) {
	parts := splitCombo(combo)
	var codes []int
	nonModifiers := 0
	for _, p := range parts {
		code, isMod, ok := windowsVirtualKey(p)
		if !ok {
			return nil, fmt.Errorf("computer key: unknown key %q", p)
		}
		if !isMod {
			nonModifiers++
		}
		codes = append(codes, code)
	}
	if nonModifiers != 1 {
		return nil, fmt.Errorf("computer key: exactly one non-modifier required in %q", combo)
	}
	return codes, nil
}

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
