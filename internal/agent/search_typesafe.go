package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// searchJudgeSummaryCap bounds one candidate's summary in the code-search judge
// state. Code lines are long and the question is a yes/no relevance call, so
// this is much smaller than docSearchJudgeSummaryCap (1000): the judge needs
// enough to see where the file matched, not the file itself.
const searchJudgeSummaryCap = 400

// searchJudgeTimeout bounds one code-search relevance round trip. Jev answers in
// roughly 0.8s; 4s is ~5x the median. Timing out early only costs an unfiltered
// result, so a tight budget is nearly free — and it keeps a provider that
// accepts the connection and then stalls from adding the 30s
// typesafeRequestTimeout to every search in the session (D4 judges on every
// search).
//
// It is a var rather than a const so tests can shrink it (mirroring
// maxGrepFileBytes in internal/tool); production never mutates it.
var searchJudgeTimeout = 4 * time.Second

// searchJudgeQueryFields is the only set of query keys forwarded to the judge.
// The judge request is a network round trip to a third-party model, so an
// explicit whitelist keeps unrelated (potentially secret-bearing) tool args out
// of it even if a caller ever adds one to the SearchJudgeRequest.Query map.
func searchJudgeQueryFields() []string { return []string{"pattern", "path", "include"} }

// searchJudgeSummary caps one candidate's summary.
func searchJudgeSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if len(summary) > searchJudgeSummaryCap {
		return summary[:searchJudgeSummaryCap] + "…(truncated)"
	}
	return summary
}

// buildSearchJudgeState assembles the structured judge request for one
// code-search call: the caller's intent, the invoking tool and its non-secret
// query scalars, plus one entry per candidate file. Pure so the state shape is
// unit-testable (spec §6).
func buildSearchJudgeState(req tool.SearchJudgeRequest) map[string]any {
	query := make(map[string]string, 3)
	for _, k := range searchJudgeQueryFields() {
		if v, ok := req.Query[k]; ok && v != "" {
			query[k] = v
		}
	}
	cands := make([]map[string]any, 0, len(req.Results))
	for _, r := range req.Results {
		entry := map[string]any{
			"id":    r.Path,
			"path":  r.Path,
			"count": r.Count,
		}
		if s := searchJudgeSummary(r.Summary); s != "" {
			entry["summary"] = s
		}
		cands = append(cands, entry)
	}
	return map[string]any{
		"request":    req.Intent,
		"tool":       req.Tool,
		"query":      query,
		"candidates": cands,
	}
}

// searchJudgeInstructions builds the per-file noul question. The candidate is
// referenced by its backticked state path (`candidates[i]`) and by its path, so
// the judge reads the same structured state the code built — the same
// convention as docSearchJudgeInstructions.
//
// The rubric is the lenient shared relevance policy (slight relevance is a yes;
// only a different area of the codebase is a no) plus two guards the doc rubric
// does not need: do not veto a file merely for being a test/fixture/generated
// artifact (those are frequently the target), and do not veto on a substring
// match that is not what the request is about (the `\bTest\b` collision).
func searchJudgeInstructions(r tool.SearchResult, idx int) string {
	return fmt.Sprintf(`You are an AI coding agent. The state holds the current search request (request), the tool that produced the results (tool), and a numbered list of candidate files (candidates) that a keyword search of the codebase returned.
Decide whether candidate `+"`candidates[%d]`"+` — the file `+"`%s`"+` — is in the same scope as the search request.
Answer yes when the file is even slightly relevant: it touches the same subject, component, behaviour, decision, or concept as the request.
Answer no only when it is of a different scope — an unrelated area of the codebase that merely happens to share a word, a prefix, or a substring with the request.
Do not answer no merely because the file is a test, a fixture, or a generated artifact: those are frequently the target of the search.
Do not answer no on a substring match that is not what the request is about, and do not answer no for boilerplate that matched incidentally.`, idx, r.Path)
}

// judgeSearchResults asks one noul question per candidate file in a single
// Decide call and returns the in-scope subset (in pre-judge order), the number
// vetoed, and any error. It delegates to judgeRelevanceQuestions, so the
// lenient 0.5 floor, per-candidate fail-open, side-usage accounting and debug
// lines are inherited rather than reimplemented. ctx carries the short
// searchJudgeTimeout budget.
//
// On a non-nil error the caller must render every result unfiltered: a judge
// failure may never make a search return fewer results than it does today.
func (a *Agent) judgeSearchResults(ctx context.Context, client *TypesafeClient, req tool.SearchJudgeRequest) ([]tool.SearchResult, int, error) {
	if len(req.Results) == 0 {
		return req.Results, 0, nil
	}
	state := buildSearchJudgeState(req)
	ids := make([]string, 0, len(req.Results))
	questions := make(map[string]TypesafeQuestion, len(req.Results))
	for i, r := range req.Results {
		ids = append(ids, r.Path)
		questions[r.Path] = TypesafeQuestion{
			Type:         "noul",
			Instructions: searchJudgeInstructions(r, i),
		}
	}
	if len(ids) == 0 {
		return req.Results, 0, nil
	}

	keepSet, err := a.judgeRelevanceQuestions(ctx, client, "TOOL", "search_typesafe", ids, state, questions)
	if err != nil {
		return nil, 0, err
	}
	kept := make([]tool.SearchResult, 0, len(req.Results))
	vetoed := 0
	for _, r := range req.Results {
		if keepSet[r.Path] {
			kept = append(kept, r)
		} else {
			vetoed++
		}
	}
	return kept, vetoed, nil
}

// searchResultJudge returns the callback the code-search tools (grep, rgrep,
// glob) use to vet their results, or nil when TypeSafe is not connected.
// "Connected" is the shared discoveryJudgeClient check (a keyed TypesafeClient
// from the factory), exactly as for the doc_search judge; a nil judge means the
// tool renders everything exactly as it does today.
//
// The callback never hides a result because of a failure: an empty intent skips
// the judge entirely (and logs intent-missing), and a judge error returns every
// result together with the error so the tool can render an unfiltered footer.
// It is attached per tool call on the execution context (see
// tool.WithSearchResultJudge), so a sub-agent or transient advisor judges with
// its own agent.
func (a *Agent) searchResultJudge() tool.SearchResultJudge {
	client := a.discoveryJudgeClient()
	if client == nil {
		return nil
	}
	return func(req tool.SearchJudgeRequest) ([]tool.SearchResult, int, error) {
		if strings.TrimSpace(req.Intent) == "" {
			a.emitDebug("TOOL", fmt.Sprintf("search_typesafe tool=%s intent-missing (intent empty; judge skipped, %d result(s) unfiltered)", req.Tool, len(req.Results)))
			return req.Results, 0, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), searchJudgeTimeout)
		defer cancel()
		kept, vetoed, err := a.judgeSearchResults(ctx, client, req)
		if err != nil {
			a.emitDebug("TOOL", fmt.Sprintf("search_typesafe tool=%s judge=%s failed (fail-open, all %d result(s) kept): %v", req.Tool, client.Model, len(req.Results), err))
			return req.Results, 0, err
		}
		return kept, vetoed, nil
	}
}
