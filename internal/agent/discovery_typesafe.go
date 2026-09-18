package agent

import (
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/discovery"
)

// discoveryJudgeModel is the TypeSafe System One model (Jev) consulted to
// decide whether each embedder-selected discovery candidate is actually needed
// for the current request. There is no separate config flag for the judge:
// "connected" means the shared client factory yields a TypesafeClient with a
// non-empty API key (see discoveryJudgeClient).
const discoveryJudgeModel = "typesafe/jev-latest"

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

// discoveryJudgeClient resolves the client for the TypeSafe discovery judge, or
// nil when TypeSafe is not connected. It is the sole "provider connected" check:
// the factory must yield a *TypesafeClient with a non-empty API key. A nil
// config, a non-typesafe factory result, or a keyless client all mean "no
// judge", and the caller keeps today's attach-everything behavior.
//
// The factory result is cached per discoveryState (see discoveryState.judge);
// with no discovery state the lookup is uncached.
func (a *Agent) discoveryJudgeClient() *TypesafeClient {
	if a.disco == nil {
		return a.resolveDiscoveryJudgeClient()
	}
	a.disco.judgeOnce.Do(func() { a.disco.judge = a.resolveDiscoveryJudgeClient() })
	return a.disco.judge
}

func (a *Agent) resolveDiscoveryJudgeClient() *TypesafeClient {
	if a.config == nil {
		return nil
	}
	client, ok := newClientFn(a.config, discoveryJudgeModel).(*TypesafeClient)
	if !ok || client == nil || client.APIKey == "" {
		return nil
	}
	return client
}

// judgeDiscoveryCandidates asks one noul (yes-probability) question per
// candidate in a single Decide call and returns the subset whose answer meets
// the shared auto-judge confidence floor. It is pure with respect to the sticky
// session: the caller seeds exactly what is returned.
//
// Fail-open contract: a transport/decode error returns (nil, err) and the
// caller seeds every candidate. A candidate whose answer is missing or not
// type "noul" is kept (fail-open per candidate) and logged — only a real
// below-threshold noul vetoes.
func (a *Agent) judgeDiscoveryCandidates(client *TypesafeClient, tail []Message, query string, candidates []discovery.Doc) ([]discovery.Doc, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	state := buildDiscoveryJudgeState(tail, query, candidates)
	questions := make(map[string]TypesafeQuestion, len(candidates))
	for i, d := range candidates {
		questions[d.ID] = TypesafeQuestion{
			Type:         "noul",
			Instructions: discoveryJudgeInstructions(d, i),
		}
	}

	resp, err := client.Decide(state, questions)
	if err != nil {
		return nil, err
	}
	a.RecordSideUsage(resp.Usage.InputTokens, resp.Usage.OutputTokens, 0, 0, "typesafe/"+client.Model)

	min := a.resolveAutoJudgeMinConfidence()
	model := "typesafe/" + client.Model
	var keep []discovery.Doc
	for _, d := range candidates {
		ans, ok := resp.Answers[d.ID]
		if !ok || ans.Type != "noul" {
			a.emitDebug("DISCOVERY", fmt.Sprintf("discovery_typesafe id=%s verdict=keep (missing noul answer; fail-open)", d.ID))
			keep = append(keep, d)
			continue
		}
		if ans.Noul >= min {
			a.emitDebug("DISCOVERY", fmt.Sprintf("discovery_typesafe id=%s noul=%.3f verdict=keep", d.ID, ans.Noul))
			keep = append(keep, d)
		} else {
			a.emitDebug("DISCOVERY", fmt.Sprintf("discovery_typesafe id=%s noul=%.3f verdict=veto", d.ID, ans.Noul))
		}
	}
	a.emitDebug("DISCOVERY", fmt.Sprintf("discovery_typesafe kept=%d vetoed=%d min=%.2f model=%s", len(keep), len(candidates)-len(keep), min, model))
	return keep, nil
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
Decide whether loading candidate `+"`candidates[%d]`"+` — the %s "%s" — into your context is needed to complete the current request.
Answer yes when the request cannot be handled well without this %s, or it directly covers the task.
Answer no when it is only topically adjacent but not required, or the conversation already covers the task.
The candidate is %s.`, idx, label, d.Name, label, desc)
}
