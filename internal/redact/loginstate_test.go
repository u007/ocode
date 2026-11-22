package redact

import (
	"runtime"
	"testing"
)

// TestIsSensitiveFileLoginStates: a path inside browse/login-states is sensitive
// as auth.json is, matched by path segments. A sibling that only shares the
// prefix is not, and the case fold follows the platform.
func TestIsSensitiveFileLoginStates(t *testing.T) {
	fold := runtime.GOOS == "darwin" || runtime.GOOS == "windows"
	cases := []struct {
		path  string
		want  bool
		label string
	}{
		{"browse/login-states/x.json", true, "relative spelling"},
		{"/home/u/.local/share/opencode/browse/login-states/x.json", true, "absolute state file"},
		{"/home/u/.local/share/opencode/browse/login-states", true, "the directory itself"},
		{"$HOME/.local/share/opencode/browse/login-states/x.json", true, "unexpanded home spelling"},
		{"/home/u/.local/share/opencode/browse/login-states-old/x.json", false, "sibling with a longer segment"},
		{"/home/u/.local/share/opencode/browse/login-states.bak", false, "dotted sibling"},
		{"login-states/x.json", false, "missing the browse segment"},
		{"other/browse/login-statesX/x", false, "longer name"},
		{"/home/u/.local/share/opencode/browse/other/x.json", false, "other browse subdirectory"},
		{"/home/u/main.go", false, "unrelated file"},
		{"Browse/Login-States/x.json", fold, "mixed case follows the platform"},
	}
	for _, c := range cases {
		if got := IsSensitiveFile(c.path); got != c.want {
			t.Errorf("IsSensitiveFile(%q) = %v, want %v (%s)", c.path, got, c.want, c.label)
		}
	}
}

// TestLoginStateSegmentsCaseFoldCore pins the segment matcher for both values of
// fold, so the case rule is checked on every platform.
func TestLoginStateSegmentsCaseFoldCore(t *testing.T) {
	cases := []struct {
		path string
		fold bool
		want bool
	}{
		{"browse/login-states/x.json", false, true},
		{"Browse/Login-States/x.json", false, false},
		{"Browse/Login-States/x.json", true, true},
		{"BROWSE/LOGIN-STATES", true, true},
		{"Browse/Login-States-old/x.json", true, false},
	}
	for _, c := range cases {
		if got := isLoginStateSegments(c.path, c.fold); got != c.want {
			t.Errorf("isLoginStateSegments(%q, fold=%v) = %v, want %v", c.path, c.fold, got, c.want)
		}
	}
}
