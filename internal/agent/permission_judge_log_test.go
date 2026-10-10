package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/paths"
)

// captureDebug redirects the agent debug sink for the duration of a test and
// returns the collected messages. t.Cleanup restores the previous sink, so a
// failure cannot leak a global into another test.
func captureDebug(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	prev := DebugAppend
	DebugAppend = func(kind, msg string) {
		mu.Lock()
		defer mu.Unlock()
		if kind == "PERMISSION" {
			lines = append(lines, msg)
		}
	}
	t.Cleanup(func() { DebugAppend = prev })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(lines))
		copy(out, lines)
		return out
	}
}

// decodeRecords parses the captured PERMISSION lines that are judge records.
func decodeRecords(t *testing.T, lines []string) []permissionJudgeRecord {
	t.Helper()
	var recs []permissionJudgeRecord
	for _, l := range lines {
		if !strings.HasPrefix(l, "{") {
			continue
		}
		var r permissionJudgeRecord
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			continue
		}
		if r.Outcome == "" {
			continue
		}
		recs = append(recs, r)
	}
	return recs
}

// A record must carry everything needed to explain a below-floor deferral after
// the fact: the roots the judge saw, the working directory, the floor it was held
// to, the confidence, the concern, and the outcome. This is the whole point of
// the file — the 0.21 investigation failed because none of this survived.
func TestPermissionJudgeRecord_CarriesDiagnosticFields(t *testing.T) {
	lines := captureDebug(t)
	a := &Agent{permissions: NewPermissionManager()}
	a.permissions.SetWorkDir(t.TempDir())
	a.SetSessionID("ses_test_123")

	a.logPermissionJudge(a.newJudgeRecord("bash", "jev-latest",
		json.RawMessage(`{"command":"cd /tmp && ls"}`),
		&PermissionRequest{Rule: "tool.bash", Scope: PermissionScopeTool},
		permissionJudgeRecord{Outcome: outcomeBelowFloor, Floor: 0.85}))

	recs := decodeRecords(t, lines())
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 judge record, got %d", len(recs))
	}
	r := recs[0]
	if r.Outcome != outcomeBelowFloor {
		t.Errorf("outcome = %q, want %q", r.Outcome, outcomeBelowFloor)
	}
	if r.Floor != 0.85 {
		t.Errorf("floor = %v, want 0.85", r.Floor)
	}
	if r.Tool != "bash" || r.Model != "jev-latest" {
		t.Errorf("tool/model = %q/%q, want bash/jev-latest", r.Tool, r.Model)
	}
	if r.Session != "ses_test_123" {
		t.Errorf("session = %q, want ses_test_123", r.Session)
	}
	if r.WorkDir == "" {
		t.Error("working_directory is empty; a below-floor record is undiagnosable without it")
	}
	if len(r.AllowedRoots) == 0 {
		t.Error("allowed_roots is empty; this is the field whose absence made the 0.21 unexplainable")
	}
	if r.Time == "" {
		t.Error("time is empty")
	}
	if r.Rule != "tool.bash" {
		t.Errorf("rule = %q, want tool.bash", r.Rule)
	}
}

// The record must show the cd-fold, because "was a cd folded for this call?" is
// the first question when a verdict looks wrong.
func TestPermissionJudgeRecord_RecordsTheFold(t *testing.T) {
	lines := captureDebug(t)
	root := t.TempDir()
	a := &Agent{permissions: NewPermissionManager()}
	a.permissions.SetWorkDir(root)

	a.logPermissionJudge(a.newJudgeRecord("bash", "jev-latest",
		json.RawMessage(`{"command":"cd `+root+` && ls drizzle"}`), nil,
		permissionJudgeRecord{Outcome: outcomeGranted, Floor: 0.85}))

	recs := decodeRecords(t, lines())
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if recs[0].ResolvedCD != root {
		t.Errorf("resolved_cd = %q, want %q", recs[0].ResolvedCD, root)
	}
	if strings.Contains(recs[0].Command, "cd ") {
		t.Errorf("command should be the folded form, got %q", recs[0].Command)
	}
}

// The log is durable and likely to be attached to a bug report, so a command
// carrying secret material must not be written even when /mask is off. This is
// the unconditional backstop; the masking registry covers the /mask case.
func TestPermissionJudgeRecord_WithholdsSecretBearingCommand(t *testing.T) {
	lines := captureDebug(t)
	a := &Agent{permissions: NewPermissionManager()}
	a.permissions.SetWorkDir(t.TempDir())

	const leaky = `curl -H "Authorization: Bearer ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789" https://api.example.com`
	a.logPermissionJudge(a.newJudgeRecord("bash", "jev-latest",
		json.RawMessage(`{"command":`+mustJSONString(leaky)+`}`), nil,
		permissionJudgeRecord{Outcome: outcomeGranted, Floor: 0.85}))

	recs := decodeRecords(t, lines())
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	raw, err := json.Marshal(recs[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") {
		t.Errorf("secret leaked into the judge log record: %s", raw)
	}
	if recs[0].Command != "" {
		t.Errorf("command should have been withheld, got %q", recs[0].Command)
	}
	if recs[0].CommandWithheld == "" {
		t.Error("command_withheld should explain the omission")
	}
	// Withholding must not gut the diagnostic value.
	if recs[0].Outcome == "" || recs[0].Floor == 0 {
		t.Error("withholding the command must not drop the outcome or floor")
	}
}

// Logging must never be able to fail a permission decision, and a nil agent (as
// in a partially built one) must not panic the permission path.
func TestPermissionJudgeRecord_NilAgentDoesNotPanic(t *testing.T) {
	lines := captureDebug(t)
	var a *Agent
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("logPermissionJudge panicked on a nil agent: %v", r)
		}
	}()
	a.logPermissionJudge(permissionJudgeRecord{Tool: "bash", Outcome: outcomeGranted})
	if len(lines()) == 0 {
		t.Error("expected a record to still be emitted for a nil agent")
	}
}

