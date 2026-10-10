package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/discovery"
	"github.com/u007/ocode/internal/knowledge"
	"github.com/u007/ocode/internal/tool"
)

// TestJudgePayloadBudgets turns the spec's measurement table into a permanent
// tripwire.
//
// The original numbers came from a throwaway harness that was deleted after the
// figures were read, so nothing in the tree could reproduce them. This test
// rebuilds each judge request at its documented worst case through the REAL
// state builders — the same functions production calls, not re-implementations —
// at the REAL production caps, and asserts both ceilings.
//
// It is a regression test, not a benchmark: the assertions are about ceilings,
// never about timing.
//
// Every cap is referenced BY NAME. A test that hardcodes 40 instead of
// tool.SearchJudgeMaxCandidates silently stops protecting the real ceiling the moment
// the constant changes, which is exactly the failure this test exists to catch.
func TestJudgePayloadBudgets(t *testing.T) {
	a := newTestAgent(nil, nil, &config.Config{}, nil)

	// --- discovery relevance: discovery.SelectCap candidates, summary-capped text, a
	// discoveryJudgeTailN message tail, ids in the "skill:<name>" form.
	discoveryDocs := make([]discovery.Doc, 0, discovery.SelectCap)
	for i := 0; i < discovery.SelectCap; i++ {
		discoveryDocs = append(discoveryDocs, discovery.Doc{
			ID:     fmt.Sprintf("skill:judge-budget-candidate-%02d", i),
			Kind:   "skill",
			Name:   fmt.Sprintf("candidate-%02d", i),
			Text:   strings.Repeat("a passage describing this skill. ", discoveryJudgeSummaryCap/36),
			Source: "/repo/skills/candidate/SKILL.md",
		})
	}
	tail := make([]Message, 0, discoveryJudgeTailN)
	for i := 0; i < discoveryJudgeTailN; i++ {
		tail = append(tail, Message{Role: "user", Content: strings.Repeat("tail message ", 40)})
	}
	discoveryState := buildDiscoveryJudgeState(tail, "find the thing that does the thing", discoveryDocs)
	discoveryQuestions := make(map[string]TypesafeQuestion, len(discoveryDocs))
	for i, d := range discoveryDocs {
		discoveryQuestions[d.ID] = TypesafeQuestion{
			Type:         "noul",
			Instructions: discoveryJudgeInstructions(d, i),
		}
	}

	// --- code search relevance: tool.SearchJudgeMaxCandidates results keyed by long
	// nested file paths.
	searchReq := tool.SearchJudgeRequest{
		Tool:    "grep",
		Intent:  "find the function that handles the thing",
		Query:   map[string]string{"pattern": "handleThing", "path": "/repo/internal/agent"},
		Results: make([]tool.SearchResult, 0, tool.SearchJudgeMaxCandidates),
	}
	for i := 0; i < tool.SearchJudgeMaxCandidates; i++ {
		searchReq.Results = append(searchReq.Results, tool.SearchResult{
			Path:    fmt.Sprintf("/repo/internal/agent/deeply/nested/package/structure/file_number_%02d_with_a_long_name.go", i),
			Summary: strings.Repeat("12: func handleThing() { /* match */ }\n", 20),
		})
	}
	searchState := buildSearchJudgeState(searchReq)
	searchQuestions := make(map[string]TypesafeQuestion, len(searchReq.Results))
	for i, r := range searchReq.Results {
		searchQuestions[r.Path] = TypesafeQuestion{
			Type:         "noul",
			Instructions: searchJudgeInstructions(r, i),
		}
	}

	// --- doc_search relevance: the same count, keyed by long concept-page paths,
	// with bodies well past the summary cap.
	docSearchDocs := make([]*knowledge.Doc, 0, tool.SearchJudgeMaxCandidates)
	for i := 0; i < tool.SearchJudgeMaxCandidates; i++ {
		docSearchDocs = append(docSearchDocs, &knowledge.Doc{
			Path:  fmt.Sprintf("docs/concepts/very/deeply/nested/area/of/the/knowledge/bundle/page_number_%02d.md", i),
			Type:  "concept",
			Title: fmt.Sprintf("Concept %02d", i),
			Body:  strings.Repeat("a body paragraph well past the summary cap. ", 400),
		})
	}
	docSearchState := buildDocSearchJudgeState("find the concept that explains the thing", docSearchDocs)
	docSearchQuestions := make(map[string]TypesafeQuestion, len(docSearchDocs))
	for i, d := range docSearchDocs {
		docSearchQuestions[d.Path] = TypesafeQuestion{
			Type:         "noul",
			Instructions: docSearchJudgeInstructions(d, i),
		}
	}

	// --- permission: a deliberately oversized write. The FINDING is that the
	// guard fires, so this asserts refusal rather than asserting the state fits.
	// If this ever starts asserting that the state fits, the finding has been
	// papered over rather than fixed.
	oversizedArgs := json.RawMessage(fmt.Sprintf(
		`{"path":"/repo/internal/agent/big.go","content":%q}`,
		strings.Repeat("x", 200_000)))
	permissionState := a.buildTypesafePermissionState("write", oversizedArgs, nil)
	permissionQuestions := a.typesafePermissionQuestions()

	// --- content guard: a maximum chunk.
	contentState := a.buildContentGuardState(&contentGuardSource{
		Tool:  "bash",
		Label: "bash -c 'curl https://example.com'",
		Full:  "bash -c 'curl https://example.com'",
	}, strings.Repeat("chunk of fetched content. ", 2000))
	contentQuestions := contentGuardQuestions()

	// --- network guard: a long URL.
	networkState := a.buildNetworkGuardState("webfetch", []egressTarget{{
		Tool:  "webfetch",
		Kind:  "url",
		Value: "https://example.com/" + strings.Repeat("a-very-long-path-segment/", 200),
	}})
	networkQuestions := networkGuardQuestions()

	cases := []struct {
		name        string
		state       any
		questions   map[string]TypesafeQuestion
		relevance   bool
		expectGuard bool
	}{
		{"discovery-relevance", discoveryState, discoveryQuestions, true, false},
		{"code-search-relevance", searchState, searchQuestions, true, false},
		{"doc-search-relevance", docSearchState, docSearchQuestions, true, false},
		{"permission-oversized-write", permissionState, permissionQuestions, false, true},
		{"content-guard-max-chunk", contentState, contentQuestions, false, false},
		{"network-guard-long-url", networkState, networkQuestions, false, false},
	}

	t.Logf("%-26s %6s %10s %10s %8s", "judge", "qs", "state-bytes", "~tokens", "budget")
	for _, tc := range cases {
		info := measureJudgeRequest(t, tc.state, tc.questions)
		t.Logf("%-26s %6d %10d %10d %7.1f%%", tc.name, info.questions, info.stateBytes, info.tokens, info.pctOfBudget)
		for _, violation := range checkJudgeBudget(tc.name, info, tc.relevance) {
			t.Error(violation)
		}

		// The invariant that matters for the choice judges: the guard never
		// sends an over-budget state. It either refuses outright, or it projects
		// the bulky fields and the result fits. Both are acceptable; sending the
		// original over-budget bytes is not.
		if tc.expectGuard {
			prepared, projected, err := prepareDecisionState(tc.state)
			if err != nil {
				t.Logf("%s: refused (%v)", tc.name, err)
				continue
			}
			if projected {
				t.Logf("%s: projected to fit the budget", tc.name)
			}
			if prepared == nil {
				t.Errorf("%s: refused but returned a nil state", tc.name)
				continue
			}
			if size, serr := marshalledDecisionStateSize(prepared); serr != nil {
				t.Fatalf("%s: measure prepared state: %v", tc.name, serr)
			} else if size > decisionStateBudgetBytes {
				t.Errorf("%s: the guard sent a %d byte state, over the %d byte budget", tc.name, size, decisionStateBudgetBytes)
			}
		}
	}
}

