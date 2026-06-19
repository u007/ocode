package tui

import (
	"strings"
	"testing"
)

func TestOrchestrateCommandRegistered(t *testing.T) {
	found := false
	for _, spec := range commandSpecs {
		if spec.name == "/orchestrate" {
			found = true
			if spec.handler == nil {
				t.Error("/orchestrate has no handler")
			}
			break
		}
	}
	if !found {
		t.Error("/orchestrate not found in commandSpecs")
	}
}

func TestOrchestrateGoalExtraction(t *testing.T) {
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
