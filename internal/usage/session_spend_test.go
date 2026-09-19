package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withTempUsageDir points the package's records file at a temp dir for the
// duration of the test, mirroring TestRecordAndQuery's isolation.
func withTempUsageDir(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	origDir := dataDirFn
	dataDirFn = func() (string, error) {
		return filepath.Join(tmpDir, "usage"), nil
	}
	t.Cleanup(func() { dataDirFn = origDir })
}

// TestRecordSessionIDRoundTrip pins the ledger's session attribution: a record
// written with a session id reads back with it, and a legacy row without `sid`
// decodes as empty rather than erroring.
func TestRecordSessionIDRoundTrip(t *testing.T) {
	withTempUsageDir(t)
	now := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)

	if err := RecordUsageForSession(now, "ses_a", "m1", "p1", 100, 50, 10, 150, 0.001); err != nil {
		t.Fatalf("RecordUsageForSession: %v", err)
	}
	// Unattributed row (process-global), written through the legacy signature.
	if err := RecordUsage(now.Add(time.Minute), "m2", "p2", 10, 5, 0, 15, 0.0002); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	recs, err := Query(time.Time{}, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].SessionID != "ses_a" {
		t.Errorf("recs[0].SessionID = %q, want ses_a", recs[0].SessionID)
	}
	if recs[1].SessionID != "" {
		t.Errorf("recs[1].SessionID = %q, want empty (unattributed)", recs[1].SessionID)
	}

	// A legacy row (no `sid` key at all) must decode without error.
	path, err := recordsPath()
	if err != nil {
		t.Fatalf("recordsPath: %v", err)
	}
	legacy := `{"t":"2026-09-19T09:00:00Z","m":"legacy","pt":1,"ct":2,"tt":3,"sp":0.5}` + "\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open records: %v", err)
	}
	if _, err := f.WriteString(legacy); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	f.Close()

	recs, err = Query(time.Time{}, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Query after legacy: %v", err)
	}
	var found bool
	for _, r := range recs {
		if r.Model == "legacy" {
			found = true
			if r.SessionID != "" {
				t.Errorf("legacy SessionID = %q, want empty", r.SessionID)
			}
			if r.Spend != 0.5 {
				t.Errorf("legacy Spend = %v, want 0.5", r.Spend)
			}
		}
	}
	if !found {
		t.Fatal("legacy record not returned by Query")
	}
}

// TestSessionSpendSumsOnlyThatSession pins the per-session ledger fallback: the
// sum includes only rows tagged with the requested session.
func TestSessionSpendSumsOnlyThatSession(t *testing.T) {
	withTempUsageDir(t)
	now := time.Now()

	_ = RecordUsageForSession(now, "ses_a", "m", "p", 1, 1, 0, 2, 0.25)
	_ = RecordUsageForSession(now, "ses_a", "m", "p", 1, 1, 0, 2, 0.10)
	_ = RecordUsageForSession(now, "ses_b", "m", "p", 1, 1, 0, 2, 9.99)
	_ = RecordUsage(now, "m", "p", 1, 1, 0, 2, 5.00) // unattributed

	got, err := SessionSpend("ses_a")
	if err != nil {
		t.Fatalf("SessionSpend: %v", err)
	}
	if got < 0.3499 || got > 0.3501 {
		t.Fatalf("SessionSpend(ses_a) = %v, want 0.35", got)
	}
	if got, _ := SessionSpend(""); got != 0 {
		t.Fatalf("SessionSpend(\"\") = %v, want 0", got)
	}
}

// TestSessionSpendMissingFileIsZero covers a fresh install with no ledger yet.
func TestSessionSpendMissingFileIsZero(t *testing.T) {
	withTempUsageDir(t)
	got, err := SessionSpend("ses_none")
	if err != nil {
		t.Fatalf("SessionSpend: %v", err)
	}
	if got != 0 {
		t.Fatalf("SessionSpend = %v, want 0", got)
	}
}

// TestRecordMarshalOmitsEmptySessionID guards the wire shape: an unattributed
// record must not emit a `sid` field.
func TestRecordMarshalOmitsEmptySessionID(t *testing.T) {
	b, err := json.Marshal(Record{Model: "m"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if containsKey(b, "sid") {
		t.Fatalf("unattributed record marshaled %s, want no sid key", b)
	}
	b, err = json.Marshal(Record{Model: "m", SessionID: "ses_a"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !containsKey(b, "sid") {
		t.Fatalf("attributed record marshaled %s, want sid key", b)
	}
}

func containsKey(b []byte, key string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}
