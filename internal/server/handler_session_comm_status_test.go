package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTUIStatusFixture(t *testing.T, turnActive bool, pid int, updatedAt time.Time) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ses_fx.json")
	b, _ := json.Marshal(map[string]any{
		"session_id":  "ses_fx",
		"turn_active": turnActive,
		"pid":         pid,
		"updated_at":  updatedAt.UTC().Format(time.RFC3339Nano),
	})
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestTUITurnActiveFromFile: a TUI record counts as a live turn only when it is
// fresh and its pid is running. A crashed TUI leaves turn_active=true behind.
func TestTUITurnActiveFromFile(t *testing.T) {
	now := time.Now()
	live := os.Getpid()
	const deadPID = 2147483646 // not a running process on any realistic host
	cases := []struct {
		name   string
		active bool
		pid    int
		age    time.Duration
		want   bool
	}{
		{"fresh and live", true, live, 0, true},
		{"stale heartbeat", true, live, 5 * time.Minute, false},
		{"dead pid", true, deadPID, 0, false},
		{"turn ended", false, live, 0, false},
		{"no pid recorded", true, 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := writeTUIStatusFixture(t, c.active, c.pid, now.Add(-c.age))
			if got := tuiTurnActiveFromFile(p, now); got != c.want {
				t.Fatalf("tuiTurnActiveFromFile = %v, want %v", got, c.want)
			}
		})
	}
	if tuiTurnActiveFromFile(filepath.Join(t.TempDir(), "missing.json"), now) {
		t.Fatal("missing file reported as live")
	}
}
