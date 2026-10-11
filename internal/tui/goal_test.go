package tui

import (
	"strings"
	"testing"
)

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