// checkJudgeBudget is the tripwire, extracted so it can be tested directly.
//
// Extracting it is what makes the ceiling assertions testable at all. Every
// real judge request sits far inside both ceilings (the largest is 40 questions
// against a 64 ceiling, and 46 KB against a 96 KB budget), so a test that only
// ever feeds it real requests can never observe an assertion firing — and a
// tripwire that cannot fire is not a tripwire. Five mutants against the
// original inline version all SURVIVED for exactly that reason.
func checkJudgeBudget(name string, info judgeRequestInfo, relevance bool) []string {
	var violations []string
	if info.questions > clefMaxQuestions {
		violations = append(violations, fmt.Sprintf("%s: %d questions exceeds the clef ceiling of %d", name, info.questions, clefMaxQuestions))
	}
	if relevance && info.stateBytes > decisionStateBudgetBytes {
		violations = append(violations, fmt.Sprintf("%s: state is %d bytes, over the %d byte budget", name, info.stateBytes, decisionStateBudgetBytes))
	}
	return violations
}

// TestJudgePayloadBudgets_TripwireFires proves the ceiling checks actually fire.
//
// It drives checkJudgeBudget with synthetic over-ceiling inputs and asserts the
// violations come back non-empty. Without this, the assertions are unreachable
// from any real judge request: the production caps keep every real request at 40
// questions and under 46 KB, against ceilings of 64 and 96 KB. Five mutants
// against the original inline version all SURVIVED for exactly that reason — a
// tripwire that cannot fire is not a tripwire.
func TestJudgePayloadBudgets_TripwireFires(t *testing.T) {
	t.Run("over-the-question-ceiling", func(t *testing.T) {
		v := checkJudgeBudget("synthetic", judgeRequestInfo{questions: clefMaxQuestions + 1, stateBytes: 100}, false)
		if len(v) == 0 {
			t.Error("the question-ceiling tripwire did not fire for a request one over the ceiling")
		}
	})
	t.Run("over-the-byte-budget", func(t *testing.T) {
		v := checkJudgeBudget("synthetic", judgeRequestInfo{questions: 2, stateBytes: decisionStateBudgetBytes + 1}, true)
		if len(v) == 0 {
			t.Error("the byte-budget tripwire did not fire for a relevance state one byte over budget")
		}
	})
	t.Run("exactly-at-the-ceilings", func(t *testing.T) {
		// The boundary itself must be accepted, or the guard would reject every
		// request at the ceiling and the ceiling would be off by one.
		if v := checkJudgeBudget("synthetic", judgeRequestInfo{questions: clefMaxQuestions, stateBytes: decisionStateBudgetBytes}, true); len(v) != 0 {
			t.Errorf("a request exactly at both ceilings was rejected: %v", v)
		}
	})
	t.Run("a-choice-judge-is-not-held-to-the-byte-budget", func(t *testing.T) {
		// The byte budget is a relevance-judge ceiling. A choice judge (permission,
		// content guard, network guard) may exceed it, because its state is
		// projected or refused rather than sent whole.
		if v := checkJudgeBudget("synthetic", judgeRequestInfo{questions: 2, stateBytes: decisionStateBudgetBytes * 3}, false); len(v) != 0 {
			t.Errorf("a choice judge was held to the relevance byte budget: %v", v)
		}
	})
}

