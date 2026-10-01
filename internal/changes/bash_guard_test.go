package changes

// Guard rails for the bash pre/post stat-walk detector.
//
// Background: a real session (ses_2026-10-01-111854-5ef12a76) showed 4,347
// rows in the changes tab, 4,214 of them attributed to a single
// `cd <workdir> && cat >> TODO.md`. Three independent defects combined:
//
//  1. Pre() hit its walkBudget and returned a PARTIAL fingerprint, so every
//     file the walk never reached looked brand new and diffFingerprints
//     reported it as BashAdded.
//  2. The `cd <workdir>` prefix supplied a path token naming the workDir
//     DIRECTORY, and touchMatchesCandidates matches a candidate as a
//     substring of the path — so one token authorized the whole tree and the
//     "intersect with the command's paths" filter did nothing.
//  3. Nothing bounded how many touches one event could contribute.
//
// Each test below pins one guard, plus the non-regression cases that keep the
// useful detections alive: a small unnamed write (`cat >> TODO.md` names no
// slash-bearing token at all) must still be recorded.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestRecorder returns the concrete recorder so tests can tighten the
// walk budget / touch cap. The exported constructor returns the interface.
func newTestRecorder(t *testing.T, dir string, reg *Registry) *StatBashRecorder {
	t.Helper()
	rec, ok := NewStatBashRecorder(dir, reg).(*StatBashRecorder)
	if !ok {
		t.Fatalf("NewStatBashRecorder returned %T, want *StatBashRecorder", rec)
	}
	return rec
}

