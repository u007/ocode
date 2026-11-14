package commands

import "testing"

func TestPromptHasArgs(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   bool
	}{
		{"double brace", "review {{args}} now", true},
		{"single brace", "review {args} now", true},
		{"claude code", "review $ARGUMENTS now", true},
		{"claude code twice", "$ARGUMENTS and $ARGUMENTS", true},
		{"no placeholder", "review the diff", false},
		{"empty", "", false},
		{"dollar without name", "costs $5", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PromptHasArgs(tc.prompt); got != tc.want {
				t.Errorf("PromptHasArgs(%q) = %v, want %v", tc.prompt, got, tc.want)
			}
		})
	}
}

func TestExpandArgs(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		args   string
		want   string
	}{
		{"double brace", "a {{args}} b", "X", "a X b"},
		{"single brace", "a {args} b", "X", "a X b"},
		{"claude code", "a $ARGUMENTS b", "X", "a X b"},
		{"claude code twice", "$ARGUMENTS/$ARGUMENTS", "X", "X/X"},
		{"mixed placeholders", "{{args}} {args} $ARGUMENTS", "X", "X X X"},
		{"no placeholder left unchanged", "review the diff", "X", "review the diff"},
		{"empty args", "a $ARGUMENTS b", "", "a  b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExpandArgs(tc.prompt, tc.args); got != tc.want {
				t.Errorf("ExpandArgs(%q, %q) = %q, want %q", tc.prompt, tc.args, got, tc.want)
			}
		})
	}
}
