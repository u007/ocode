package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// globFixtureN creates n .txt files with distinct, increasing modification
// times (f000 oldest, f{n-1} newest), so the mtime-descending order glob emits
// is deterministic.
func globFixtureN(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	base := time.Unix(1_700_000_000, 0)
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%03d.txt", i))
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		ts := base.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runGlob(t *testing.T, ctx context.Context, argsJSON string) string {
	t.Helper()
	res, err := GlobTool{}.ExecuteCtx(ctx, json.RawMessage(argsJSON))
	if err != nil {
		t.Fatalf("glob %s: %v", argsJSON, err)
	}
	return res
}

// TestGlobJudgeAbsentGolden is the refactor guard: no judge -> byte-identical
// to the pre-judge implementation, including the mtime ordering and the
// existing "N files matched, showing first 100" note.
func TestGlobJudgeAbsentGolden(t *testing.T) {
	t.Run("uncapped", func(t *testing.T) {
		ctx := grepJudgeCtx(t, globFixtureN(t, 3), nil)
		if got, want := runGlob(t, ctx, `{"pattern":"*.txt"}`), "f002.txt\nf001.txt\nf000.txt"; got != want {
			t.Fatalf("glob golden mismatch: got %q want %q", got, want)
		}
	})
	t.Run("no_match", func(t *testing.T) {
		ctx := grepJudgeCtx(t, globFixtureN(t, 3), nil)
		if got, want := runGlob(t, ctx, `{"pattern":"*.zzz"}`), "No files matched"; got != want {
			t.Fatalf("glob no-match mismatch: got %q want %q", got, want)
		}
	})
	t.Run("capped", func(t *testing.T) {
		ctx := grepJudgeCtx(t, globFixtureN(t, globMaxResults+5), nil)
		got := runGlob(t, ctx, `{"pattern":"*.txt"}`)
		if !strings.HasPrefix(got, "f104.txt\nf103.txt\n") {
			t.Fatalf("capped glob should start with the newest files:\n%s", got)
		}
		if !strings.HasSuffix(got, fmt.Sprintf("... (%d files matched, showing first %d)", globMaxResults+5, globMaxResults)) {
			t.Fatalf("capped glob missing the truncation note:\n%s", got)
		}
		// Exactly globMaxResults path lines, then a blank line and the note.
		if n := strings.Count(strings.SplitN(got, "\n\n", 2)[0], "\n") + 1; n != globMaxResults {
			t.Fatalf("capped glob showed %d paths, want %d", n, globMaxResults)
		}
	})
}

// TestGlobJudgeReceivesMtimeOrderAndCap pins that the judge samples the
// mtime-descending order glob produces, capped at searchJudgeMaxCandidates, and
// that the unjudged remainder is disclosed rather than dropped.
func TestGlobJudgeReceivesMtimeOrderAndCap(t *testing.T) {
	n := globMaxResults + 5
	dir := globFixtureN(t, n)

	var mu sync.Mutex
	var got SearchJudgeRequest
	judge := SearchResultJudge(func(req SearchJudgeRequest) ([]SearchResult, int, error) {
		mu.Lock()
		got = req
		mu.Unlock()
		return req.Results[1:], 1, nil // veto the newest
	})

	out := runGlob(t, grepJudgeCtx(t, dir, judge), `{"pattern":"*.txt","intent":"the txt files"}`)

	mu.Lock()
	defer mu.Unlock()
	if got.Tool != "glob" || got.Intent != "the txt files" {
		t.Fatalf("tool/intent = %q/%q", got.Tool, got.Intent)
	}
	if len(got.Results) != searchJudgeMaxCandidates {
		t.Fatalf("judge received %d candidates, want %d", len(got.Results), searchJudgeMaxCandidates)
	}
	if got.Results[0].Path != fmt.Sprintf("f%03d.txt", n-1) {
		t.Fatalf("first judged candidate = %q, want the newest file", got.Results[0].Path)
	}
	if got.Results[searchJudgeMaxCandidates-1].Path != fmt.Sprintf("f%03d.txt", n-searchJudgeMaxCandidates) {
		t.Fatalf("last judged candidate = %q, want mtime-descending order preserved", got.Results[searchJudgeMaxCandidates-1].Path)
	}
	wantFooter := fmt.Sprintf("[relevance judge: 1 of %d result(s) omitted as out of scope for this intent]; %d beyond the judge cap were not judged",
		searchJudgeMaxCandidates, globMaxResults-searchJudgeMaxCandidates)
	if !strings.Contains(out, wantFooter) {
		t.Fatalf("cap footer missing:\n--- want substring ---\n%s\n--- got ---\n%s", wantFooter, out)
	}
	// The vetoed newest file is absent; the unjudged remainder is present.
	if strings.Contains(out, fmt.Sprintf("f%03d.txt", n-1)) {
		t.Fatalf("vetoed file leaked into output:\n%s", out)
	}
	if !strings.Contains(out, fmt.Sprintf("f%03d.txt", n-searchJudgeMaxCandidates-1)) {
		t.Fatalf("unjudged file past the cap was dropped:\n%s", out)
	}
}

func TestGlobJudgeVetoedFileAbsentAndDisclosed(t *testing.T) {
	dir := globFixtureN(t, 3)
	judge := SearchResultJudge(func(req SearchJudgeRequest) ([]SearchResult, int, error) {
		kept := make([]SearchResult, 0, len(req.Results))
		vetoed := 0
		for _, r := range req.Results {
			if r.Path == "f001.txt" {
				vetoed++
				continue
			}
			kept = append(kept, r)
		}
		return kept, vetoed, nil
	})
	out := runGlob(t, grepJudgeCtx(t, dir, judge), `{"pattern":"*.txt","intent":"txt"}`)
	want := "f002.txt\nf000.txt\n\n[relevance judge: 1 of 3 result(s) omitted as out of scope for this intent]"
	if out != want {
		t.Fatalf("vetoed glob mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}
}

func TestGlobJudgeAllVetoedMessage(t *testing.T) {
	dir := globFixtureN(t, 3)
	judge := SearchResultJudge(func(req SearchJudgeRequest) ([]SearchResult, int, error) {
		return nil, len(req.Results), nil
	})
	out := runGlob(t, grepJudgeCtx(t, dir, judge), `{"pattern":"*.txt","intent":"txt"}`)
	want := "Found 3 matching file(s), but none are in scope for this intent (relevance judge omitted all 3).\n" +
		`Narrow "pattern"/"path", or re-run with an intent that matches what these files contain.`
	if out != want {
		t.Fatalf("all-vetoed glob mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}
}

func TestGlobJudgeErrorFooter(t *testing.T) {
	dir := globFixtureN(t, 3)
	judge := SearchResultJudge(func(req SearchJudgeRequest) ([]SearchResult, int, error) {
		return nil, 0, fmt.Errorf("boom")
	})
	out := runGlob(t, grepJudgeCtx(t, dir, judge), `{"pattern":"*.txt","intent":"txt"}`)
	if !strings.HasPrefix(out, "f002.txt\nf001.txt\nf000.txt") {
		t.Fatalf("fail-open must render every result:\n%s", out)
	}
	if !strings.Contains(out, "[relevance judge unavailable — results are unfiltered: boom]") {
		t.Fatalf("missing judge-unavailable footer:\n%s", out)
	}
}

func TestGlobIntentInSchema(t *testing.T) {
	def := GlobTool{}.Definition()
	params := def["parameters"].(map[string]interface{})
	props := params["properties"].(map[string]interface{})
	if _, ok := props["intent"]; !ok {
		t.Fatalf("intent missing from glob properties: %#v", props)
	}
	required := params["required"].([]string)
	for _, r := range required {
		if r == "intent" {
			return
		}
	}
	t.Fatalf("intent missing from glob required list: %#v", required)
}
