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
)

// grepJudgeFixture is the fixed tree the grep judge tests run against. a.txt
// and b.txt match "foo"; sub/c.txt never matches.
func grepJudgeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFixture(t, dir, "a.txt", "alpha\nfoo beta\ngamma\n")
	writeFixture(t, dir, "b.txt", "foo one\nfoo two\n")
	writeFixture(t, dir, "sub/c.txt", "nothing here\n")
	return dir
}

func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func grepJudgeCtx(t *testing.T, dir string, judge SearchResultJudge) context.Context {
	t.Helper()
	ctx := WithWorkDir(context.Background(), dir)
	if judge != nil {
		ctx = WithSearchResultJudge(ctx, judge)
	}
	return ctx
}

func runGrep(t *testing.T, ctx context.Context, argsJSON string) string {
	t.Helper()
	res, err := GrepTool{}.ExecuteCtx(ctx, json.RawMessage(argsJSON))
	if err != nil {
		t.Fatalf("grep %s: %v", argsJSON, err)
	}
	return res
}

// TestGrepJudgeAbsentGolden is the refactor guard: with no judge attached the
// output must be byte-identical to the pre-judge implementation for every
// output_mode (values captured from the implementation before this change).
func TestGrepJudgeAbsentGolden(t *testing.T) {
	dir := grepJudgeFixture(t)
	ctx := grepJudgeCtx(t, dir, nil)

	cases := []struct {
		name string
		args string
		want string
	}{
		{
			name: "content",
			args: `{"pattern":"foo","include":"*.txt","output_mode":"content"}`,
			want: "a.txt:2:foo beta\n\nb.txt:1:foo one\nb.txt:2:foo two",
		},
		{
			name: "count",
			args: `{"pattern":"foo","include":"*.txt","output_mode":"count"}`,
			want: "a.txt: 1\nb.txt: 2",
		},
		{
			name: "files_with_matches",
			args: `{"pattern":"foo","include":"*.txt","output_mode":"files_with_matches"}`,
			want: "a.txt\nb.txt",
		},
		{
			name: "default_mode_is_content",
			args: `{"pattern":"foo","include":"*.txt"}`,
			want: "a.txt:2:foo beta\n\nb.txt:1:foo one\nb.txt:2:foo two",
		},
		{
			name: "no_match",
			args: `{"pattern":"zzzznotthere","include":"*.txt"}`,
			want: "No matches found",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runGrep(t, ctx, tc.args); got != tc.want {
				t.Fatalf("golden mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, tc.want)
			}
		})
	}
}

// TestGrepJudgeAbsentTruncatedInputGolden pins the large-file cap note with no
// judge attached.
func TestGrepJudgeAbsentTruncatedInputGolden(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "big.txt", "foo\n"+strings.Repeat("x", 200))
	prev := maxGrepFileBytes
	t.Cleanup(func() { maxGrepFileBytes = prev })
	maxGrepFileBytes = 64

	want := "big.txt:1:foo\nbig.txt:3:[file larger than the 64-byte scan cap — past-cap content not searched]"
	if got := runGrep(t, grepJudgeCtx(t, dir, nil), `{"pattern":"foo","include":"*.txt","output_mode":"content"}`); got != want {
		t.Fatalf("truncated-input golden mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestGrepJudgeReceivesPerFileResults pins the candidate shape: one
// SearchResult per matching file, with the count and a line-numbered summary.
func TestGrepJudgeReceivesPerFileResults(t *testing.T) {
	dir := grepJudgeFixture(t)
	var mu sync.Mutex
	var got SearchJudgeRequest
	calls := 0
	judge := SearchResultJudge(func(req SearchJudgeRequest) ([]SearchResult, int, error) {
		mu.Lock()
		got = req
		calls++
		mu.Unlock()
		return req.Results, 0, nil
	})

	runGrep(t, grepJudgeCtx(t, dir, judge), `{"pattern":"foo","include":"*.txt","output_mode":"content","intent":"find the foo helper"}`)

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("judge calls = %d, want 1", calls)
	}
	if got.Tool != "grep" || got.Intent != "find the foo helper" {
		t.Fatalf("tool/intent = %q/%q", got.Tool, got.Intent)
	}
	if got.Query["pattern"] != "foo" || got.Query["include"] != "*.txt" {
		t.Fatalf("query = %#v", got.Query)
	}
	if len(got.Results) != 2 {
		t.Fatalf("results = %d, want 2 (one per matching file)", len(got.Results))
	}
	byPath := map[string]SearchResult{}
	for _, r := range got.Results {
		byPath[r.Path] = r
	}
	a, ok := byPath["a.txt"]
	if !ok {
		t.Fatalf("missing a.txt in %#v", got.Results)
	}
	if a.Count != 1 || !strings.Contains(a.Summary, "L2:foo beta") {
		t.Fatalf("a.txt result = %+v, want count 1 with line-numbered summary", a)
	}
	b, ok := byPath["b.txt"]
	if !ok {
		t.Fatalf("missing b.txt in %#v", got.Results)
	}
	if b.Count != 2 {
		t.Fatalf("b.txt count = %d, want 2", b.Count)
	}
}

func TestGrepJudgeVetoedFileAbsentFromAllModes(t *testing.T) {
	dir := grepJudgeFixture(t)
	judge := SearchResultJudge(func(req SearchJudgeRequest) ([]SearchResult, int, error) {
		kept := make([]SearchResult, 0, len(req.Results))
		vetoed := 0
		for _, r := range req.Results {
			if r.Path == "b.txt" {
				vetoed++
				continue
			}
			kept = append(kept, r)
		}
		return kept, vetoed, nil
	})
	ctx := grepJudgeCtx(t, dir, judge)

	for _, tc := range []struct{ mode, args string }{
		{"content", `{"pattern":"foo","include":"*.txt","output_mode":"content"}`},
		{"count", `{"pattern":"foo","include":"*.txt","output_mode":"count"}`},
		{"files_with_matches", `{"pattern":"foo","include":"*.txt","output_mode":"files_with_matches"}`},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			out := runGrep(t, ctx, tc.args)
			if strings.Contains(out, "b.txt") {
				t.Fatalf("vetoed b.txt leaked into %s output:\n%s", tc.mode, out)
			}
			if !strings.HasPrefix(out, "a.txt") {
				t.Fatalf("%s output should start with the kept file:\n%s", tc.mode, out)
			}
			if !strings.Contains(out, "[relevance judge: 1 of 2 result(s) omitted as out of scope for this intent]") {
				t.Fatalf("%s output missing the veto disclosure footer:\n%s", tc.mode, out)
			}
		})
	}
}