// writeFiles creates n small files named f000.txt… directly in dir.
func writeFiles(t *testing.T, dir string, n int) []string {
	t.Helper()
	paths := make([]string, 0, n)
	for i := range n {
		p := filepath.Join(dir, fmt.Sprintf("f%03d.txt", i))
		if err := os.WriteFile(p, []byte("v1\n"), 0644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

// onlySkip returns the single retained SkipNotice, failing if there is not
// exactly one.
func onlySkip(t *testing.T, reg *Registry) SkipNotice {
	t.Helper()
	skips := reg.BashSkips()
	if len(skips) != 1 {
		t.Fatalf("BashSkips() = %d notices, want 1: %+v", len(skips), skips)
	}
	return skips[0]
}

// listPaths renders a FileChange list as bare paths, bounded: a 210-row
// failure has to stay readable in the test output.
func listPaths(list []FileChange) string {
	const max = 5
	out := make([]string, 0, max+1)
	for i, fc := range list {
		if i == max {
			out = append(out, "…")
			break
		}
		out = append(out, fc.OriginalPath)
	}
	return strings.Join(out, ", ")
}

// ---------------------------------------------------------------------------
// Fix 1 — an incomplete baseline must never be diffed
// ---------------------------------------------------------------------------

// A Pre() walk that blew its time budget covers only a lexical PREFIX of the
// tree. Diffing the post-walk against it reports every remaining file as
// newly created, which is how one `cat >> TODO.md` claimed 4,214 phantom
// "added" rows. The event must be dropped, not diffed.
func TestStatBashRecorderDropsEventWhenPreWalkTruncated(t *testing.T) {
	if !hasShell() {
		t.Skip("/bin/sh not available")
	}
	tmpDir := t.TempDir()
	reg := NewRegistry()
	rec := newTestRecorder(t, tmpDir, reg)

	// Starve ONLY the pre-walk. Leaving the post-walk starved too would
	// empty the diff for a second, unrelated reason and the test would
	// pass without ever exercising the guard.
	rec.budget = time.Nanosecond
	pre := rec.Pre()
	rec.budget = time.Second
	if pre.complete {
		t.Fatal("Pre() reported a complete baseline with a 1ns walk budget")
	}

	cmd := "echo hi > foo"
	rec.Post(pre, cmd, runShell(tmpDir, cmd))

	if got := len(reg.List()); got != 0 {
		t.Fatalf("entries = %d, want 0: a truncated baseline must not be diffed (%s)",
			got, listPaths(reg.List()))
	}
	if n := onlySkip(t, reg); n.Reason != SkipIncompleteBaseline {
		t.Errorf("skip reason = %q, want %q", n.Reason, SkipIncompleteBaseline)
	}
}

// The post-walk truncating is just as poisonous, in the other direction: the
// abort point is arbitrary, so files the pre-walk DID cover read as deleted.
// A removal is the honest way to provoke it — a file created during the
// command is simply absent from a truncated post-walk, which proves nothing.
func TestStatBashRecorderDropsEventWhenPostWalkTruncated(t *testing.T) {
	if !hasShell() {
		t.Skip("/bin/sh not available")
	}
	tmpDir := t.TempDir()
	victim := filepath.Join(tmpDir, "victim.txt")
	if err := os.WriteFile(victim, []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	rec := newTestRecorder(t, tmpDir, reg)

	pre := rec.Pre()
	if !pre.complete {
		t.Fatal("Pre() reported an incomplete baseline with the default budget")
	}
	cmd := "rm victim.txt"
	code := runShell(tmpDir, cmd)
	rec.budget = time.Nanosecond // only the post-walk is starved now
	rec.Post(pre, cmd, code)

	if got := len(reg.List()); got != 0 {
		t.Fatalf("entries = %d, want 0: a truncated post-walk must not be diffed (%s)",
			got, listPaths(reg.List()))
	}
	if n := onlySkip(t, reg); n.Reason != SkipIncompleteBaseline {
		t.Errorf("skip reason = %q, want %q", n.Reason, SkipIncompleteBaseline)
	}
}

// The guard must not disarm the detector: a full walk still records writes.
func TestStatBashRecorderCompleteBaselineStillRecords(t *testing.T) {
	if !hasShell() {
		t.Skip("/bin/sh not available")
	}
	tmpDir := t.TempDir()
	reg := NewRegistry()
	rec := newTestRecorder(t, tmpDir, reg)

	pre := rec.Pre()
	if !pre.complete {
		t.Fatal("Pre() reported an incomplete baseline on an empty tempdir")
	}
	cmd := "echo hi > foo"
	rec.Post(pre, cmd, runShell(tmpDir, cmd))

	if got := len(reg.List()); got != 1 {
		t.Fatalf("entries = %d, want 1", got)
	}
	if n := len(reg.BashSkips()); n != 0 {
		t.Errorf("BashSkips() = %d, want 0 on a clean walk", n)
	}
}

// ---------------------------------------------------------------------------
// Fix 2 — a `cd` is a location, not a target
// ---------------------------------------------------------------------------

func TestTargetTokensDropWorkDirAndAncestors(t *testing.T) {
	workDir := filepath.Join(string(filepath.Separator)+"Users", "james", "www", "ocode")
	rec := newTestRecorder(t, workDir, NewRegistry())

	cases := []struct {
		name    string
		command string
		keep    []string
		drop    []string
	}{
		{
			name:    "cd workdir is a location, not a target",
			command: "cd " + workDir + " && cat >> TODO.md",
			drop:    []string{workDir},
		},
		{
			name:    "cd above the workdir is a location too",
			command: "cd " + filepath.Dir(workDir) + " && go build ./x",
			drop:    []string{filepath.Dir(workDir)},
		},
		{
			name:    "an absolute path inside the workdir is a real target",
			command: "cp -r " + filepath.Join(workDir, "docs") + " /tmp/snap",
			keep:    []string{filepath.Join(workDir, "docs"), "/tmp/snap"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rec.targetTokens(tc.command)
			for _, k := range tc.keep {
				if _, ok := got[k]; !ok {
					t.Errorf("targetTokens dropped target %q; got %v", k, keysOf(got))
				}
			}
			for _, d := range tc.drop {
				if _, ok := got[d]; ok {
					t.Errorf("targetTokens kept location token %q", d)
				}
			}
		})
	}
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// A command whose only path token is the workDir names no file, so the diff
// is unattributable by name. It must still not fill the tab: with the location
// token gone there is nothing to narrow by, so the touch cap is what holds the
// line. (The token filter is pinned on its own by
// TestStatBashRecorderKeepsNamedTargetAndDropsRestOfFlood, where a real
// target token narrows a flood while the cap is raised out of the way.)
func TestStatBashRecorderDoesNotLetUnnamedFloodFillChangesTab(t *testing.T) {
	tmpDir := t.TempDir()
	reg := NewRegistry()
	rec := newTestRecorder(t, tmpDir, reg) // default cap (maxTouchesPerEvent)

	pre := rec.Pre()
	writeFiles(t, tmpDir, maxTouchesPerEvent+10) // a concurrent writer, not this shell
	cmd := "cd " + tmpDir + " && true"
	rec.Post(pre, cmd, 0)

	if got := len(reg.List()); got != 0 {
		t.Fatalf("entries = %d, want 0: an unattributable flood must not reach the tab (%s)",
			got, listPaths(reg.List()))
	}
	if n := onlySkip(t, reg); n.Reason != SkipTooManyTouches {
		t.Errorf("skip reason = %q, want %q", n.Reason, SkipTooManyTouches)
	}
}

// A named target inside the same flood IS recorded, and nothing else is: the
// filter narrows attribution rather than dropping the event wholesale.
func TestStatBashRecorderKeepsNamedTargetAndDropsRestOfFlood(t *testing.T) {
	if !hasShell() {
		t.Skip("/bin/sh not available")
	}
	tmpDir := t.TempDir()
	sub := filepath.Join(tmpDir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(sub, "t.txt")
	if err := os.WriteFile(target, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	rec := newTestRecorder(t, tmpDir, reg)
	rec.maxTouches = 1000 // isolate the token filter from the touch cap

	pre := rec.Pre()
	writeFiles(t, tmpDir, 210) // concurrent writer, unrelated to the command
	// `sed -i` needs a backup-suffix argument on BSD sed, so write with a
	// redirect: `sub/t.txt` carries a slash, which makes it a real token —
	// exactly the shape this test needs to tell apart from the workDir token.
	cmd := "cd " + tmpDir + " && printf b > sub/t.txt"
	rec.Post(pre, cmd, runShell(tmpDir, cmd))

	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("entries = %d, want 1 (only the named target): %s", len(list), listPaths(list))
	}
	if list[0].OriginalPath != target {
		t.Errorf("path = %q, want %q", list[0].OriginalPath, target)
	}
}

// Non-regression: `cat >> TODO.md` produces NO slash-bearing path token, so
// the command names no file at all. The unbounded fail-open this replaced let
// the whole diff through; the bounded one keeps small diffs (a shell that
// changed a handful of files during its own run almost certainly did it) and
// is what makes this fix safe to ship without losing heredoc/sed detection.
func TestStatBashRecorderKeepsSmallDiffWithNoPathTokens(t *testing.T) {
	if !hasShell() {
		t.Skip("/bin/sh not available")
	}
	tmpDir := t.TempDir()
	reg := NewRegistry()
	rec := newTestRecorder(t, tmpDir, reg)

	pre := rec.Pre()
	cmd := "printf 'note\\n' >> TODO.md"
	rec.Post(pre, "cd "+tmpDir+" && "+cmd, runShell(tmpDir, cmd))

	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("entries = %d, want 1: an unnamed but small write must survive", len(list))
	}
	if list[0].OriginalPath != filepath.Join(tmpDir, "TODO.md") {
		t.Errorf("path = %q, want %q", list[0].OriginalPath, filepath.Join(tmpDir, "TODO.md"))
	}
}

// ---------------------------------------------------------------------------
// Fix 3 — bound one event, and bound the registry
// ---------------------------------------------------------------------------

// An event above the cap is a walk artifact, a bulk rewrite, or another
// process's writes — not something a shell command did. Drop it whole: a
// truncated list would be a lie about which files were touched.
func TestStatBashRecorderDropsEventAboveTouchCap(t *testing.T) {
	tmpDir := t.TempDir()
	reg := NewRegistry()
	rec := newTestRecorder(t, tmpDir, reg)
	rec.maxTouches = 3

	pre := rec.Pre()
	writeFiles(t, tmpDir, 4)
	rec.Post(pre, "cd "+tmpDir+" && true", 0)

	if got := len(reg.List()); got != 0 {
		t.Fatalf("entries = %d, want 0: an oversized event must be dropped whole", got)
	}
	n := onlySkip(t, reg)
	if n.Reason != SkipTooManyTouches {
		t.Errorf("skip reason = %q, want %q", n.Reason, SkipTooManyTouches)
	}
	if n.Paths != 4 {
		t.Errorf("notice Paths = %d, want 4", n.Paths)
	}
}

// The cap is a ceiling, not a new low-water mark: an event at the cap lands.
func TestStatBashRecorderKeepsEventAtTouchCap(t *testing.T) {
	tmpDir := t.TempDir()
	reg := NewRegistry()
	rec := newTestRecorder(t, tmpDir, reg)
	rec.maxTouches = 3

	pre := rec.Pre()
	writeFiles(t, tmpDir, 3)
	rec.Post(pre, "cd "+tmpDir+" && true", 0)

	if got := len(reg.List()); got != 3 {
		t.Fatalf("entries = %d, want 3 (at the cap, not above it)", got)
	}
	if n := len(reg.BashSkips()); n != 0 {
		t.Errorf("BashSkips() = %d, want 0", n)
	}
}

// The registry backstop is independent of the recorder: whatever the source,
// no single session can grow the changes list without bound.
func TestNotifyBashWriteRefusesBeyondRegistryCeiling(t *testing.T) {
	reg := NewRegistry()
	reg.maxFiles = 2

	first := filepath.Join("/tmp/work", "a.txt")
	reg.NotifyBashWrite(BashWriteEvent{
		Command: "cp a.txt b.txt",
		WorkDir: "/tmp/work",
		Touches: []BashTouch{
			{Path: first, Op: BashAdded},
			{Path: filepath.Join("/tmp/work", "b.txt"), Op: BashAdded},
			{Path: filepath.Join("/tmp/work", "c.txt"), Op: BashAdded},
		},
	})

	if got := len(reg.List()); got != 2 {
		t.Fatalf("entries = %d, want 2 (ceiling)", got)
	}
	for _, fc := range reg.List() {
		if fc.OriginalPath == filepath.Join("/tmp/work", "c.txt") {
			t.Error("c.txt was tracked past the ceiling")
		}
	}
	if n := onlySkip(t, reg); n.Reason != SkipRegistryCeiling {
		t.Errorf("skip reason = %q, want %q", n.Reason, SkipRegistryCeiling)
	}

	// The ceiling gates NEW paths only. A bash touch on an already-tracked
	// path must still refresh its metadata, or LastBashCommand would go
	// stale for every row once a session approaches the ceiling.
	reg.NotifyBashWrite(BashWriteEvent{
		Command: "sed -i s/a/b/ a.txt",
		WorkDir: "/tmp/work",
		Touches: []BashTouch{{Path: first, Op: BashModified}},
	})
	for _, fc := range reg.List() {
		if fc.OriginalPath == first && fc.LastBashCommand != "sed -i s/a/b/ a.txt" {
			t.Errorf("LastBashCommand = %q, want the refreshed command", fc.LastBashCommand)
		}
	}
}
