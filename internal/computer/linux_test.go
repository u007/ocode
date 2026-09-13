package computer

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// TestLinuxBackend_Detection verifies backend detection from
// XDG_SESSION_TYPE: wayland → "wayland", else → "x11".
func TestLinuxBackend_Detection(t *testing.T) {
	tests := []struct {
		envValue string
		want     string
	}{
		{"wayland", "wayland"},
		{"x11", "x11"},
		{"", "x11"},
		{"something_else", "x11"},
	}
	for _, tc := range tests {
		got := linuxBackend(func(key string) string {
			if key == "XDG_SESSION_TYPE" {
				return tc.envValue
			}
			return ""
		})
		if got != tc.want {
			t.Errorf("XDG_SESSION_TYPE=%q: got %q want %q", tc.envValue, got, tc.want)
		}
	}
}

// TestXdotoolArgs_ClickAndScroll verifies xdotoolArgs produces
// the expected argv for click and scroll operations.
func TestXdotoolArgs_ClickAndScroll(t *testing.T) {
	// click with multiple args
	args := xdotoolArgs("click", "100", "200", "click", "--repeat", "3", "1")
	if len(args) != 8 {
		t.Fatalf("click: expected 8 args got %d", len(args))
	}
	if args[0] != "xdotool" {
		t.Fatalf("args[0] = %q", args[0])
	}
	if args[1] != "click" {
		t.Fatalf("args[1] = %q", args[1])
	}

	// scroll: xdotool click button
	args = xdotoolArgs("click", "4")
	if len(args) != 3 {
		t.Fatalf("scroll: expected 3 args got %d", len(args))
	}
	if args[0] != "xdotool" || args[1] != "click" || args[2] != "4" {
		t.Fatalf("scroll: got %v", args)
	}
}

// TestYdotoolArgs_ClickCodes verifies ydotoolArgs produces
// click commands with the correct Linux input event codes:
// left=0xC0, right=0xC1, middle=0xC2.
func TestYdotoolArgs_ClickCodes(t *testing.T) {
	for _, tc := range []struct {
		code string
		name string
	}{
		{"0xC0", "left"},
		{"0xC1", "right"},
		{"0xC2", "middle"},
	} {
		args := ydotoolArgs("click", tc.code)
		if len(args) != 3 {
			t.Fatalf("%s: expected 3 args got %d", tc.name, len(args))
		}
		if args[0] != "ydotool" {
			t.Fatalf("%s: args[0] = %q", tc.name, args[0])
		}
		if args[1] != "click" {
			t.Fatalf("%s: args[1] = %q", tc.name, args[1])
		}
		if args[2] != tc.code {
			t.Fatalf("%s: click code: got %q want %q", tc.name, args[2], tc.code)
		}
	}
}

// TestLinuxMissingBinaryIsNoticed verifies that when a binary
// is missing (exec.ErrNotFound), the error is a NoticedError
// containing the apt install hint for both backends.
func TestLinuxMissingBinaryIsNoticed(t *testing.T) {
	// X11 backend hint should mention xdotool and scrot
	hint := linuxInstallHint("x11")
	if !strings.Contains(hint, "xdotool") {
		t.Errorf("x11 hint missing xdotool: %q", hint)
	}
	if !strings.Contains(hint, "scrot") {
		t.Errorf("x11 hint missing scrot: %q", hint)
	}

	// Wayland backend hint should mention ydotool and grim
	hint = linuxInstallHint("wayland")
	if !strings.Contains(hint, "ydotool") {
		t.Errorf("wayland hint missing ydotool: %q", hint)
	}
	if !strings.Contains(hint, "grim") {
		t.Errorf("wayland hint missing grim: %q", hint)
	}

	// Verify that exec.ErrNotFound maps to NoticedError with hint
	err := linuxError(exec.ErrNotFound, "x11")
	noticed, ok := err.(*tool.NoticedError)
	if !ok {
		t.Fatalf("expected *tool.NoticedError got %T: %v", err, err)
	}
	if !errors.Is(noticed.Err, exec.ErrNotFound) {
		t.Errorf("expected Err to wrap exec.ErrNotFound")
	}
	if !strings.Contains(noticed.Notice, "apt install") {
		t.Errorf("notice missing apt install hint: %q", noticed.Notice)
	}
}

// TestYdotoolKeyCombo verifies that ctrl+c produces the
// expected code:press/code:release pairs: 29:1 46:1 46:0 29:0.
func TestYdotoolKeyCombo(t *testing.T) {
	codes, err := ydotoolKeyCombo("ctrl+c")
	if err != nil {
		t.Fatalf("ydotoolKeyCombo error: %v", err)
	}
	got := strings.Join(codes, " ")
	want := "29:1 46:1 46:0 29:0"
	if got != want {
		t.Errorf("ctrl+c: got %q want %q", got, want)
	}
}
