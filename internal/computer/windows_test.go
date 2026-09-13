package computer

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

func TestWindowsArgs_Type(t *testing.T) {
	args := windowsArgs("C:\\tmp\\script.ps1", "type", "hello")
	if len(args) != 8 {
		t.Fatalf("expected 8 args got %d", len(args))
	}
	if args[0] != "-NoProfile" {
		t.Fatalf("args[0] = %q", args[0])
	}
	if args[1] != "-NonInteractive" {
		t.Fatalf("args[1] = %q", args[1])
	}
	if args[2] != "-ExecutionPolicy" {
		t.Fatalf("args[2] = %q", args[2])
	}
	if args[3] != "Bypass" {
		t.Fatalf("args[3] = %q", args[3])
	}
	if args[4] != "-File" {
		t.Fatalf("args[4] = %q", args[4])
	}
	if args[5] != "C:\\tmp\\script.ps1" {
		t.Fatalf("args[5] = %q", args[5])
	}
	if args[6] != "type" {
		t.Fatalf("args[6] = %q", args[6])
	}
}

func TestWindowsVirtualKey_KnownAndUnknown(t *testing.T) {
	vk, _, ok := windowsVirtualKey("ctrl")
	if vk != 0x11 || !ok {
		t.Fatalf("ctrl: expected (0x11, _, true) got (%d, _, %v)", vk, ok)
	}
	vk, _, ok = windowsVirtualKey("Return")
	if vk != 0x0D || !ok {
		t.Fatalf("Return: expected (0x0D, _, true) got (%d, _, %v)", vk, ok)
	}
	vk, _, ok = windowsVirtualKey("F1")
	if vk != 0x70 || !ok {
		t.Fatalf("F1: expected (0x70, _, true) got (%d, _, %v)", vk, ok)
	}
	vk, _, ok = windowsVirtualKey("a")
	if vk != 0x41 || !ok {
		t.Fatalf("a: expected (0x41, _, true) got (%d, _, %v)", vk, ok)
	}
	vk, _, ok = windowsVirtualKey("0")
	if vk != 0x30 || !ok {
		t.Fatalf("0: expected (0x30, _, true) got (%d, _, %v)", vk, ok)
	}
	_, _, ok = windowsVirtualKey("Bogus")
	if ok {
		t.Fatalf("Bogus: expected (_, _, false) got (_, _, %v)", ok)
	}
}

func TestWindowsVirtualKey_ModifierFlag(t *testing.T) {
	_, isMod, _ := windowsVirtualKey("ctrl")
	if !isMod {
		t.Fatalf("ctrl: expected isModifier=true")
	}
	_, isMod, _ = windowsVirtualKey("alt")
	if !isMod {
		t.Fatalf("alt: expected isModifier=true")
	}
	_, isMod, _ = windowsVirtualKey("shift")
	if !isMod {
		t.Fatalf("shift: expected isModifier=true")
	}
	_, isMod, _ = windowsVirtualKey("super")
	if !isMod {
		t.Fatalf("super: expected isModifier=true")
	}
	_, isMod, _ = windowsVirtualKey("cmd")
	if !isMod {
		t.Fatalf("cmd: expected isModifier=true")
	}
	_, isMod, _ = windowsVirtualKey("Return")
	if isMod {
		t.Fatalf("Return: expected isModifier=false")
	}
	_, isMod, _ = windowsVirtualKey("a")
	if isMod {
		t.Fatalf("a: expected isModifier=false")
	}
}

func TestWindowsKeyCombo_Order(t *testing.T) {
	codes, err := windowsKeyCombo("ctrl+s")
	if err != nil {
		t.Fatalf("windowsKeyCombo error: %v", err)
	}
	if len(codes) != 2 || codes[0] != 0x11 || codes[1] != 0x53 {
		t.Fatalf("expected [17, 83] got %v", codes)
	}
}

func TestWindowsKeyCombo_RejectsTwoMainKeys(t *testing.T) {
	_, err := windowsKeyCombo("a+b")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWindowsMissingPowershellIsNoticed(t *testing.T) {
	err := windowsError(exec.ErrNotFound)
	if err == nil {
		t.Fatal("expected error")
	}
	var ne *tool.NoticedError
	if !errors.As(err, &ne) {
		t.Fatalf("expected *tool.NoticedError, got %T: %v", err, err)
	}
	if ne.Notice != "PowerShell not found on PATH" {
		t.Fatalf("expected 'PowerShell not found on PATH' notice, got %q", ne.Notice)
	}
}