// Registration is one-shot: the mirror is process-global state, so a second
// verdict must not re-register it.
func TestPermissionJudgeLog_RegistrationIsIdempotent(t *testing.T) {
	captureDebug(t)
	ensurePermissionJudgeLog()
	ensurePermissionJudgeLog()
	ensurePermissionJudgeLog()
	dir, err := paths.LogsDir()
	if err != nil {
		t.Skipf("logs dir unavailable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "permission-judge.log")); err != nil {
		t.Errorf("permission-judge.log not created at %s: %v", dir, err)
	}
}

func mustJSONString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// The mirror image of the withholding test: a benign command must be recorded
// verbatim. Without this, an over-eager detector that withheld EVERY command
// would still pass the suite while destroying the log's entire diagnostic value —
// the failure mode being guarded is "no record", not just "leaked secret".
func TestPermissionJudgeRecord_KeepsBenignCommandVerbatim(t *testing.T) {
	lines := captureDebug(t)
	a := &Agent{permissions: NewPermissionManager()}
	root := t.TempDir()
	a.permissions.SetWorkDir(root)

	const benign = `total=$(ls drizzle/*.sql | wc -l | tr -d ' '); echo "total: $total"`
	a.logPermissionJudge(a.newJudgeRecord("bash", "jev-latest",
		json.RawMessage(`{"command":`+mustJSONString(benign)+`}`), nil,
		permissionJudgeRecord{Outcome: outcomeGranted, Floor: 0.85}))

	recs := decodeRecords(t, lines())
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if recs[0].Command == "" {
		t.Error("a benign command was withheld; the log would be diagnostically useless")
	}
	if recs[0].CommandWithheld != "" {
		t.Errorf("unexpected withholding notice: %q", recs[0].CommandWithheld)
	}
	if !strings.Contains(recs[0].Command, "drizzle/*.sql") {
		t.Errorf("benign command not recorded verbatim: %q", recs[0].Command)
	}
}

// The root list must stay small enough to be useful: recording all ~100 roots
// made each record ~100KB and left only ~20 records in the 2MB cap, which is a
// log that cannot hold a session. The total is still reported so "the target was
// in none of the N roots" stays answerable.
func TestPermissionJudgeRecord_TrimsAllowedRoots(t *testing.T) {
	lines := captureDebug(t)
	root := t.TempDir()
	a := &Agent{permissions: NewPermissionManager()}
	a.permissions.SetWorkDir(root)

	a.logPermissionJudge(a.newJudgeRecord("bash", "jev-latest",
		json.RawMessage(`{"command":"cd `+root+`/sub && ls `+root+`/sub/x"}`), nil,
		permissionJudgeRecord{Outcome: outcomeGranted, Floor: 0.85}))

	recs := decodeRecords(t, lines())
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	r := recs[0]
	if r.AllowedRootsAll < len(r.AllowedRoots) {
		t.Errorf("allowed_roots_total (%d) is smaller than the number logged (%d)",
			r.AllowedRootsAll, len(r.AllowedRoots))
	}
	if len(r.AllowedRoots) > judgeMaxLoggedRoots {
		t.Errorf("logged %d roots, cap is %d", len(r.AllowedRoots), judgeMaxLoggedRoots)
	}
	// The workdir and the paths the command names must survive the trim.
	// Roots are logged symlink-resolved, so compare in the same space.
	want := normalizeForCompare(root)
	found := false
	for _, got := range r.AllowedRoots {
		if normalizeForCompare(got) == want {
			found = true
		}
	}
	if !found {
		t.Errorf("the working directory root was trimmed away; want %s in %v", want, r.AllowedRoots)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 4096 {
		t.Errorf("record is %d bytes; too large to keep many records in the 2MB cap", len(raw))
	}
}

// A URL carrying credentials in the command must be withheld, not just the
// vendor-format token shapes.
func TestPermissionJudgeRecord_WithholdsURLCredentials(t *testing.T) {
	lines := captureDebug(t)
	a := &Agent{permissions: NewPermissionManager()}
	a.permissions.SetWorkDir(t.TempDir())

	const leaky = "psql postgres://admin:hunter2correcthorse@db.internal:5432/app -c 'select 1'"
	a.logPermissionJudge(a.newJudgeRecord("bash", "jev-latest",
		json.RawMessage(`{"command":`+mustJSONString(leaky)+`}`), nil,
		permissionJudgeRecord{Outcome: outcomeGranted, Floor: 0.85}))

	recs := decodeRecords(t, lines())
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	raw, err := json.Marshal(recs[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hunter2correcthorse") {
		t.Errorf("URL credential leaked into the judge log: %s", raw)
	}
	if recs[0].Command != "" || recs[0].CommandWithheld == "" {
		t.Errorf("expected the command to be withheld, got command=%q withheld=%q",
			recs[0].Command, recs[0].CommandWithheld)
	}
}
