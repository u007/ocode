package telegram

import (
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	cases := []struct {
		in      string
		wantCmd string
		wantN   int
	}{
		{"/sessions", "sessions", 0},
		{"/session abc123", "session", 1},
		{"/yolo on now", "yolo", 2},
		{"/help@mybot", "help", 0},
		{"just text", "", 0},
		{"", "", 0},
	}
	for _, c := range cases {
		cmd, args := parseCommand(c.in)
		if cmd != c.wantCmd {
			t.Errorf("parseCommand(%q) cmd = %q, want %q", c.in, cmd, c.wantCmd)
		}
		if len(args) != c.wantN {
			t.Errorf("parseCommand(%q) args = %d, want %d", c.in, len(args), c.wantN)
		}
	}
}

func TestTruncateAndTrim(t *testing.T) {
	long := "abcdefghij"
	if got := truncate(long, 4); got != "abcd…" {
		t.Errorf("truncate = %q", got)
	}
	if got := trimRunes(long, 4); got != "ghij" {
		t.Errorf("trimRunes = %q", got)
	}
	if got := short("abcdef", 6); got != "abcdef" {
		t.Errorf("short = %q", got)
	}
}

// TestShortRuneAware verifies short truncates by rune count, not byte count, so
// multi-byte UTF-8 characters are not split.
func TestShortRuneAware(t *testing.T) {
	// 4 Japanese characters, each 3 bytes in UTF-8.
	in := "日本語テスト"
	if got := short(in, 3); got != "日本語" {
		t.Errorf("short(%q, 3) = %q, want %q", in, got, "日本語")
	}
	if got := short(in, 4); got != "日本語テ" {
		t.Errorf("short(%q, 4) = %q, want %q", in, got, "日本語テ")
	}
	if got := short(in, 6); got != in {
		t.Errorf("short(%q, 6) = %q, want %q", in, got, in)
	}
}

func TestEscapeMarkdownV2(t *testing.T) {
	// All characters here are MarkdownV2 specials, so each must be escaped.
	in := "._*[]()~`>#+-=|{}!"
	out := EscapeMarkdownV2(in)
	for _, ch := range []byte(in) {
		needle := "\\" + string(ch)
		if !contains(out, needle) {
			t.Errorf("EscapeMarkdownV2(%q) missing escape for %q in %q", in, string(ch), out)
		}
	}
	// A normal letter should NOT be escaped.
	if contains(EscapeMarkdownV2("abc"), "\\a") {
		t.Errorf("EscapeMarkdownV2 escaped a non-special char")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestIsSelected(t *testing.T) {
	sel := map[int][]int{0: {1, 3}, 1: {2}}
	if !isSelected(sel, 0, 3) {
		t.Error("expected oi=3 to be selected for qi=0")
	}
	if isSelected(sel, 0, 2) {
		t.Error("expected oi=2 to NOT be selected for qi=0")
	}
	if isSelected(sel, 2, 0) {
		t.Error("expected qi=2 to have no selections")
	}
}

func TestQuestionComplete(t *testing.T) {
	single := questionPending{questions: []questionPromptLite{{}, {}}}
	if questionComplete(single) {
		t.Error("empty selections should be incomplete")
	}
	single.selected = map[int][]int{0: {0}} // only first question answered
	if questionComplete(single) {
		t.Error("one of two questions answered should be incomplete")
	}
	single.selected = map[int][]int{0: {0}, 1: {1}}
	if !questionComplete(single) {
		t.Error("both questions answered should be complete")
	}
}

func TestBuildQuestionKeyboardHasSubmit(t *testing.T) {
	pq := questionPending{
		questions: []questionPromptLite{
			{Header: "H", Question: "Q1", Options: []string{"a", "b"}, Multiple: true},
		},
		selected: map[int][]int{0: {1}},
	}
	rows := buildQuestionKeyboard("k", pq)
	// last row must be the Submit button
	last := rows[len(rows)-1][0]
	if last.Text != "✅ Submit answers" {
		t.Errorf("expected Submit button, got %q", last.Text)
	}
	// an option that is selected must be prefixed with ✓
	foundCheck := false
	for _, row := range rows {
		for _, b := range row {
			if b.Text == "✓ b" {
				foundCheck = true
			}
		}
	}
	if !foundCheck {
		t.Error("expected selected option 'b' to render with ✓ prefix")
	}
}

func TestIsOtherLabel(t *testing.T) {
	yes := []string{"Other", "Something else", "Type your own answer", "Custom value", "Enter a custom name"}
	for _, l := range yes {
		if !isOtherLabel(l) {
			t.Errorf("isOtherLabel(%q) should be true", l)
		}
	}
	no := []string{"Staging", "Production", "Yes", "No"}
	for _, l := range no {
		if isOtherLabel(l) {
			t.Errorf("isOtherLabel(%q) should be false", l)
		}
	}
}

func TestQuestionCompleteWithCustom(t *testing.T) {
	pq := questionPending{
		questions: []questionPromptLite{{}, {}},
		selected:  map[int][]int{},
		custom:    map[int]string{},
	}
	if questionComplete(pq) {
		t.Error("nothing answered should be incomplete")
	}
	pq.custom[0] = "my custom answer"
	if questionComplete(pq) {
		t.Error("only first question custom-answered should still be incomplete")
	}
	pq.selected[1] = []int{0}
	if !questionComplete(pq) {
		t.Error("one custom + one selected should be complete")
	}
}

func TestBuildQuestionKeyboardOtherButton(t *testing.T) {
	// No real "Other" option => synthetic "Something else" button with :other callback.
	pq := questionPending{
		questions: []questionPromptLite{{Question: "Q1", Options: []string{"a", "b"}}},
		selected:  map[int][]int{},
		custom:    map[int]string{},
	}
	rows := buildQuestionKeyboard("k", pq)
	var otherBtn string
	for _, row := range rows {
		for _, b := range row {
			if strings.Contains(b.CallbackData, ":other") {
				otherBtn = b.Text
			}
		}
	}
	if otherBtn == "" {
		t.Fatal("expected a synthetic Other button")
	}
	if !strings.Contains(otherBtn, "Something else") {
		t.Errorf("expected 'Something else' button, got %q", otherBtn)
	}

	// A real "Other" option should also route to :other and not be duplicated.
	pq2 := questionPending{
		questions: []questionPromptLite{{Question: "Q1", Options: []string{"a", "Other"}}},
		selected:  map[int][]int{},
		custom:    map[int]string{},
	}
	rows2 := buildQuestionKeyboard("k", pq2)
	count := 0
	for _, row := range rows2 {
		for _, b := range row {
			if strings.Contains(b.CallbackData, ":other") {
				count++
			}
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one Other button when a real Other option exists, got %d", count)
	}
}
