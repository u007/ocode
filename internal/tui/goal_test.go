package tui

import (
	"strings"
	"testing"
)

func TestGoalCommandRegistered(t *testing.T) {
	found := false
	for _, spec := range commandSpecs {
		if spec.name == "/goal" {
			found = true
			if spec.handler == nil {
				t.Error("/goal has no handler")
			}
			break
		}
	}
	if !found {
		t.Error("/goal not found in commandSpecs")
	}
}

func TestGoalGoalExtraction(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"add", "user", "validation"}, "add user validation"},
		{[]string{"fix", "nil", "panic", "in", "auth"}, "fix nil panic in auth"},
		{[]string{}, ""},
	}
	for _, c := range cases {
		got := strings.Join(c.args, " ")
		if got != c.want {
			t.Errorf("args %v → %q, want %q", c.args, got, c.want)
		}
	}
}