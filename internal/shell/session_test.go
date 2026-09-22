package shell

import (
	"strings"
	"testing"
)

// The framing helpers are the part of a persistent shell that must be right on
// every platform and can be tested without a pty: the pty integration tests
// skip on a host that denies /dev/ptmx, so these carry the logic coverage.

func TestFrameCommandHeredocEvalUnit(t *testing.T) {
	const delim = "__OCODE_CMD_deadbeef__"
	got, err := frameCommand("echo hi", delim)
	if err != nil {
		t.Fatalf("frameCommand: %v", err)
	}
	want := "eval \"$(cat <<'" + delim + "'\n" +
		"echo hi\n" +
		delim + "\n" +
		")\"\n"
	if got != want {
		t.Errorf("frameCommand = %q, want %q", got, want)
	}
}

func TestFrameCommandPreservesMultiLineAndDoesNotDoubleNewline(t *testing.T) {
	const delim = "__OCODE_CMD_x__"
	cases := map[string]string{
		"two lines":       "echo a\necho b",
		"trailing newlf":  "echo a\necho b\n",
		"inner heredoc":   "cat <<EOF\nbody\nEOF",
		"continues":       "echo a &&\necho b",
		"single quote in": "echo 'it\\'s'",
	}
	for name, cmd := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := frameCommand(cmd, delim)
			if err != nil {
				t.Fatalf("frameCommand(%q): %v", cmd, err)
			}
			// The command body must appear verbatim, and the delimiter must
			// appear exactly twice (open + close).
			if !strings.Contains(got, cmd) {
				t.Errorf("framed unit lost the command body:\n%s", got)
			}
			if n := strings.Count(got, delim); n != 2 {
				t.Errorf("delimiter appears %d times, want 2:\n%s", n, got)
			}
			if strings.Contains(got, "\n\n__OCODE") {
				t.Errorf("unexpected blank line before the closing delimiter:\n%q", got)
			}
		})
	}
}

func TestFrameCommandRejectsDelimiterLine(t *testing.T) {
	const delim = "__OCODE_CMD_x__"
	// A command that contains the delimiter as a whole line would terminate
	// the heredoc early and let the remainder run as separate commands.
	_, err := frameCommand("echo before\n"+delim+"\necho after", delim)
	if err == nil {
		t.Fatal("expected an error for a command containing the delimiter line")
	}
	// Substring (not whole-line) containment is fine.
	if _, err := frameCommand("echo "+delim, delim); err != nil {
		t.Errorf("substring containment must be allowed, got %v", err)
	}
}

func TestFindMarkerParsesStatusAndCwd(t *testing.T) {
	const marker = "__OCODE_DONE_ab12__"
	cases := []struct {
		name       string
		raw        string
		wantStatus int
		wantCwd    string
		wantOut    string
	}{
		{
			name:       "simple",
			raw:        "hello\n\n" + marker + " 0 /tmp\n",
			wantStatus: 0,
			wantCwd:    "/tmp",
			wantOut:    "hello",
		},
		{
			name:       "non-zero",
			raw:        "boom\n\n" + marker + " 1 /var/log\n",
			wantStatus: 1,
			wantCwd:    "/var/log",
			wantOut:    "boom",
		},
		{
			name:       "cwd with spaces",
			raw:        marker + " 0 /tmp/a b c\n",
			wantStatus: 0,
			wantCwd:    "/tmp/a b c",
			wantOut:    "",
		},
		{
			name:       "no trailing newline on output",
			raw:        "partial" + marker + " 0 /tmp\n",
			wantStatus: 0,
			wantCwd:    "/tmp",
			wantOut:    "partial",
		},
		{
			name:       "crlf",
			raw:        "hello\r\n\r\n" + marker + " 2 /tmp\r\n",
			wantStatus: 2,
			wantCwd:    "/tmp",
			wantOut:    "hello",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, status, cwd, ok := findMarker(tc.raw, marker)
			if !ok {
				t.Fatalf("findMarker did not find %q in %q", marker, tc.raw)
			}
			if status != tc.wantStatus {
				t.Errorf("status = %d, want %d", status, tc.wantStatus)
			}
			if cwd != tc.wantCwd {
				t.Errorf("cwd = %q, want %q", cwd, tc.wantCwd)
			}
			if got := cleanOutput(tc.raw[:start], ""); got != tc.wantOut {
				t.Errorf("output = %q, want %q", got, tc.wantOut)
			}
		})
	}
}

func TestFindMarkerAbsent(t *testing.T) {
	if _, _, _, ok := findMarker("just output\nno marker here\n", "__OCODE_DONE_nope__"); ok {
		t.Fatal("findMarker reported a marker that is not present")
	}
}

func TestFindMarkerIgnoresMarkerLookingPrefixWithoutStatus(t *testing.T) {
	// A line that merely starts with the marker but has no numeric status
	// field is not a boundary.
	const marker = "__OCODE_DONE_ab__"
	raw := marker + "banana\n" + marker + " 0 /tmp\n"
	_, status, cwd, ok := findMarker(raw, marker)
	if !ok || status != 0 || cwd != "/tmp" {
		t.Fatalf("findMarker = ok=%v status=%d cwd=%q; want the second line", ok, status, cwd)
	}
}

func TestCleanOutputStripsANSIAndEcho(t *testing.T) {
	framed := "eval \"$(cat <<'__D__'\nls\n__D__\n)\"\n"
	raw := framed + "\x1b[31mred\x1b[0m\n"
	if got, want := cleanOutput(raw, framed), "red"; got != want {
		t.Errorf("cleanOutput = %q, want %q", got, want)
	}
}

func TestStripANSI(t *testing.T) {
	cases := map[string]string{
		"\x1b[31mred\x1b[0m":       "red",
		"a\x1b[1;32mb\x1b[0mc":     "abc",
		"plain":                    "plain",
		"\x1b]0;title\x07text":     "text",
		"\x1b[?25lhidden\x1b[?25h": "hidden",
	}
	for in, want := range cases {
		if got := stripANSI(in); got != want {
			t.Errorf("stripANSI(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeNewlines(t *testing.T) {
	if got, want := normalizeNewlines("a\r\nb\rc\nd"), "a\nb\nc\nd"; got != want {
		t.Errorf("normalizeNewlines = %q, want %q", got, want)
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"/tmp/x":    "'/tmp/x'",
		"/tmp/a b":  "'/tmp/a b'",
		"/tmp/it's": `'/tmp/it'\''s'`,
		"":          "''",
	}
	for in, want := range cases {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %q, want %q", in, got, want)
		}
	}
}
