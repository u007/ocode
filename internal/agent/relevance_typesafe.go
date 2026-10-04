package agent

import (
	"context"
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
// (autoJudgeMinConfidenceDefault = 0.80): a relevance veto only hides a
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
// candidate in a single Decide call and returns (a) the set of candidate ids
// whose answer meets the lenient relevance floor and (b) the raw noul score for
// every candidate that got a real noul answer. The caller builds the state and
// per-candidate instructions; this function owns the shared mechanics: the
// Decide call, side-usage recording, the confidence floor, per-candidate
// fail-open handling, and the debug lines.
//
// The score map is a SECOND output rather than a change to the keep contract, so
// the three judges keep identical veto semantics. It exists because a boolean
// cannot express "how relevant": the discovery auto-inject gate
// (discovery_autoinject.go) needs a much higher bar than relevance and must not
// re-derive the number from the bool. A vetoed candidate still carries its real
// score, which is what lets the caller log how close a turn came to firing. An
// id is ABSENT from the map when the answer was missing or not a noul (fail-open,
// kept) — callers must treat "absent" as "unknown", never as 0.
//
// ctx bounds the round trip. A caller that supplies its own deadline gets that
// budget (the code-search judge passes searchJudgeTimeout); a caller that passes
// context.Background() keeps DecideCtx's typesafeRequestTimeout fallback, which
// is what the discovery and doc_search judges rely on.
//
// Fail-open contract (mirrors the pre-existing discovery judge): a
// transport/decode error returns (nil, err) so the caller keeps every
// candidate; a missing or non-noul answer keeps that candidate; only a real
// below-floor noul vetoes. The judge can therefore only ever veto.
func (a *Agent) judgeRelevanceQuestions(ctx context.Context, client Decider, debugKind, logTag string, candidateIDs []string, state any, questions map[string]TypesafeQuestion) (map[string]bool, map[string]float64, error) {
	if len(candidateIDs) == 0 {
		return map[string]bool{}, map[string]float64{}, nil
	}
	resp, err := client.DecideCtx(ctx, state, questions)
	if err != nil {
		return nil, nil, err
	}
	a.RecordSideUsage(resp.Usage.InputTokens, resp.Usage.OutputTokens, 0, 0, deciderLabel(client))

	min := a.resolveRelevanceJudgeMinConfidence()
	model := deciderLabel(client)
	keep := make(map[string]bool, len(candidateIDs))
	scores := make(map[string]float64, len(candidateIDs))
	for _, id := range candidateIDs {
		ans, ok := resp.Answers[id]
		if !ok {
			a.emitDebug(debugKind, fmt.Sprintf("%s id=%s verdict=keep (missing answer; fail-open)", logTag, id))
			keep[id] = true
			continue
		}
		// validateAnswer replaces the old bare type check. It also catches the
		// cases a type check cannot: an out-of-range noul, an unnormalised
		// probability set, a choice that was never offered.
		//
		// A REJECTED ANSWER MUST KEEP THE CANDIDATE. The zero value of Noul is 0,
		// which reads as "definitely irrelevant" and vetoes — so falling through
		// to the score comparison below would be a fail-CLOSED bug hiding inside a
		// system whose contract is fail-open.
		if err := validateAnswer(questions[id], ans); err != nil {
			a.emitDebug(debugKind, fmt.Sprintf("%s id=%s verdict=keep (untrustworthy answer: %v; fail-open)", logTag, id, err))
			keep[id] = true
			continue
		}
		// Recorded for BOTH verdicts: a vetoed candidate's real score is what a
		// caller needs to see how far below the floor it landed.
		scores[id] = ans.Noul
		if ans.Noul >= min {
			a.emitDebug(debugKind, fmt.Sprintf("%s id=%s noul=%.3f verdict=keep", logTag, id, ans.Noul))
			keep[id] = true
		} else {
			a.emitDebug(debugKind, fmt.Sprintf("%s id=%s noul=%.3f verdict=veto", logTag, id, ans.Noul))
		}
	}
	a.emitDebug(debugKind, fmt.Sprintf("%s kept=%d vetoed=%d min=%.2f model=%s", logTag, len(keep), len(candidateIDs)-len(keep), min, model))
	return keep, scores, nil
}
