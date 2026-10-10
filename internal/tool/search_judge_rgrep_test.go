package tool

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// rgrepJudgeFixture matches the golden-capture fixture: add.go (1), add_test.go
// (2), long.txt (1 long line), sub/notes.txt (2).
func rgrepJudgeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFixture(t, dir, "add.go", "package x\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n")
	writeFixture(t, dir, "add_test.go", "package x\n\nfunc TestAdd(t *testing.T) {\n\t_ = Add(1, 2)\n}\n")
	writeFixture(t, dir, "long.txt", "Add "+strings.Repeat("x", 3000)+"\n")
	writeFixture(t, dir, "sub/notes.txt", "Add one\nAdd two\n")
	return dir
}

func rgrepLongLineGolden() string {
	line := "Add " + strings.Repeat("x", 3000)
	return line[:rgMaxLineLen] + " …[line truncated]"
}

func TestRgrepJudgeAbsentGolden(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg unavailable")
	}
	dir := rgrepJudgeFixture(t)

	wantContent := strings.Join([]string{
		"add.go:3:func Add(a, b int) int {",
		"add_test.go:3:func TestAdd(t *testing.T) {",
		"add_test.go:4:\t_ = Add(1, 2)",
		"long.txt:1:" + rgrepLongLineGolden(),
		"sub/notes.txt:1:Add one",
		"sub/notes.txt:2:Add two",
	}, "\n")
	wantCount := "add.go: 1\nadd_test.go: 2\nlong.txt: 1\nsub/notes.txt: 2"
	wantFiles := "add.go\nadd_test.go\nlong.txt\nsub/notes.txt"

	for mode, want := range map[string]string{
		"content":            wantContent,
		"count":              wantCount,
		"files_with_matches": wantFiles,
	} {
		t.Run(mode, func(t *testing.T) {
			got, err := runRgrep(t, context.Background(), dir, map[string]any{"pattern": "Add", "output_mode": mode})
			if err != nil {
				t.Fatalf("rgrep: %v", err)
			}
			if got != want {
				t.Fatalf("golden mismatch:\n--- got ---\n%q\n--- want ---\n%q", got, want)
			}
		})
	}
}

func TestRgrepJudgeReceivesGroupedResults(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg unavailable")
	}
	dir := rgrepJudgeFixture(t)

	var mu sync.Mutex
	var got SearchJudgeRequest
	judge := SearchResultJudge(func(req SearchJudgeRequest) ([]SearchResult, int, error) {
		mu.Lock()
		got = req
		mu.Unlock()
		return req.Results, 0, nil
	})
	_, err := runRgrep(t,
		WithSearchResultJudge(context.Background(), judge), dir,
		map[string]any{"pattern": "Add", "output_mode": "content", "intent": "find the Add function"})
	if err != nil {
		t.Fatalf("rgrep: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if got.Tool != "rgrep" || got.Intent != "find the Add function" {
		t.Fatalf("tool/intent = %q/%q", got.Tool, got.Intent)
	}
	if got.Query["pattern"] != "Add" {
		t.Fatalf("query = %#v", got.Query)
	}
	if len(got.Results) != 4 {
		t.Fatalf("grouped results = %d, want 4 (one per file)", len(got.Results))
	}
	byPath := map[string]SearchResult{}
	for _, r := range got.Results {
		byPath[r.Path] = r
	}
	// Order is rg --sort path.
	if got.Results[0].Path != "add.go" || got.Results[1].Path != "add_test.go" {
		t.Fatalf("path order not preserved: %v", []string{got.Results[0].Path, got.Results[1].Path})
	}
	if byPath["add_test.go"].Count != 2 {
		t.Fatalf("add_test.go count = %d, want 2", byPath["add_test.go"].Count)
	}
	if !strings.Contains(byPath["add.go"].Summary, "L3:func Add") {
		t.Fatalf("add.go summary missing line-numbered match: %q", byPath["add.go"].Summary)
	}
	if !strings.Contains(byPath["long.txt"].Summary, "…[line truncated]") {
		t.Fatalf("long.txt summary must carry the line truncation: %q", byPath["long.txt"].Summary)
	}
}

func TestRgrepJudgeVetoedFileAbsentFromAllModes(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg unavailable")
	}
	dir := rgrepJudgeFixture(t)
	judge := SearchResultJudge(func(req SearchJudgeRequest) ([]SearchResult, int, error) {
		kept := make([]SearchResult, 0, len(req.Results))
		vetoed := 0
		for _, r := range req.Results {
			if r.Path == "sub/notes.txt" {
				vetoed++
				continue
			}
			kept = append(kept, r)
		}
		return kept, vetoed, nil
	})
	ctx := WithSearchResultJudge(context.Background(), judge)

	for _, mode := range []string{"content", "count", "files_with_matches"} {
		t.Run(mode, func(t *testing.T) {
			out, err := runRgrep(t, ctx, dir, map[string]any{"pattern": "Add", "output_mode": mode})
			if err != nil {
				t.Fatalf("rgrep: %v", err)
			}
			if strings.Contains(out, "sub/notes.txt") {
				t.Fatalf("vetoed file leaked into %s output:\n%s", mode, out)
			}
			if !strings.Contains(out, "[relevance judge: 1 of 4 result(s) omitted as out of scope for this intent]") {
				t.Fatalf("%s output missing the veto footer:\n%s", mode, out)
			}
		})
	}
}

