package session

// Regression tests for the HIGH review findings: cross-process concurrent
// appends, fail-safe legacy migration, and the bounded striped lock pool.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// Two independent processes appending different messages to the same session
// must both survive: the loser of a commit race retries against the fresh
// tail (conflict error, never a silent drop) until its message lands.
func TestCrossProcessConcurrentAppendsBothSurvive(t *testing.T) {
	if os.Getenv("OCODE_APPEND_CHILD") == "1" {
		appendChildMain()
		return
	}

	dir := t.TempDir()
	id := "ses_xproc"

	base := []agent.Message{{Role: "user", Content: "base"}}
	if err := saveToDir(dir, id, "", base, nil, false, 0); err != nil {
		t.Fatalf("saveToDir (seed): %v", err)
	}

	goFile := filepath.Join(dir, "go")
	childMsgs := []string{"child-A-message", "child-B-message"}

	// Start all children, then open the barrier so their writes overlap.
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	procs := make([]*os.Process, 0, len(childMsgs))
	for _, msg := range childMsgs {
		cmd := exec.Command(self, "-test.run=TestCrossProcessConcurrentAppendsBothSurvive")
		cmd.Env = append(os.Environ(),
			"OCODE_APPEND_CHILD=1",
			"OCODE_APPEND_DIR="+dir,
			"OCODE_APPEND_ID="+id,
			"OCODE_APPEND_MSG="+msg,
			"OCODE_APPEND_GO="+goFile,
		)
		if err := cmd.Start(); err != nil {
			t.Fatalf("start child: %v", err)
		}
		procs = append(procs, cmd.Process)
	}
	// Give both children a moment to reach the barrier, then release them.
	time.Sleep(500 * time.Millisecond)
	if err := os.WriteFile(goFile, []byte("go"), 0600); err != nil {
		t.Fatalf("open barrier: %v", err)
	}
	for _, p := range procs {
		state, err := p.Wait()
		if err != nil {
			t.Fatalf("child wait: %v", err)
		}
		if !state.Success() {
			t.Fatalf("child process failed: %v", state)
		}
	}

	got, err := readSqliteSession(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("readSqliteSession: %v", err)
	}
	contents := map[string]bool{}
	for _, m := range got.Messages {
		contents[m.Content] = true
	}
	if !contents["base"] || !contents["child-A-message"] || !contents["child-B-message"] {
		t.Fatalf("concurrent appends lost a message: %+v", got.Messages)
	}
}

