package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"
)

// These tests pin the FAIL-OPEN contract of every decision judge structurally,
// by reading this package's own source. That is deliberate: a grep run by hand
// once is not a guarantee, and the failure this protects against is entirely
// silent. A judge that reads an absent answer as a zero value turns "the model
// did not answer" into a confidence-0 verdict, which the relevance and guard
// judges read as a veto — so an unanswered question would silently drop real
// candidates with no error anywhere.
//
// Because the checks are AST walks over the whole package rather than a list of
// known call sites, a NEW judge is covered automatically instead of having to be
// added to a hand-kept enumeration that can fall out of date.

// agentPkgFiles parses every non-test .go file in this package.
func agentPkgFiles(t *testing.T) (*token.FileSet, *ast.Package) {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	files, ok := pkgs["agent"]
	if !ok {
		t.Fatal("parser.ParseDir did not find package agent")
	}
	// Guard against a vacuous scan. If ParseDir ever resolves zero files — a
	// wrong working directory, or a filter change — every AST-based assertion
	// below would pass while inspecting nothing, which is the same silent
	// false-confidence failure as asserting on a debounced write before the
	// debounce has fired.
	if len(files.Files) == 0 {
		t.Fatal("agentPkgFiles parsed zero files; every AST assertion in this file would be vacuously green")
	}
	if len(files.Files) < 5 {
		t.Fatalf("agentPkgFiles parsed only %d files, expected the whole package", len(files.Files))
	}
	return fset, files
}

// isAnswersIndex reports whether e is an index into a field named "Answers".
func isAnswersIndex(e ast.Expr) bool {
	idx, ok := e.(*ast.IndexExpr)
	if !ok {
		return false
	}
	sel, ok := idx.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel.Name == "Answers"
}

func TestJudges_ReadAnswersWithTheTwoValueForm(t *testing.T) {
	fset, files := agentPkgFiles(t)
	var violations []string
	note := func(e ast.Expr, why string) {
		violations = append(violations, fset.Position(e.Pos()).String()+" "+why)
	}

	ast.Inspect(files, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			// The `ans, ok := resp.Answers[k]` form is legal. Anything else that
			// indexes Answers cannot tell "absent" from "present and zero".
			if len(node.Lhs) != 2 {
				for _, rhs := range node.Rhs {
					if isAnswersIndex(rhs) {
						note(rhs, "assigned without the two-value map form")
					}
				}
			}
		case *ast.IfStmt:
			// `if resp.Answers[k].Noul > 0.5 {}` never consults ok at all.
			if cond := node.Cond; cond != nil {
				if idx, ok := cond.(*ast.IndexExpr); ok && isAnswersIndex(idx) {
					note(cond, "used directly as an if condition with no ok check")
				}
			}
			assign, isAssign := node.Init.(*ast.AssignStmt)
			if !isAssign || len(assign.Lhs) == 2 {
				return true
			}
			for _, rhs := range assign.Rhs {
				if isAnswersIndex(rhs) {
					note(rhs, "if-initialised without the two-value map form")
				}
			}
		case *ast.SelectorExpr:
			// `resp.Answers[k].Noul` where the index is not the whole RHS.
			if idx, ok := node.X.(*ast.IndexExpr); ok && isAnswersIndex(idx) {
				note(node, "field read off an answer without checking ok")
			}
		}
		return true
	})

	// Prove the scan has teeth: assert it actually observed the accesses it is
	// supposed to police, so a broken matcher cannot masquerade as a clean bill.
	var seen int
	for _, f := range files.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			if a, ok := n.(*ast.AssignStmt); ok && len(a.Lhs) == 2 {
				for _, rhs := range a.Rhs {
					if isAnswersIndex(rhs) {
						seen++
					}
				}
			}
			return true
		})
	}
	if seen == 0 {
		t.Fatal("the scan found no two-value Answers reads at all; isAnswersIndex is broken and this test proves nothing")
	}
	t.Logf("scanned %d files, verified %d two-value Answers reads", len(files.Files), seen)

	if len(violations) > 0 {
		t.Errorf("judges read decision answers without the two-value map form. An unanswered question would then read as a zero-valued verdict, which every relevance and guard judge treats as a veto — a silent fail-CLOSED bug:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

func TestJudges_EverySlotHasAModelCase(t *testing.T) {
	_, files := agentPkgFiles(t)

	var declared []string
	for _, f := range files.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok || vs.Type == nil {
				return true
			}
			if ident, ok := vs.Type.(*ast.Ident); !ok || ident.Name != "judgeSlot" {
				return true
			}
			for _, name := range vs.Names {
				declared = append(declared, name.Name)
			}
			return true
		})
	}
	if len(declared) == 0 {
		t.Fatal("no judgeSlot constants found; the scan itself is broken")
	}

	covered := map[string]bool{}
	for _, f := range files.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "slotModel" {
				return true
			}
			ast.Inspect(fn, func(m ast.Node) bool {
				cc, ok := m.(*ast.CaseClause)
				if !ok {
					return true
				}
				for _, e := range cc.List {
					if id, ok := e.(*ast.Ident); ok {
						covered[id.Name] = true
					}
				}
				return true
			})
			return true
		})
	}

	var missing []string
	for _, d := range declared {
		if !covered[d] {
			missing = append(missing, d)
		}
	}
	if len(missing) > 0 {
		t.Errorf("judgeSlot constants with no slotModel case; they would silently fall through to another slot's configured model: %s (of %d declared)", strings.Join(missing, ", "), len(declared))
	}
}

