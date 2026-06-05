package ide

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Run with: IDE_SMOKE=1 go test ./internal/ide/ -run TestSmokeLive -v
func TestSmokeLive(t *testing.T) {
	if os.Getenv("IDE_SMOKE") == "" {
		t.Skip("set IDE_SMOKE=1 to run live smoke test")
	}
	home, _ := os.UserHomeDir()
	entries, err := os.ReadDir(filepath.Join(home, ".claude", "ide"))
	if err != nil {
		t.Fatal(err)
	}
	var lock *Lock
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".lock") {
			continue
		}
		port, _ := strconv.Atoi(strings.TrimSuffix(e.Name(), ".lock"))
		data, _ := os.ReadFile(filepath.Join(home, ".claude", "ide", e.Name()))
		var raw struct {
			AuthToken string `json:"authToken"`
		}
		_ = json.Unmarshal(data, &raw)
		lock = &Lock{Port: port, AuthToken: raw.AuthToken}
		break
	}
	if lock == nil {
		t.Fatal("no lock found")
	}
	ch := make(chan Update, 16)
	client := NewClient(lock, ch)
	// Overall ceiling; the test actually finishes once the data stream goes idle
	// (idle timer below), so a healthy-but-quiet connection ends promptly instead
	// of blocking for the full cap.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	go client.Run(ctx)

	const idle = 20 * time.Second
	timer := time.NewTimer(idle)
	defer timer.Stop()
	resetIdle := func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(idle)
	}

	for {
		select {
		case u := <-ch:
			resetIdle()
			switch u.Kind {
			case UpdateConnected:
				t.Logf("CONNECTED")
			case UpdateDisconnected:
				t.Logf("DISCONNECTED")
			case UpdateOpenEditors:
				t.Logf("OPEN EDITORS: %d", len(u.OpenEditors))
				for _, e := range u.OpenEditors {
					t.Logf("  - %s active=%v dirty=%v", e.FilePath, e.Active, e.Dirty)
				}
			case UpdateSelection:
				if s, e, ok := u.Selection.LineSpan(); ok {
					t.Logf("SELECTION: %s L%d-%d", u.Selection.FilePath, s, e)
				} else {
					t.Logf("SELECTION: %s (no range)", u.Selection.FilePath)
				}
			}
		case <-timer.C:
			t.Logf("idle %s with no data — stream stopped, ending", idle)
			return
		case <-ctx.Done():
			t.Logf("reached overall ceiling")
			return
		}
	}
}