func TestRgrepJudgeCapBoundaryAndFooter(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg unavailable")
	}
	dir := t.TempDir()
	n := SearchJudgeMaxCandidates + 5
	for i := 0; i < n; i++ {
		writeFixture(t, dir, fmt.Sprintf("f%02d.txt", i), "Add\n")
	}
	var mu sync.Mutex
	received := 0
	judge := SearchResultJudge(func(req SearchJudgeRequest) ([]SearchResult, int, error) {
		mu.Lock()
		received = len(req.Results)
		mu.Unlock()
		return req.Results[1:], 1, nil
	})
	out, err := runRgrep(t, WithSearchResultJudge(context.Background(), judge), dir,
		map[string]any{"pattern": "Add", "output_mode": "files_with_matches", "intent": "the Add files"})
	if err != nil {
		t.Fatalf("rgrep: %v", err)
	}
	mu.Lock()
	gotReceived := received
	mu.Unlock()
	if gotReceived != SearchJudgeMaxCandidates {
		t.Fatalf("judge received %d candidates, want %d", gotReceived, SearchJudgeMaxCandidates)
	}
	wantFooter := fmt.Sprintf("[relevance judge: 1 of %d result(s) omitted as out of scope for this intent]; %d beyond the judge cap were not judged",
		SearchJudgeMaxCandidates, n-SearchJudgeMaxCandidates)
	if !strings.Contains(out, wantFooter) {
		t.Fatalf("footer missing:\n--- want ---\n%s\n--- got ---\n%s", wantFooter, out)
	}
	if !strings.Contains(out, fmt.Sprintf("f%02d.txt", n-1)) {
		t.Fatalf("unjudged file past the cap was dropped:\n%s", out)
	}
}

func TestRgrepJudgeAllVetoedMessage(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg unavailable")
	}
	dir := rgrepJudgeFixture(t)
	judge := SearchResultJudge(func(req SearchJudgeRequest) ([]SearchResult, int, error) {
		return nil, len(req.Results), nil
	})
	out, err := runRgrep(t, WithSearchResultJudge(context.Background(), judge), dir,
		map[string]any{"pattern": "Add", "output_mode": "content", "intent": "nothing"})
	if err != nil {
		t.Fatalf("rgrep: %v", err)
	}
	want := "Found 4 matching file(s), but none are in scope for this intent (relevance judge omitted all 4).\n" +
		`Narrow "pattern"/"include", or re-run with an intent that matches what these files contain.`
	if out != want {
		t.Fatalf("all-vetoed message mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}
}

func TestRgrepJudgeErrorFooter(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg unavailable")
	}
	dir := rgrepJudgeFixture(t)
	judge := SearchResultJudge(func(req SearchJudgeRequest) ([]SearchResult, int, error) {
		return nil, 0, fmt.Errorf("boom")
	})
	out, err := runRgrep(t, WithSearchResultJudge(context.Background(), judge), dir,
		map[string]any{"pattern": "Add", "output_mode": "files_with_matches", "intent": "x"})
	if err != nil {
		t.Fatalf("rgrep: %v", err)
	}
	if !strings.HasPrefix(out, "add.go\nadd_test.go\nlong.txt\nsub/notes.txt") {
		t.Fatalf("fail-open must render every result:\n%s", out)
	}
	if !strings.Contains(out, "[relevance judge unavailable — results are unfiltered: boom]") {
		t.Fatalf("missing judge-unavailable footer:\n%s", out)
	}
}

func TestRgrepIntentInSchema(t *testing.T) {
	def := (&RgrepTool{}).Definition()
	params := def["parameters"].(map[string]interface{})
	props := params["properties"].(map[string]interface{})
	if _, ok := props["intent"]; !ok {
		t.Fatalf("intent missing from rgrep properties: %#v", props)
	}
	required := params["required"].([]string)
	found := false
	for _, r := range required {
		if r == "intent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("intent missing from rgrep required: %#v", required)
	}
}

// TestRgrepGroupResultsTruncatedTail pins the cappedBuffer behaviour the
// grouping pass must preserve: a content run cut mid-record ignores the
// incomplete trailing record, while a list-mode run keeps a partial trailing
// line exactly as the pre-refactor split did.
func TestRgrepGroupResultsTruncatedTail(t *testing.T) {
	rec := func(path, text string, line int) string {
		return fmt.Sprintf(`{"type":"match","data":{"path":{"text":%q},"lines":{"text":%q},"line_number":%d}}`, path, text, line)
	}
	t.Run("content_drops_incomplete_record", func(t *testing.T) {
		raw := rec("./a.txt", "Add one\n", 1) + "\n" + rec("./b.txt", "Add two\n", 1) + "\n" + `{"type":"match","data":{"path":{"text":"./c.txt"},"li`
		stdout := &cappedBuffer{limit: len(rec("./a.txt", "Add one\n", 1)) + 1 + len(rec("./b.txt", "Add two\n", 1)) + 1 + 20}
		if _, err := stdout.Write([]byte(raw)); err != nil {
			t.Fatal(err)
		}
		results := (&RgrepTool{}).groupResults(grepParams{OutputMode: "content"}, "/proj", stdout)
		if got := searchResultPaths2(results); strings.Join(got, ",") != "a.txt,b.txt" {
			t.Fatalf("results = %v, want a.txt,b.txt (incomplete record dropped)", got)
		}
	})
	t.Run("files_keeps_partial_trailing_line", func(t *testing.T) {
		stdout := &cappedBuffer{limit: 1 << 20}
		if _, err := stdout.Write([]byte("a.txt\nb.txt\nsub")); err != nil {
			t.Fatal(err)
		}
		results := (&RgrepTool{}).groupResults(grepParams{OutputMode: "files_with_matches"}, "/proj", stdout)
		if got := searchResultPaths2(results); strings.Join(got, ",") != "a.txt,b.txt,sub" {
			t.Fatalf("results = %v, want the partial trailing path kept as before", got)
		}
	})
}

// searchResultPaths2 is the local path extractor for rgrepResult.
func searchResultPaths2(rs []rgrepResult) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.path
	}
	return out
}
