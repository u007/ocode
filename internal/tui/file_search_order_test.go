package tui

import "testing"

func pathsOf(results []fileSearchResult) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.path
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPathSegmentCount(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"main.go", 1},
		{"internal/tui/model.go", 3},
		{"./web/src/App.tsx", 3},
		{"web\\src\\App.tsx", 3},
		{"/abs/path/file.go", 3},
		{"internal//tui/model.go", 3},
	}
	for _, c := range cases {
		if got := pathSegmentCount(c.in); got != c.want {
			t.Errorf("pathSegmentCount(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestLessPathShortest(t *testing.T) {
	if !lessPathShortest("README.md", "internal/tui/model.go") {
		t.Error("expected a 1-segment path to sort before a 3-segment path")
	}
	if lessPathShortest("internal/tui/model.go", "README.md") {
		t.Error("expected longer path NOT to sort before shorter path")
	}
	// Equal segment counts fall back to lexicographic order.
	if !lessPathShortest("internal/aaa.go", "internal/zzz.go") {
		t.Error("expected equal-depth paths to sort lexicographically")
	}
}

func TestFilterFileSearchResultsEmptyQuerySortsShortestPathFirst(t *testing.T) {
	cache := []fileSearchResult{
		{path: "internal/tui/model.go", dirName: "tui", fileName: "model.go"},
		{path: "README.md", dirName: "", fileName: "README.md"},
		{path: "web/src/App.tsx", dirName: "src", fileName: "App.tsx"},
		{path: "a/b/c/deep.go", dirName: "c", fileName: "deep.go"},
	}
	got := pathsOf(filterFileSearchResults(cache, ""))
	want := []string{"README.md", "internal/tui/model.go", "web/src/App.tsx", "a/b/c/deep.go"}
	if !equalStrings(got, want) {
		t.Fatalf("empty query order = %v, want %v", got, want)
	}
	// The cached slice must not be mutated in place.
	if cache[0].path != "internal/tui/model.go" {
		t.Fatalf("cache mutated: %q", cache[0].path)
	}
}

func TestFilterFileSearchResultsTiebreakByShortestPath(t *testing.T) {
	// Both share the same filename prefix score (500_000); the shallower path
	// must win the tie.
	cache := []fileSearchResult{
		{path: "internal/tui/model.go", dirName: "tui", fileName: "model.go"},
		{path: "model.go", dirName: "", fileName: "model.go"},
	}
	got := pathsOf(filterFileSearchResults(cache, "model"))
	want := []string{"model.go", "internal/tui/model.go"}
	if !equalStrings(got, want) {
		t.Fatalf("tie order = %v, want %v", got, want)
	}
}

func TestFuzzyFilterPathsEmptyQuerySortsShortestPathFirst(t *testing.T) {
	items := []string{"internal/tui/model.go", "z.md", "a/b/c/deep.go"}
	got := fuzzyFilterPaths(items, "")
	want := []string{"z.md", "internal/tui/model.go", "a/b/c/deep.go"}
	if !equalStrings(got, want) {
		t.Fatalf("empty query order = %v, want %v", got, want)
	}
}

func TestFuzzyFilterPathsTiebreakByShortestPath(t *testing.T) {
	// "." occurs at the same index and both items share the same length, so
	// fuzzyScore ties; the 2-segment path must come first.
	items := []string{"a/b/c.go", "ab/cd.go"}
	got := fuzzyFilterPaths(items, ".")
	want := []string{"ab/cd.go", "a/b/c.go"}
	if !equalStrings(got, want) {
		t.Fatalf("tie order = %v, want %v", got, want)
	}
}