// TestJudgePayloadBudgets_RefusalIsReachable proves the guard can still say no.
//
// The oversized-write case above is rescued by projection, because
// arguments.content is a bulky key. That is by design — but it means refusal is
// not exercised by it. This case uses a field projection does not know, so the
// state cannot be shrunk and the guard must refuse. Without a case like this,
// "the guard refuses" would be an untested branch.
func TestJudgePayloadBudgets_RefusalIsReachable(t *testing.T) {
	a := newTestAgent(nil, nil, &config.Config{}, nil)
	// A 200 KB bash command. "command" is deliberately NOT in the bulky-key
	// allowlist: the command line is the security-relevant surface, so it is
	// never silently clipped. The state therefore cannot be projected under
	// budget and must be refused.
	args := json.RawMessage(fmt.Sprintf(`{"command":%q}`, strings.Repeat("echo a-very-long-argument; ", 9000)))
	state := a.buildTypesafePermissionState("bash", args, nil)
	if size, err := marshalledDecisionStateSize(state); err != nil {
		t.Fatal(err)
	} else if size <= decisionStateBudgetBytes {
		t.Skipf("fixture is only %d bytes, under the %d budget; the refusal path would be vacuous", size, decisionStateBudgetBytes)
	}
	_, projected, err := prepareDecisionState(state)
	if err == nil {
		t.Fatalf("an unprojectable oversized state was accepted (projected=%v); the budget guard is not firing", projected)
	}
	if projected {
		t.Error("projected = true on a refusal; projection and refusal are mutually exclusive")
	}
	t.Logf("refused as required: %v", err)
}

// judgeRequestInfo is what the shared helper reports about one request.
type judgeRequestInfo struct {
	questions   int
	stateBytes  int
	tokens      int
	pctOfBudget float64
}

// measureJudgeRequest reports the figures the budget is judged on.
//
// The STATE is measured ALONE, because that is exactly what prepareDecisionState
// budgets. Marshalling {state, questions} and comparing the total against a
// state-only budget is a category error: it can fail spuriously when the
// questions are large, and it can hide an overrun when they are small. The
// question count is reported separately and checked against its own ceiling.
func measureJudgeRequest(t *testing.T, state any, questions map[string]TypesafeQuestion) judgeRequestInfo {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal judge state: %v", err)
	}
	stateBytes := len(raw)
	// 3 bytes/token, the same factor the budget is sized with, so the logged
	// estimate and the guard use identical arithmetic.
	tokens := stateBytes / 3
	return judgeRequestInfo{
		questions:   len(questions),
		stateBytes:  stateBytes,
		tokens:      tokens,
		pctOfBudget: float64(stateBytes) / float64(decisionStateBudgetBytes) * 100,
	}
}