func capFixture(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		writeFixture(t, dir, fmt.Sprintf("f%02d.txt", i), "foo\n")
	}
	return dir
}

func TestGrepJudgeCapBoundary(t *testing.T) {
	for _, n := range []int{39, 40, 41} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			dir := capFixture(t, n)
			var mu sync.Mutex
			var received int
			judge := SearchResultJudge(func(req SearchJudgeRequest) ([]SearchResult, int, error) {
				mu.Lock()
				received = len(req.Results)
				mu.Unlock()
				// Veto the first candidate so a footer is rendered.
				return req.Results[1:], 1, nil
			})

			out := runGrep(t, grepJudgeCtx(t, dir, judge), `{"pattern":"foo","include":"*.txt","output_mode":"files_with_matches"}`)

			wantJudged := n
			if wantJudged > searchJudgeMaxCandidates {
				wantJudged = searchJudgeMaxCandidates
			}
			mu.Lock()
			gotReceived := received
			mu.Unlock()
			if gotReceived != wantJudged {
				t.Fatalf("judge received %d candidates, want %d", gotReceived, wantJudged)
			}

			wantFooter := fmt.Sprintf("[relevance judge: 1 of %d result(s) omitted as out of scope for this intent]", wantJudged)
			if n > searchJudgeMaxCandidates {
				wantFooter += fmt.Sprintf("; %d beyond the judge cap were not judged", n-searchJudgeMaxCandidates)
			}
			if !strings.Contains(out, wantFooter) {
				t.Fatalf("footer missing/mismatched for n=%d:\n--- got ---\n%s\n--- want substring ---\n%s", n, out, wantFooter)
			}
			// The unjudged remainder is never dropped: every file that was not
			// vetoed still appears (f00.txt is the deliberately vetoed first
			// candidate).
			if strings.Contains(out, "f00.txt") {
				t.Fatalf("n=%d rendered the vetoed file f00.txt:\n%s", n, out)
			}
			for i := 1; i < n; i++ {
				name := fmt.Sprintf("f%02d.txt", i)
				if !strings.Contains(out, name) {
					t.Fatalf("n=%d dropped kept/unjudged file %s from output:\n%s", n, name, out)
				}
			}
		})
	}
}

func TestGrepJudgeAllVetoedMessage(t *testing.T) {
	dir := grepJudgeFixture(t)
	judge := SearchResultJudge(func(req SearchJudgeRequest) ([]SearchResult, int, error) {
		return nil, len(req.Results), nil
	})
	out := runGrep(t, grepJudgeCtx(t, dir, judge), `{"pattern":"foo","include":"*.txt","output_mode":"content"}`)

	want := "Found 2 matching file(s), but none are in scope for this intent (relevance judge omitted all 2).\n" +
		`Narrow "pattern"/"include", or re-run with an intent that matches what these files contain.`
	if out != want {
		t.Fatalf("all-vetoed message mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}
}

func TestGrepJudgeErrorFooter(t *testing.T) {
	dir := grepJudgeFixture(t)
	judge := SearchResultJudge(func(req SearchJudgeRequest) ([]SearchResult, int, error) {
		return nil, 0, fmt.Errorf("boom")
	})
	out := runGrep(t, grepJudgeCtx(t, dir, judge), `{"pattern":"foo","include":"*.txt","output_mode":"content"}`)

	if !strings.Contains(out, "b.txt:1:foo one") {
		t.Fatalf("fail-open must render every result:\n%s", out)
	}
	if !strings.Contains(out, "[relevance judge unavailable — results are unfiltered: boom]") {
		t.Fatalf("missing judge-unavailable footer:\n%s", out)
	}
}

func TestGrepIntentInSchema(t *testing.T) {
	def := GrepTool{}.Definition()
	params, ok := def["parameters"].(map[string]interface{})
	if !ok {
		t.Fatalf("no parameters: %#v", def["parameters"])
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("no properties: %#v", params["properties"])
	}
	if _, ok := props["intent"]; !ok {
		t.Fatalf("intent missing from grep properties: %#v", props)
	}
	required, ok := params["required"].([]string)
	if !ok {
		t.Fatalf("no required list: %#v", params["required"])
	}
	for _, r := range required {
		if r == "intent" {
			return
		}
	}
	t.Fatalf("intent missing from grep required list: %#v", required)
}