func TestJudges_RelevanceFloorIsNotThePermissionFloor(t *testing.T) {
	// The permission floor is calibrated to the cost of a wrong ALLOW, so it must
	// never be reused for a lower-stakes judge. A relevance veto only hides a
	// candidate, so it legitimately runs at a much lower bar. Collapsing the two
	// onto one constant would remove the only protection a wrong allow has.
	t.Logf("permission floor default = %v, opaque = %v, relevance = %v, auto-continue = %v, network guard = %v",
		autoJudgeMinConfidenceDefault, autoJudgeOpaqueMinConfidenceDefault,
		relevanceJudgeMinConfidenceDefault, autoContinueMinConfidenceDefault,
		networkGuardMinConfidenceDefault)

	if relevanceJudgeMinConfidenceDefault == autoJudgeMinConfidenceDefault {
		t.Errorf("relevance floor (%v) equals the permission floor (%v); the permission floor is scaled to the cost of a wrong allow and must not be reused for lower-stakes judges",
			relevanceJudgeMinConfidenceDefault, autoJudgeMinConfidenceDefault)
	}
	if relevanceJudgeMinConfidenceDefault >= autoJudgeMinConfidenceDefault {
		t.Errorf("relevance floor (%v) is not below the permission floor (%v)", relevanceJudgeMinConfidenceDefault, autoJudgeMinConfidenceDefault)
	}
}

func TestJudges_DeciderStaysNarrow(t *testing.T) {
	// Widening Decider with a chat-capable method would let a decision-only
	// backend reach the chat, compaction or small-model paths — exactly what
	// "decision-only" exists to prevent.
	iface := reflect.TypeOf((*Decider)(nil)).Elem()
	if iface.NumMethod() != 4 {
		var names []string
		for i := 0; i < iface.NumMethod(); i++ {
			names = append(names, iface.Method(i).Name)
		}
		t.Errorf("Decider has %d methods %v, want exactly 4 (DecideCtx, Decide, GetProvider, GetModel)", iface.NumMethod(), names)
	}
	for _, want := range []string{"Decide", "DecideCtx", "GetProvider", "GetModel"} {
		if _, ok := iface.MethodByName(want); !ok {
			t.Errorf("Decider is missing %s", want)
		}
	}
	if _, ok := iface.MethodByName("Chat"); ok {
		t.Error("Decider exposes Chat; a decision backend must not be able to reach a chat path")
	}
}

func TestJudges_BothBackendsSatisfyTheSameContract(t *testing.T) {
	// clef is meant to be a drop-in replacement for Jev, so both clients must
	// satisfy the identical interfaces. A client that satisfied only one would
	// route to it and then fail at the call site.
	var _ Decider = (*TypesafeClient)(nil)
	var _ Decider = (*ClefClient)(nil)
	var _ LLMClient = (*TypesafeClient)(nil)
	var _ LLMClient = (*ClefClient)(nil)

	clef := newClefClient("k", "acct", "@cf/cloudflare/clef-flash")
	jev := newTypesafeClient("k", "typesafe/jev-latest", "https://example.invalid")

	// The wire contract apart from the model selector must be identical: same
	// state, same questions, same JSON framing.
	q := map[string]TypesafeQuestion{
		"perm.1": {Type: "choice", Instructions: "allow?", Criteria: map[string]string{"yes": "fine", "no": "risky"}},
	}
	clefBody, err := clef.buildBody(map[string]any{"tool": "bash"}, q)
	if err != nil {
		t.Fatalf("clef buildBody: %v", err)
	}
	jevBody, err := jev.marshalRequestBody(map[string]any{"tool": "bash"}, q)
	if err != nil {
		t.Fatalf("typesafe marshalRequestBody: %v", err)
	}
	stripModel := func(b []byte) string {
		s := string(b)
		i := strings.Index(s, `"model":`)
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], ",")
		if j < 0 {
			return s
		}
		return s[:i] + s[i+j:]
	}
	if got, want := stripModel(clefBody), stripModel(jevBody); got != want {
		t.Errorf("clef and Jev disagree on the request contract apart from the model selector.\n clef: %s\n  jev: %s", got, want)
	}
}

func TestJudges_BothBackendsRejectChatLoudly(t *testing.T) {
	// A decision backend selected for a chat role must fail, not return empty
	// content that a caller would treat as a real (blank) reply.
	if _, err := (&ClefClient{}).Chat(nil, nil); err == nil {
		t.Error("ClefClient.Chat returned nil error")
	}
	if _, err := (&TypesafeClient{}).Chat(nil, nil); err == nil {
		t.Error("TypesafeClient.Chat returned nil error")
	}
}
