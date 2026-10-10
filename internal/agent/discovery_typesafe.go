package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/discovery"
)

// discoveryJudgeSummaryCap bounds one candidate's summary in the judge state so
// a large project-doc summary cannot blow up the request.
const discoveryJudgeSummaryCap = 1000

// discoveryJudgeTailN / discoveryJudgeTailCap mirror the auto-continue judge's
// transcript budget: at most the last N messages, each capped, so one huge
// tool result cannot dominate the request.
const (
	discoveryJudgeTailN   = 6
	discoveryJudgeTailCap = 4000
)

// discoveryJudgeClient resolves the discovery relevance judge's decision
// client, or nil when no backend is connected for this slot. It is the sole
// "provider connected" check shared by the discovery, doc_search and code_search
// relevance judges: the slot must resolve to a Decider with usable credentials.
// A nil config, a client that is not a decision backend, or a keyless client all
// mean "no judge", and the caller keeps today's keep-everything behavior.
//
// The result is cached per discoveryState and keyed on the slot's model id (see
// discoveryState.judge). Keying on the model rather than using a one-shot latch
// is what lets a mid-session /connect or judge_model change take effect. A nil
// client is NOT cached, for the same reason: "no credential yet" must become live
// without needing a discovery reset.
func (a *Agent) discoveryJudgeClient() Decider {
	if a.disco == nil {
		return a.resolveDecider(slotDiscovery)
	}
	// Read the model ONCE. Reading it twice lets a config reload between the reads
	// return two different values and cache a client under the wrong key.
	model := a.slotModel(slotDiscovery)

	// Fast path for a cached client on the same model, so a credentialed session
	// does not re-run the client factory on every turn and status read. This is
	// NOT what keeps the debug log quiet for a keyless user — nothing is cached
	// in that case, and NewClient dedupes its own "no API key" line per
	// (provider, model), so the log stays quiet without pinning a nil.
	a.disco.judgeMu.Lock()
	cached, cachedModel := a.disco.judge, a.disco.judgeModel
	a.disco.judgeMu.Unlock()
	if cached != nil && cachedModel == model {
		return cached
	}

	client := a.resolveDecider(slotDiscovery)

	a.disco.judgeMu.Lock()
	defer a.disco.judgeMu.Unlock()
	a.disco.judgeModel = model
	a.disco.judge = client
	return client
}

// judgeDiscoveryCandidates asks one noul (yes-probability) question per
// candidate in a single Decide call and returns the subset judged relevant plus
// the raw per-candidate noul scores. It is pure with respect to the sticky
// session: the caller seeds exactly what is returned.
//
// The floor is the lenient shared relevance floor
// (relevanceJudgeMinConfidenceDefault = 0.5), not the high-stakes permission
// floor: this judge only decides whether a candidate is in scope, and the
// product rule is "even slight relevancy should be presented; only a different
// scope is skipped". Fail-open is handled by judgeRelevanceQuestions — a
// transport/decode error returns (nil, nil, err) and the caller seeds every
// candidate; a missing or non-noul answer keeps that candidate.
//
// The scores are the ONLY channel by which the auto-inject gate learns how
// relevant Jev thought a skill was (see discovery_autoinject.go). They are
// returned, never consulted here: a vetoed candidate still carries its real
// score, and this judge's keep contract is unchanged.
func (a *Agent) judgeDiscoveryCandidates(client Decider, tail []Message, query string, candidates []discovery.Doc) ([]discovery.Doc, map[string]float64, error) {
	if len(candidates) == 0 {
		return nil, nil, nil
	}
	state := buildDiscoveryJudgeState(tail, query, candidates)
	ids := make([]string, len(candidates))
	questions := make(map[string]TypesafeQuestion, len(candidates))
	for i, d := range candidates {
		ids[i] = d.ID
		questions[d.ID] = TypesafeQuestion{
			Type:         "noul",
			Instructions: discoveryJudgeInstructions(d, i),
		}
	}

	keepSet, scores, err := a.judgeRelevanceQuestions(context.Background(), client, "DISCOVERY", "discovery_typesafe", ids, state, questions)
	if err != nil {
		return nil, nil, err
	}
	keep := make([]discovery.Doc, 0, len(candidates))
	for _, d := range candidates {
		if keepSet[d.ID] {
			keep = append(keep, d)
		}
	}
	return keep, scores, nil
}

// buildDiscoveryJudgeState assembles the structured judge request: the current
// request text, a bounded transcript tail, and one entry per candidate
// (id/kind/name/summary). Pure so the state shape is unit-testable.
func buildDiscoveryJudgeState(tail []Message, query string, candidates []discovery.Doc) map[string]any {
	if len(tail) > discoveryJudgeTailN {
		tail = tail[len(tail)-discoveryJudgeTailN:]
	}
	entries := make([]map[string]any, 0, len(tail))
	for _, m := range tail {
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		if len(content) > discoveryJudgeTailCap {
			content = content[:discoveryJudgeTailCap] + "…(truncated)"
		}
		entries = append(entries, map[string]any{"role": m.Role, "content": content})
	}
	cands := make([]map[string]any, 0, len(candidates))
	for _, d := range candidates {
		summary := strings.TrimSpace(d.Text)
		if len(summary) > discoveryJudgeSummaryCap {
			summary = summary[:discoveryJudgeSummaryCap]
		}
		cands = append(cands, map[string]any{
			"id":      d.ID,
			"kind":    d.Kind,
			"name":    d.Name,
			"summary": summary,
		})
	}
	return map[string]any{
		"request":         query,
		"transcript_tail": entries,
		"candidates":      cands,
	}
}

// discoveryJudgeKindLabel returns the human noun for a candidate kind used in
// the question text ("skill", "project document", "tool").
func discoveryJudgeKindLabel(kind string) string {
	switch kind {
	case "skill":
		return "skill"
	case "md":
		return "project document"
	case "mcp":
		return "tool"
	default:
		return "item"
	}
}

// discoveryJudgeKindDescription explains what the kind is to the judge.
func discoveryJudgeKindDescription(kind string) string {
	switch kind {
	case "skill":
		return "a reusable procedure the assistant follows"
	case "md":
		return "a project document with background or reference material"
	case "mcp":
		return "a callable tool"
	default:
		return "an item selected by an embedding search"
	}
}

// discoveryJudgeInstructions builds the per-candidate noul question. The
// candidate is referenced by its backticked state path (candidates[i]) so the
// judge reads the same structured state the code built.
func discoveryJudgeInstructions(d discovery.Doc, idx int) string {
	label := discoveryJudgeKindLabel(d.Kind)
	desc := discoveryJudgeKindDescription(d.Kind)
	return fmt.Sprintf(`You are an AI coding agent mid-conversation. The state holds the current request (request), a tail of the conversation (transcript_tail), and a numbered list of candidates (candidates) that an embedding search proposed from your skills, project docs, and tools.
Decide whether candidate `+"`candidates[%d]`"+` — the %s "%s" — is in the same scope as the current request.
Answer yes when it is even slightly relevant: it touches the same subject, component, decision, workflow, or concept as the request.
Answer no only when it is of a different scope — an unrelated subject that merely happens to share a word — or the conversation already covers it.
The candidate is %s.`, idx, label, d.Name, desc)
}