// appendChildMain runs in a re-executed test binary: it waits on the barrier
// file, then appends its message after the current tail, retrying on
// conflict errors until the message lands or attempts run out.
func appendChildMain() {
	dir := os.Getenv("OCODE_APPEND_DIR")
	id := os.Getenv("OCODE_APPEND_ID")
	msg := os.Getenv("OCODE_APPEND_MSG")
	goFile := os.Getenv("OCODE_APPEND_GO")

	deadline := time.Now().Add(30 * time.Second)
	for !fileExists(goFile) {
		if time.Now().After(deadline) {
			os.Exit(2)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for attempt := 0; attempt < 50; attempt++ {
		s, err := readSqliteSession(sqliteSessionPath(dir, id))
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		for _, m := range s.Messages {
			if m.Content == msg {
				os.Exit(0)
			}
		}
		next := append(append([]agent.Message(nil), s.Messages...), agent.Message{Role: "user", Content: msg})
		if err := saveToDir(dir, id, "", next, nil, false, 0); err != nil {
			// Conflict with the sibling writer (or a transient lock):
			// re-read the fresh tail and retry — never drop the message.
			time.Sleep(20 * time.Millisecond)
			continue
		}
		os.Exit(0)
	}
	os.Exit(3)
}

// Corrupt legacy sources must fail the migration with the original retained:
// writing only the caller's snapshot and deleting the source would destroy
// the only remaining copy of the transcript.
func TestMigrateCorruptLegacyRetainsOriginal(t *testing.T) {
	newMsg := []agent.Message{{Role: "user", Content: "new"}}

	t.Run("malformed JSON", func(t *testing.T) {
		dir := t.TempDir()
		id := "ses_badjson"
		raw := []byte(`{"id": "ses_badjson", "title": "keep me", "messages": [BROKEN`)
		assertMigrationFailsRetaining(t, dir, id, id+".json", raw, newMsg)
	})

	t.Run("truncated JSON", func(t *testing.T) {
		dir := t.TempDir()
		id := "ses_truncjson"
		raw := []byte(`{"id": "ses_truncjson", "title": "keep me", "messages": [{"role": "user", "content": "hi"}, {"role": "ass`)
		assertMigrationFailsRetaining(t, dir, id, id+".json", raw, newMsg)
	})

	t.Run("wrong-type JSON", func(t *testing.T) {
		dir := t.TempDir()
		id := "ses_typejson"
		raw := []byte(`"just a string, not a session"`)
		assertMigrationFailsRetaining(t, dir, id, id+".json", raw, newMsg)
	})

	t.Run("empty JSON", func(t *testing.T) {
		dir := t.TempDir()
		id := "ses_emptyjson"
		assertMigrationFailsRetaining(t, dir, id, id+".json", []byte{}, newMsg)
	})

	t.Run("garbage ojsonl", func(t *testing.T) {
		dir := t.TempDir()
		id := "ses_badojsonl"
		raw := []byte("this is not ojsonl at all\n{\"also\": \"broken\"\n")
		assertMigrationFailsRetaining(t, dir, id, id+".ojsonl", raw, newMsg)
	})

	t.Run("corrupt mid-file ojsonl", func(t *testing.T) {
		dir := t.TempDir()
		id := "ses_midojsonl"
		if err := saveOjsonl(dir, id, "keep me", []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
			t.Fatalf("saveOjsonl (seed): %v", err)
		}
		path := ojsonlSessionPath(dir, id)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatalf("reopen ojsonl: %v", err)
		}
		// Corrupt line followed by a valid line: the corruption is NOT the
		// tail, so the loader must reject the file (a torn tail alone is
		// tolerated — see the next case).
		if _, err := f.WriteString("{\"type\": \"msg\", BROKEN\n"); err != nil {
			t.Fatalf("append corrupt line: %v", err)
		}
		valid, err := encodeMsgLine(agent.Message{Role: "assistant", Content: "after"})
		if err != nil {
			t.Fatalf("encode valid line: %v", err)
		}
		if _, err := f.Write(valid); err != nil {
			t.Fatalf("append valid line: %v", err)
		}
		f.Close()
		raw, _ := os.ReadFile(path)
		assertMigrationFailsRetaining(t, dir, id, id+".ojsonl", raw, newMsg)
	})

	t.Run("torn tail ojsonl still migrates", func(t *testing.T) {
		// A crash-torn last line is tolerated by the loader (dropped), so
		// migration succeeds and the source is removed only after the new
		// file round-trips.
		dir := t.TempDir()
		id := "ses_tornojsonl"
		if err := saveOjsonl(dir, id, "keep me", []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
			t.Fatalf("saveOjsonl (seed): %v", err)
		}
		path := ojsonlSessionPath(dir, id)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatalf("reopen ojsonl: %v", err)
		}
		if _, err := f.WriteString("{\"type\": \"msg\", \"message\": {\"role\": \"user\", \"content\": \"tor"); err != nil {
			t.Fatalf("append torn tail: %v", err)
		}
		f.Close()
		if err := saveToDir(dir, id, "", []agent.Message{{Role: "user", Content: "hi"}}, nil, false, 0); err != nil {
			t.Fatalf("torn-tail migration should succeed, got: %v", err)
		}
		if fileExists(path) {
			t.Fatalf("valid migration must remove the legacy file after verification")
		}
		got, err := readSqliteSession(sqliteSessionPath(dir, id))
		if err != nil {
			t.Fatalf("readSqliteSession: %v", err)
		}
		if len(got.Messages) != 1 || got.Title != "keep me" {
			t.Fatalf("migrated content wrong: %+v", got)
		}
	})
}

func assertMigrationFailsRetaining(t *testing.T, dir, id, legacyName string, raw []byte, msgs []agent.Message) {
	t.Helper()
	legacyPath := filepath.Join(dir, legacyName)
	if !fileExists(legacyPath) {
		if err := os.WriteFile(legacyPath, raw, 0600); err != nil {
			t.Fatalf("seed legacy file: %v", err)
		}
	} else {
		if data, _ := os.ReadFile(legacyPath); string(data) != string(raw) {
			t.Fatalf("legacy seed mismatch before migration")
		}
	}
	if err := saveToDir(dir, id, "", msgs, nil, false, 0); err == nil {
		t.Fatalf("expected saveToDir to fail on corrupt legacy %s", legacyName)
	}
	if !fileExists(legacyPath) {
		t.Fatalf("corrupt legacy %s was deleted by a failed migration", legacyName)
	}
	if data, _ := os.ReadFile(legacyPath); string(data) != string(raw) {
		t.Fatalf("corrupt legacy %s was modified by a failed migration", legacyName)
	}
	if fileExists(sqliteSessionPath(dir, id)) {
		t.Fatalf("failed migration must not leave a partial .sqlite file")
	}
}

// The in-process lock pool must be bounded: any number of distinct sessions
// maps into a fixed set of stripe mutexes, and one session always maps to
// the same stripe.
func TestLockForStripedPoolBounded(t *testing.T) {
	dir := t.TempDir()
	if lockFor(dir, "ses_a") != lockFor(dir, "ses_a") {
		t.Fatalf("lockFor must return a stable mutex for one session")
	}
	seen := map[*sync.Mutex]bool{}
	for i := 0; i < 2000; i++ {
		seen[lockFor(dir, strings.Repeat("x", i%7)+"_stripe_"+string(rune('a'+i%26))+string(rune('0'+(i/26)%10))+strings.Repeat("y", i%5))] = true
	}
	if len(seen) > sessionLockStripes {
		t.Fatalf("lock pool grew past %d stripes: %d", sessionLockStripes, len(seen))
	}
}
