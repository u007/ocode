//go:build !windows

package server

import (
	"reflect"
	"testing"
)

func TestTerminalEnvPinsTermAndStripsGhostty(t *testing.T) {
	tests := []struct {
		name string
		base []string
		want []string
	}{
		{
			name: "empty base still pins TERM and COLORTERM",
			base: nil,
			want: []string{"TERM=xterm-256color", "COLORTERM=truecolor"},
		},
		{
			name: "ghostty variables are dropped and unrelated ones keep their order",
			base: []string{
				"HOME=/home/u",
				"TERM=xterm-ghostty",
				"GHOSTTY_RESOURCES_DIR=/Applications/Ghostty.app/Contents/Resources/ghostty",
				"PATH=/usr/bin",
				"TERM_PROGRAM=ghostty",
				"TERM_PROGRAM_VERSION=1.2.0",
				"COLORTERM=foo",
				"GHOSTTY_BIN_DIR=/x",
				"LANG=en_US.UTF-8",
			},
			want: []string{"HOME=/home/u", "PATH=/usr/bin", "LANG=en_US.UTF-8", "TERM=xterm-256color", "COLORTERM=truecolor"},
		},
		{
			name: "similarly named variables are kept",
			base: []string{"TERMINFO=/x", "MY_TERM=vt100", "GHOSTTYX=1", "TERM_SESSION_ID=abc"},
			want: []string{"TERMINFO=/x", "MY_TERM=vt100", "GHOSTTYX=1", "TERM_SESSION_ID=abc", "TERM=xterm-256color", "COLORTERM=truecolor"},
		},
		{
			name: "duplicate TERM entries collapse to exactly one",
			base: []string{"TERM=a", "X=1", "TERM=b"},
			want: []string{"X=1", "TERM=xterm-256color", "COLORTERM=truecolor"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := append([]string(nil), tc.base...)
			got := terminalEnv(in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("terminalEnv(%q) = %q, want %q", tc.base, got, tc.want)
			}
			if !reflect.DeepEqual(in, tc.base) && !(len(in) == 0 && len(tc.base) == 0) {
				t.Fatalf("terminalEnv mutated its input: %q != %q", in, tc.base)
			}
		})
	}
}
