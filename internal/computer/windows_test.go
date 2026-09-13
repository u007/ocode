package computer

import (
	"encoding/base64"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

const winScript = "C:\\tmp\\script.ps1"

func TestWindowsArgs_Order(t *testing.T) {
	args := windowsArgs(winScript, "move", "10", "20")
	want := []string{
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", winScript, "move", "10", "20",
	}
	if len(args) != len(want) {
		t.Fatalf("expected %d args got %d: %v", len(want), len(args), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q want %q", i, args[i], want[i])
		}
	}
}

func TestWindowsTypeArgs_Base64(t *testing.T) {
	const text = "héllo \"wörld\" & 😀"
	args := windowsTypeArgs(winScript, text)
	if len(args) != 8 {
		t.Fatalf("expected 8 args got %d: %v", len(args), args)
	}
	if args[6] != "type" {
		t.Fatalf("args[6] = %q want %q", args[6], "type")
	}
	want := base64.StdEncoding.EncodeToString([]byte(text))
	if args[7] != want {
		t.Fatalf("args[7] = %q want %q", args[7], want)
	}
	if strings.Contains(strings.Join(args, " "), text) {
		t.Fatalf("raw text must not appear in argv: %v", args)
	}
}

func TestWindowsKeyArgs_CtrlS(t *testing.T) {
	args, err := windowsKeyArgs(winScript, "ctrl+s")
	if err != nil {
		t.Fatalf("windowsKeyArgs error: %v", err)
	}
	got := strings.Join(args[6:], " ")
	if got != "key 17 83" {
		t.Fatalf("key argv = %q want %q", got, "key 17 83")
	}
}

func TestWindowsKeyArgs_ModifiersFirst(t *testing.T) {
	args, err := windowsKeyArgs(winScript, "s+ctrl+shift")
	if err != nil {
		t.Fatalf("windowsKeyArgs error: %v", err)
	}
	got := strings.Join(args[6:], " ")
	if got != "key 17 16 83" {
		t.Fatalf("key argv = %q want %q", got, "key 17 16 83")
	}
}

func TestWindowsKeyArgs_RejectsTwoMainKeys(t *testing.T) {
	if _, err := windowsKeyArgs(winScript, "a+b"); err == nil {
		t.Fatal("expected error for two non-modifier keys")
	}
}

func TestWindowsKeyArgs_RejectsModifiersOnly(t *testing.T) {
	if _, err := windowsKeyArgs(winScript, "ctrl+shift"); err == nil {
		t.Fatal("expected error for a combo with no main key")
	}
}

func TestWindowsKeyArgs_RejectsUnknownKey(t *testing.T) {
	if _, err := windowsKeyArgs(winScript, "ctrl+Bogus"); err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestWindowsVirtualKey_KnownAndUnknown(t *testing.T) {
	cases := map[string]int{
		"ctrl":      0x11,
		"alt":       0x12,
		"shift":     0x10,
		"super":     0x5B,
		"cmd":       0x5B,
		"Return":    0x0D,
		"Tab":       0x09,
		"Escape":    0x1B,
		"space":     0x20,
		"BackSpace": 0x08,
		"Delete":    0x2E,
		"Left":      0x25,
		"Up":        0x26,
		"Right":     0x27,
		"Down":      0x28,
		"Home":      0x24,
		"End":       0x23,
		"Page_Up":   0x21,
		"Page_Down": 0x22,
		"F1":        0x70,
		"F12":       0x7B,
		"a":         0x41,
		"z":         0x5A,
		"0":         0x30,
		"9":         0x39,
	}
	for name, want := range cases {
		vk, _, ok := windowsVirtualKey(name)
		if !ok {
			t.Fatalf("%s: expected ok", name)
		}
		if vk != want {
			t.Fatalf("%s: vk = 0x%02X want 0x%02X", name, vk, want)
		}
	}
	if _, _, ok := windowsVirtualKey("Bogus"); ok {
		t.Fatal("Bogus: expected ok=false")
	}
}

func TestWindowsVirtualKey_ModifierFlag(t *testing.T) {
	for _, name := range []string{"ctrl", "alt", "shift", "super", "cmd"} {
		if _, isMod, _ := windowsVirtualKey(name); !isMod {
			t.Fatalf("%s: expected isModifier=true", name)
		}
	}
	for _, name := range []string{"Return", "a", "F1", "Left"} {
		if _, isMod, _ := windowsVirtualKey(name); isMod {
			t.Fatalf("%s: expected isModifier=false", name)
		}
	}
}

func TestWindowsMissingPowershellIsNoticed(t *testing.T) {
	err := windowsError(exec.ErrNotFound)
	var ne *tool.NoticedError
	if !errors.As(err, &ne) {
		t.Fatalf("expected *tool.NoticedError, got %T: %v", err, err)
	}
	if ne.Notice != "PowerShell not found on PATH" {
		t.Fatalf("notice = %q", ne.Notice)
	}
}

// TestWindowsScript_ParamIsFirstStatement guards the PowerShell rule that
// param() must precede every other statement; only comments may come before.
func TestWindowsScript_ParamIsFirstStatement(t *testing.T) {
	for _, line := range strings.Split(string(windowsScript), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(trimmed, "param(") {
			t.Fatalf("first non-comment line is %q, want a param( declaration", trimmed)
		}
		return
	}
	t.Fatal("script has no statements")
}

func TestWindowsScript_RequiredCalls(t *testing.T) {
	script := string(windowsScript)
	for _, want := range []string{
		"[OcodeInput]::SetProcessDPIAware()",
		"MOUSEEVENTF_WHEEL",
		"MOUSEEVENTF_HWHEEL",
		"KEYEVENTF_UNICODE",
		"$ErrorActionPreference = 'Stop'",
		"exit 1",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script is missing %q", want)
		}
	}
	// Buttons and the wheel act on the cursor position set by SetCursorPos,
	// so no input event may carry a relative MOUSEEVENTF_MOVE.
	if strings.Contains(script, "MOUSEEVENTF_MOVE") {
		t.Fatal("script must not use MOUSEEVENTF_MOVE; it moves relatively")
	}
}
