package server

import (
	"strings"
	"testing"
	"time"
)

func TestDerivePulseStatusPrecedence(t *testing.T) {
	ask := &PulseAsk{Kind: "permission", Summary: "run rm -rf"}
	question := &PulseAsk{Kind: "question", Summary: "which branch?"}

	tests := []struct {
		name string
		in   PulseInput
		want PulseStatus
	}{
		{"nothing at all is idle", PulseInput{}, PulseStatusIdle},
		{
			// An ask outranks a running turn: the turn is paused waiting on the
			// user, so "running" would hide the one thing needing attention.
			name: "pending permission outranks running",
			in:   PulseInput{Running: true, PendingAsk: ask},
			want: PulseStatusNeedsPermission,
		},
		{
			name: "pending question outranks a prior error",
			in:   PulseInput{LastTurnErr: "boom", PendingAsk: question},
			want: PulseStatusNeedsQuestion,
		},
		{
			// A running turn outranks a stale error: the registry clears
			// lastTurnErr when the turn goes active, and if a caller ever did
			// hand us both, "running" is the truthful reading.
			name: "running outranks last turn error",
			in:   PulseInput{Running: true, LastTurnErr: "boom"},
			want: PulseStatusRunning,
		},
		{
			name: "last turn error",
			in:   PulseInput{LastTurnErr: "llm 500"},
			want: PulseStatusError,
		},
		{
			// A todo plan alone is not activity — it survives across turns, so
			// deriving "running" from it would pin a session in Running forever.
			name: "a todo plan does not make a session run",
			in:   PulseInput{Todo: &PulseTodo{Done: 1, Total: 3, Current: "wire it"}},
			want: PulseStatusIdle,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := derivePulseStatus(tc.in); got != tc.want {
				t.Errorf("derivePulseStatus = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDerivePulseTaskFallsBackInOrder(t *testing.T) {
	todo := &PulseTodo{Done: 1, Total: 3, Current: "wire the store"}
	ask := &PulseAsk{Kind: "permission", Summary: "run rm -rf /tmp/x"}

	t.Run("needs-you carries no task, the ask is the text", func(t *testing.T) {
		in := PulseInput{PendingAsk: ask, Todo: todo, LastAssistantLine: "I will now edit"}
		if got := derivePulseTask(in); got != nil {
			t.Errorf("task = %+v, want nil (the pending ask already carries the text)", got)
		}
	})

	t.Run("in-progress todo wins over a running tool", func(t *testing.T) {
		in := PulseInput{Todo: todo, ActiveTool: "bash", LastAssistantLine: "editing"}
		got := derivePulseTask(in)
		if got == nil || got.Kind != "todo" || got.Text != "wire the store" {
			t.Errorf("task = %+v, want the todo item", got)
		}
	})

	t.Run("active tool is next, with args as a truncated hint", func(t *testing.T) {
		in := PulseInput{ActiveTool: "bash", ActiveToolArgs: "ls -la", LastAssistantLine: "checking"}
		got := derivePulseTask(in)
		if got == nil || got.Kind != "tool" {
			t.Fatalf("task = %+v, want a tool task", got)
		}
		if got.Text != "bash ls -la" {
			t.Errorf("task text = %q, want %q (tool name plus args hint)", got.Text, "bash ls -la")
		}
	})

	t.Run("last assistant line is the floor", func(t *testing.T) {
		got := derivePulseTask(PulseInput{LastAssistantLine: "wrapping up"})
		if got == nil || got.Kind != "text" || got.Text != "wrapping up" {
			t.Errorf("task = %+v, want the assistant line", got)
		}
	})

	t.Run("no inputs at all yields nothing", func(t *testing.T) {
		if got := derivePulseTask(PulseInput{}); got != nil {
			t.Errorf("task = %+v, want nil", got)
		}
	})

	t.Run("a todo with no in-progress item does not become a task", func(t *testing.T) {
		// Every item already done: Current is empty, so this must fall through
		// to the tool/text tiers rather than render an empty todo line.
		done := &PulseTodo{Done: 3, Total: 3}
		got := derivePulseTask(PulseInput{Todo: done, LastAssistantLine: "all done"})
		if got == nil || got.Kind != "text" {
			t.Errorf("task = %+v, want the assistant line", got)
		}
	})
}

func TestDerivePulseTaskToolArgsTruncatedAtRunes(t *testing.T) {
	// The card is one line; args are a hint, not the payload. Truncation must
	// be rune-based or a multi-byte command gets sliced mid-rune.
	args := strings.Repeat("é", 200) // 400 bytes, 200 runes
	got := derivePulseTask(PulseInput{ActiveTool: "bash", ActiveToolArgs: args})
	if got == nil {
		t.Fatal("task = nil")
	}
	// The budget applies to the args HINT, not the whole line — the tool name
	// leads the text and is not charged against it.
	hint, ok := strings.CutPrefix(got.Text, "bash ")
	if !ok {
		t.Fatalf("task text %q does not lead with the tool name", got.Text)
	}
	if n := len([]rune(strings.TrimSuffix(hint, "…"))); n != pulseToolArgsRuneBudget {
		t.Errorf("args hint is %d runes, want exactly %d", n, pulseToolArgsRuneBudget)
	}
	if !strings.HasSuffix(hint, "…") {
		t.Errorf("task text %q is truncated but carries no ellipsis", got.Text)
	}

	short := derivePulseTask(PulseInput{ActiveTool: "bash", ActiveToolArgs: "ls"})
	if short == nil || short.Text != "bash ls" {
		t.Errorf("short args task = %+v, want the tool name plus an untruncated hint", short)
	}
}

func pulseInputFixture(id, path string, updated time.Time) PulseInput {
	return PulseInput{SessionID: id, ProjectPath: path, Title: "t " + id, UpdatedAt: updated}
}

func TestBuildPulseRowsCountsChildrenAndHidesThem(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	parent := pulseInputFixture("ses_parent", "/p", now.Add(-time.Minute))
	parent.Running = true
	child := pulseInputFixture("ses_parent_child_task_1", "/p", now.Add(-time.Minute))

	rows := buildPulseRows([]PulseInput{parent, child}, "live", now)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (the child must not be its own card)", len(rows))
	}
	if rows[0].ChildCount != 1 {
		t.Errorf("ChildCount = %d, want 1", rows[0].ChildCount)
	}
}

func TestBuildPulseRowsInclusionWindows(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

	live23h := pulseInputFixture("ses_23h", "/p", now.Add(-23*time.Hour))
	live25h := pulseInputFixture("ses_25h", "/p", now.Add(-25*time.Hour))
	day8 := pulseInputFixture("ses_8d", "/p", now.Add(-8*24*time.Hour))
	running := pulseInputFixture("ses_running", "/p", now.Add(-72*time.Hour))
	running.Running = true

	inputs := []PulseInput{live23h, live25h, day8, running}

	live := buildPulseRows(inputs, "live", now)
	liveIDs := pulseRowIDs(live)
	for _, want := range []string{"ses_23h", "ses_running"} {
		if !liveIDs[want] {
			t.Errorf("live scope missing %s (got %v)", want, liveIDs)
		}
	}
	for _, unwanted := range []string{"ses_25h", "ses_8d"} {
		if liveIDs[unwanted] {
			t.Errorf("live scope wrongly included %s", unwanted)
		}
	}

	all := buildPulseRows(inputs, "all", now)
	allIDs := pulseRowIDs(all)
	for _, want := range []string{"ses_23h", "ses_25h", "ses_running"} {
		if !allIDs[want] {
			t.Errorf("all scope missing %s (got %v)", want, allIDs)
		}
	}
	if allIDs["ses_8d"] {
		t.Error("all scope included an 8-day-old idle session (window is 7 days)")
	}
}

func TestBuildPulseRowsSortOrder(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

	newest := pulseInputFixture("ses_b", "/p", now.Add(-time.Minute))
	older := pulseInputFixture("ses_a", "/p", now.Add(-time.Hour))
	newest.Running = true
	older.Running = true

	ask := pulseInputFixture("ses_zzz", "/p", now.Add(-3*time.Hour)) // oldest, but needs you
	ask.PendingAsk = &PulseAsk{Kind: "permission", Summary: "approve?"}

	failed := pulseInputFixture("ses_err", "/p", now.Add(-2*time.Hour))
	failed.LastTurnErr = "boom"

	rows := buildPulseRows([]PulseInput{newest, older, ask, failed}, "live", now)
	got := pulseRowIDsInOrder(rows)
	want := []string{"ses_zzz", "ses_b", "ses_a", "ses_err"}
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestBuildPulseRowsTiebreaksEqualUpdatedAtBySessionID(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	same := now.Add(-time.Minute)

	c := pulseInputFixture("ses_c", "/p", same)
	c.Running = true
	a := pulseInputFixture("ses_a", "/p", same)
	a.Running = true
	b := pulseInputFixture("ses_b", "/p", same)
	b.Running = true

	rows := buildPulseRows([]PulseInput{c, a, b}, "live", now)
	got := pulseRowIDsInOrder(rows)
	want := []string{"ses_a", "ses_b", "ses_c"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestPagePulseRowsWalksEveryRowOnce(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	var inputs []PulseInput
	for i := range 120 {
		in := pulseInputFixture(strings.Repeat("x", 3)+string(rune('a'+i%26))+itoa(i), "/p", now.Add(-time.Duration(i)*time.Minute))
		in.Running = true
		inputs = append(inputs, in)
	}
	all := buildPulseRows(inputs, "live", now)
	if len(all) != 120 {
		t.Fatalf("fixture built %d rows, want 120", len(all))
	}

	var seen []string
	cursor := ""
	pages := 0
	for {
		page, next, err := pagePulseRows(all, cursor, 50)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		pages++
		for _, r := range page {
			seen = append(seen, r.SessionID)
		}
		if next == "" {
			break
		}
		if pages > 10 {
			t.Fatal("paging did not terminate")
		}
		cursor = next
	}

	if pages != 3 {
		t.Errorf("pages = %d, want 3 (120 rows at limit 50)", pages)
	}
	if len(seen) != 120 {
		t.Fatalf("saw %d rows across pages, want 120", len(seen))
	}
	uniq := map[string]bool{}
	for _, id := range seen {
		if uniq[id] {
			t.Errorf("duplicate row %s across pages", id)
		}
		uniq[id] = true
	}
	for i, r := range all {
		if seen[i] != r.SessionID {
			t.Fatalf("page order diverged from sort order at %d: got %s want %s", i, seen[i], r.SessionID)
		}
	}
}

func TestPagePulseRowsRejectsBadCursor(t *testing.T) {
	now := time.Now()
	rows := buildPulseRows([]PulseInput{pulseInputFixture("ses_a", "/p", now)}, "live", now)
	if _, _, err := pagePulseRows(rows, "not-a-cursor", 50); err == nil {
		t.Error("err = nil for a garbage cursor, want an error (a bad cursor is a client bug, not an empty page)")
	}
	if _, _, err := pagePulseRows(rows, "", 0); err == nil {
		t.Error("err = nil for limit 0, want an error")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func pulseRowIDs(rows []PulseRow) map[string]bool {
	out := map[string]bool{}
	for _, r := range rows {
		out[r.SessionID] = true
	}
	return out
}

func pulseRowIDsInOrder(rows []PulseRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.SessionID)
	}
	return out
}
