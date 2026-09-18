package agent

import (
	"fmt"
)

// relevanceJudgeMinConfidenceDefault is the shared TypeSafe confidence floor for
// the lenient RELEVANCE judges: the discovery candidate judge
// (discovery_typesafe.go) and the doc_search result judge
// (doc_search_typesafe.go). Both answer the product question "is this retrieved
// item in the same scope as the request?", where the rule is "even slight
// relevancy should be presented; only a different scope is skipped".
//
// It is deliberately far below the high-stakes permission floor
// (autoJudgeMinConfidenceDefault = 0.85): a relevance veto only hides a
// retrieval result, it never grants a tool call. 0.5 is TypeSafe's documented
// "genuinely unsure / do not act" boundary, so anything Jev judges more likely
// relevant than not is kept.
const relevanceJudgeMinConfidenceDefault = 0.5

// resolveRelevanceJudgeMinConfidence returns the confidence floor for the
// relevance judges. Per the risk-scaling rule it is intentionally decoupled from
// permissions.auto.min_confidence — that key remains the high-stakes
// permission/interpreter floor, and overloading it here would let a strict
// permission tuning silently suppress slightly-relevant retrieval results.
func (a *Agent) resolveRelevanceJudgeMinConfidence() float64 {
	return relevanceJudgeMinConfidenceDefault
}

// judgeRelevanceQuestions sends one noul (yes-probability) question per
// candidate in a single Decide call and returns the set of candidate ids whose
// answer meets the lenient relevance floor. The caller builds the state and
// per-candidate instructions; this function owns the shared mechanics: the
// Decide call, side-usage recording, the confidence floor, per-candidate
// fail-open handling, and the debug lines.
//
// Fail-open contract (mirrors the pre-existing discovery judge): a
// transport/decode error returns (nil, err) so the caller keeps every
// candidate; a missing or non-noul answer keeps that candidate; only a real
// below-floor noul vetoes. The judge can therefore only ever veto.
func (a *Agent) judgeRelevanceQuestions(client *TypesafeClient, debugKind, logTag string, candidateIDs []string, state any, questions map[string]TypesafeQuestion) (map[string]bool, error) {
	if len(candidateIDs) == 0 {
		return map[string]bool{}, nil
	}
	resp, err := client.Decide(state, questions)
	if err != nil {
		return nil, err
	}
	a.RecordSideUsage(resp.Usage.InputTokens, resp.Usage.OutputTokens, 0, 0, "typesafe/"+client.Model)

	min := a.resolveRelevanceJudgeMinConfidence()
	model := "typesafe/" + client.Model
	keep := make(map[string]bool, len(candidateIDs))
	for _, id := range candidateIDs {
		ans, ok := resp.Answers[id]
		if !ok || ans.Type != "noul" {
			a.emitDebug(debugKind, fmt.Sprintf("%s id=%s verdict=keep (missing noul answer; fail-open)", logTag, id))
			keep[id] = true
			continue
		}
		if ans.Noul >= min {
			a.emitDebug(debugKind, fmt.Sprintf("%s id=%s noul=%.3f verdict=keep", logTag, id, ans.Noul))
			keep[id] = true
		} else {
			a.emitDebug(debugKind, fmt.Sprintf("%s id=%s noul=%.3f verdict=veto", logTag, id, ans.Noul))
		}
	}
	a.emitDebug(debugKind, fmt.Sprintf("%s kept=%d vetoed=%d min=%.2f model=%s", logTag, len(keep), len(candidateIDs)-len(keep), min, model))
	return keep, nil
}
