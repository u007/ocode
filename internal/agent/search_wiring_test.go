package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// noulReplyServer answers every judge question in the request body with the
// configured noul, so one agent can be made to veto everything (0.0) and
// another to keep everything (1.0).
func noulReplyServer(t *testing.T, noul float64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Questions map[string]any `json:"questions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		answers := make(map[string]any, len(body.Questions))
		for id := range body.Questions {
			answers[id] = map[string]any{"type": "noul", "noul": noul}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "jev-latest",
			"answers": answers,
			"usage":   map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// judgeCaptureTool is named "grep" so the agent dispatch attaches a judge to
// its context. It records what the attached judge decided for a fixed result
// set, which distinguishes one agent's judge from another's.
type judgeCaptureTool struct {
	verdicts []string
}

func (p *judgeCaptureTool) Name() string        { return "grep" }
func (p *judgeCaptureTool) Description() string { return "capture" }
func (p *judgeCaptureTool) Parallel() bool      { return false }
func (p *judgeCaptureTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": "grep"}
}
func (p *judgeCaptureTool) Execute(json.RawMessage) (string, error) { return "", nil }
func (p *judgeCaptureTool) ExecuteCtx(ctx context.Context, _ json.RawMessage) (string, error) {
	judge := tool.SearchJudgeFromContext(ctx)
	if judge == nil {
		p.verdicts = append(p.verdicts, "nil")
		return "", nil
	}
	req := tool.SearchJudgeRequest{
		Tool:   "grep",
		Intent: "the fixed intent",
		Results: []tool.SearchResult{
			{Path: "a.go", Summary: "L1:x", Count: 1},
			{Path: "b.go", Summary: "L1:x", Count: 1},
			{Path: "c.go", Summary: "L1:x", Count: 1},
		},
	}
	kept, vetoed, err := judge(req)
	p.verdicts = append(p.verdicts, fmt.Sprintf("kept=%d vetoed=%d err=%v", len(kept), vetoed, err != nil))
	return "", nil
}

func dispatchGrep(t *testing.T, a *Agent) {
	t.Helper()
	a.toolBatchDelay = 0
	if _, err := a.executeToolCallWithContext(context.Background(), "grep", json.RawMessage(`{}`), nil, ""); err != nil {
		t.Fatalf("dispatch grep: %v", err)
	}
}

// TestSearchJudgeIsPerAgentOnSharedTools pins the seam: because the judge rides
// the execution context, a sub-agent and a transient advisor that are handed the
// parent's tool objects each judge with their OWN agent, and a child coming and
// going leaves the parent's judge intact. A judge stored on the tool struct
// would have been clobbered here (the defect that ruled that design out).
func TestSearchJudgeIsPerAgentOnSharedTools(t *testing.T) {
	prev := newClientFn
	t.Cleanup(func() { newClientFn = prev })

	srvVeto := noulReplyServer(t, 0.0) // below the 0.5 floor -> veto all
	srvKeep := noulReplyServer(t, 1.0) // keep all

	cfgParent, cfgChild, cfgAdvisor, cfgOff := &config.Config{}, &config.Config{}, &config.Config{}, &config.Config{}
	newClientFn = func(cfg *config.Config, _ string) LLMClient {
		switch cfg {
		case cfgParent:
			return newTypesafeClient("k", "parent", srvVeto.URL)
		case cfgChild:
			return newTypesafeClient("k", "child", srvKeep.URL)
		case cfgAdvisor:
			return newTypesafeClient("k", "advisor", srvVeto.URL)
		}
		return nil // cfgOff: not connected
	}

	parent := NewAgent(nil, nil, cfgParent, nil)
	child := NewAgent(nil, nil, cfgChild, nil)
	advisor := NewAgent(nil, nil, cfgAdvisor, nil)
	off := NewAgent(nil, nil, cfgOff, nil)

	probe := &judgeCaptureTool{}
	parent.tools["grep"] = probe
	// Sub-agents and the transient advisor are handed the parent's tool objects.
	child.tools = parent.tools
	advisor.tools = parent.tools
	off.tools = parent.tools

	dispatchGrep(t, parent)  // parent's own judge (veto-all)
	dispatchGrep(t, child)   // child's judge (keep-all)
	dispatchGrep(t, advisor) // transient advisor's judge (veto-all)
	dispatchGrep(t, off)     // disconnected child attaches nothing
	dispatchGrep(t, parent)  // parent again: must still be the parent's judge

	want := []string{"kept=0 vetoed=3 err=false", "kept=3 vetoed=0 err=false", "kept=0 vetoed=3 err=false", "nil", "kept=0 vetoed=3 err=false"}
	if strings.Join(probe.verdicts, " | ") != strings.Join(want, " | ") {
		t.Fatalf("per-agent judging broken:\n got %v\nwant %v", probe.verdicts, want)
	}
}

// TestSearchToolsOnlyInvokedThroughAgentDispatch guards the coverage claim in
// the design: every grep/rgrep/glob invocation in production code goes through
// the agent dispatch chain, so no caller can silently bypass filtering. It parses
// non-test Go outside internal/tool and internal/agent and fails on a direct
// Execute* call on (or type assertion to) one of the three concrete tool types.
func TestSearchToolsOnlyInvokedThroughAgentDispatch(t *testing.T) {
	root := moduleRoot(t)
	searchTypes := map[string]bool{"GrepTool": true, "GlobTool": true, "RgrepTool": true}
	execNames := map[string]bool{"Execute": true, "ExecuteCtx": true, "ExecuteStream": true, "ExecuteStreamCtx": true}

	var offenders []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".worktrees", ".superpowers", "web", "vendor", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "internal/tool/") || strings.HasPrefix(rel, "internal/agent/") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil
		}
		vars := map[string]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for i, rhs := range x.Rhs {
					if i < len(x.Lhs) && isSearchToolValue(rhs, searchTypes) {
						if id, ok := x.Lhs[i].(*ast.Ident); ok {
							vars[id.Name] = true
						}
					}
				}
			case *ast.ValueSpec:
				for i, v := range x.Values {
					if i < len(x.Names) && isSearchToolValue(v, searchTypes) {
						vars[x.Names[i].Name] = true
					}
				}
			}
			return true
		})
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.TypeAssertExpr:
				if isSearchToolValue(x.Type, searchTypes) {
					offenders = append(offenders, fmt.Sprintf("%s:%d type assertion to a search tool", rel, fset.Position(x.Pos()).Line))
				}
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok || !execNames[sel.Sel.Name] {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok && vars[id.Name] {
					offenders = append(offenders, fmt.Sprintf("%s:%d %s.%s", rel, fset.Position(x.Pos()).Line, id.Name, sel.Sel.Name))
				} else if isSearchToolValue(sel.X, searchTypes) {
					offenders = append(offenders, fmt.Sprintf("%s:%d direct %s call", rel, fset.Position(x.Pos()).Line, sel.Sel.Name))
				}
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}
	if len(offenders) > 0 {
		t.Fatalf("search tools must only be invoked through the agent dispatch chain (a direct Execute would bypass the relevance judge):\n%s", strings.Join(offenders, "\n"))
	}
}

// isSearchToolValue reports whether expr is a (possibly addressed) composite
// literal or an identifier/selector naming one of the searched tool types.
func isSearchToolValue(expr ast.Expr, types map[string]bool) bool {
	switch x := expr.(type) {
	case *ast.UnaryExpr:
		return isSearchToolValue(x.X, types)
	case *ast.ParenExpr:
		return isSearchToolValue(x.X, types)
	case *ast.CompositeLit:
		return isSearchToolTypeName(x.Type, types)
	}
	return false
}

func isSearchToolTypeName(expr ast.Expr, types map[string]bool) bool {
	switch x := expr.(type) {
	case *ast.Ident:
		return types[x.Name]
	case *ast.SelectorExpr:
		return types[x.Sel.Name]
	case *ast.StarExpr:
		return isSearchToolTypeName(x.X, types)
	}
	return false
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate module root (go.mod)")
		}
		dir = parent
	}
}

// TestDocPromptDirectiveRequiresSearchIntent pins the prompt reinforcement: the
// directive line that names grep/glob must also tell the model to pass the
// required intent, so the requirement is stated outside the tool schema too.
func TestDocPromptDirectiveRequiresSearchIntent(t *testing.T) {
	if !strings.Contains(docPromptContent, "grep") {
		t.Fatal("docPromptContent no longer names grep; the intent directive is moot")
	}
	if !strings.Contains(docPromptContent, "must pass the required intent") {
		t.Fatal("the grep/glob directive must instruct the model to pass the required intent on code-search calls")
	}
}
