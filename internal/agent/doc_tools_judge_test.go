package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/knowledge"
)

// newMultiDocSearchStore builds a bundle with three conforming docs that all
// match the query "zebrafish", each carrying a unique body token. Search ties
// are broken by path ascending, so the rank order is alpha, beta, gamma.
func newMultiDocSearchStore(t *testing.T) *knowledge.Store {
	t.Helper()
	td := t.TempDir()
	docsDir := filepath.Join(td, "docs")
	if err := os.MkdirAll(filepath.Join(docsDir, "guide"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "index.md"), []byte("---\nokf_version: \"0.1\"\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	write := func(name, title, token string) {
		content := fmt.Sprintf("---\ntype: concept\ntitle: %s\ndescription: about %s\n---\nzebrafish %s\n", title, name, token)
		if err := os.WriteFile(filepath.Join(docsDir, "guide", name+".md"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("alpha", "Alpha", "alphabodytoken")
	write("beta", "Beta", "betabodytoken")
	write("gamma", "Gamma", "gammabodytoken")

	b, ok := knowledge.DetectBundle(td)
	if !ok {
		t.Fatal("DetectBundle returned false for test bundle")
	}
	return knowledge.NewStore(b)
}

// judgeReturning builds a DocSearchJudge that keeps docs whose path is in keep,
// preserving the input order, or returns err.
func judgeReturning(keep []string, err error) DocSearchJudge {
	return func(_ string, docs []*knowledge.Doc) ([]*knowledge.Doc, error) {
		if err != nil {
			return nil, err
		}
		want := make(map[string]bool, len(keep))
		for _, p := range keep {
			want[p] = true
		}
		out := make([]*knowledge.Doc, 0, len(docs))
		for _, d := range docs {
			if want[d.Path] {
				out = append(out, d)
			}
		}
		return out, nil
	}
}

func TestDocSearchToolJudgeHidesOutOfScope(t *testing.T) {
	store := newMultiDocSearchStore(t)
	tool := &DocSearchTool{store: store, judge: judgeReturning([]string{"guide/alpha.md", "guide/gamma.md"}, nil)}

	out, err := tool.Execute(json.RawMessage(`{"query":"zebrafish"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(out, "guide/alpha.md") || !strings.Contains(out, "guide/gamma.md") {
		t.Fatalf("kept docs missing from output:\n%s", out)
	}
	if strings.Contains(out, "guide/beta.md") {
		t.Fatalf("out-of-scope doc leaked into output:\n%s", out)
	}
	if !strings.Contains(out, "1 out-of-scope result(s) omitted by the relevance judge") {
		t.Fatalf("output must note the omission:\n%s", out)
	}
}

func TestDocSearchToolNoJudgeKeepsAll(t *testing.T) {
	store := newMultiDocSearchStore(t)
	tool := &DocSearchTool{store: store} // nil judge

	out, err := tool.Execute(json.RawMessage(`{"query":"zebrafish"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for _, p := range []string{"guide/alpha.md", "guide/beta.md", "guide/gamma.md"} {
		if !strings.Contains(out, p) {
			t.Fatalf("doc %s missing without a judge:\n%s", p, out)
		}
	}
	if strings.Contains(out, "omitted by the relevance judge") {
		t.Fatalf("no judge must mean no omission note:\n%s", out)
	}
}

func TestDocSearchToolJudgeErrorKeepsAll(t *testing.T) {
	store := newMultiDocSearchStore(t)
	tool := &DocSearchTool{store: store, judge: judgeReturning(nil, fmt.Errorf("judge boom"))}

	out, err := tool.Execute(json.RawMessage(`{"query":"zebrafish"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for _, p := range []string{"guide/alpha.md", "guide/beta.md", "guide/gamma.md"} {
		if !strings.Contains(out, p) {
			t.Fatalf("doc %s missing after judge error (must fail open):\n%s", p, out)
		}
	}
	if strings.Contains(out, "omitted by the relevance judge") {
		t.Fatalf("a judge error must not claim omissions:\n%s", out)
	}
}

func TestDocSearchToolJudgeAllVetoed(t *testing.T) {
	store := newMultiDocSearchStore(t)
	tool := &DocSearchTool{store: store, judge: judgeReturning(nil, nil)}

	out, err := tool.Execute(json.RawMessage(`{"query":"zebrafish"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(out, "none are in scope") {
		t.Fatalf("all-vetoed output should say nothing is in scope:\n%s", out)
	}
	if strings.Contains(out, "alphabodytoken") || strings.Contains(out, "guide/alpha.md") {
		t.Fatalf("vetoed docs must not appear:\n%s", out)
	}
}

func TestDocSearchToolJudgeRunsBeforeGetTop(t *testing.T) {
	store := newMultiDocSearchStore(t)
	// The top-ranked doc (alpha) is out of scope; the next kept doc must be the
	// one whose body gets inlined by get_top=1.
	tool := &DocSearchTool{store: store, judge: judgeReturning([]string{"guide/beta.md", "guide/gamma.md"}, nil)}

	out, err := tool.Execute(json.RawMessage(`{"query":"zebrafish","get_top":1}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(out, "betabodytoken") {
		t.Fatalf("get_top should inline the top *kept* doc's body:\n%s", out)
	}
	if strings.Contains(out, "alphabodytoken") {
		t.Fatalf("vetoed top doc's body leaked via get_top:\n%s", out)
	}
}
