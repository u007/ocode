package agent

import (
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/knowledge"
)

// docSearchJudgeSummaryCap bounds one candidate's summary in the doc_search
// judge state so an oversized document body cannot blow up the request.
const docSearchJudgeSummaryCap = 1000

// docSearchJudgeSummary builds the bounded text the judge sees for one doc:
// the description followed by the body, capped. Title/path/type/tags ride as
// structured fields in buildDocSearchJudgeState, so only the prose needs a cap.
func docSearchJudgeSummary(d *knowledge.Doc) string {
	if d == nil {
		return ""
	}
	var parts []string
	if desc := strings.TrimSpace(d.Description); desc != "" {
		parts = append(parts, desc)
	}
	if body := strings.TrimSpace(d.Body); body != "" {
		parts = append(parts, body)
	}
	summary := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if len(summary) > docSearchJudgeSummaryCap {
		summary = summary[:docSearchJudgeSummaryCap] + "…(truncated)"
	}
	return summary
}

// buildDocSearchJudgeState assembles the structured judge request for one
// doc_search call: the query the caller is looking for plus one entry per
// returned document. Pure so the state shape is unit-testable.
func buildDocSearchJudgeState(query string, docs []*knowledge.Doc) map[string]any {
	cands := make([]map[string]any, 0, len(docs))
	for _, d := range docs {
		if d == nil {
			continue
		}
		entry := map[string]any{
			"id":   d.Path,
			"name": d.Title,
		}
		if d.Type != "" {
			entry["type"] = d.Type
		}
		if len(d.Tags) > 0 {
			entry["tags"] = d.Tags
		}
		if summary := docSearchJudgeSummary(d); summary != "" {
			entry["summary"] = summary
		}
		cands = append(cands, entry)
	}
	return map[string]any{
		"request":    query,
		"candidates": cands,
	}
}

// docSearchJudgeInstructions builds the per-document noul question. The
// candidate is referenced by its backticked state path (candidates[i]) so the
// judge reads the same structured state the code built. The rubric is the
// lenient relevance policy: slight relevance is a yes; only a different scope is
// a no.
func docSearchJudgeInstructions(d *knowledge.Doc, idx int) string {
	title := ""
	if d != nil {
		title = d.Title
	}
	return fmt.Sprintf(`You are an AI coding agent. The state holds the current search request (request) and a numbered list of candidate documents (candidates) that a keyword search of the project's knowledge bundle returned.
Decide whether candidate `+"`candidates[%d]`"+` — the document "%s" — is in the same scope as the search request.
Answer yes when the document is even slightly relevant: it touches the same subject, component, decision, workflow, or concept as the request.
Answer no only when it is of a different scope — an unrelated subject that merely happens to share a word with the request.`, idx, title)
}

// judgeDocSearchResults asks one noul (yes-probability) question per document in
// a single Decide call and returns the subset judged in scope, preserving the
// input (rank) order. Fail-open: a transport/decode error returns (nil, err) so
// the caller shows every result; a missing or non-noul answer keeps that doc.
func (a *Agent) judgeDocSearchResults(client *TypesafeClient, query string, docs []*knowledge.Doc) ([]*knowledge.Doc, error) {
	if len(docs) == 0 {
		return docs, nil
	}
	state := buildDocSearchJudgeState(query, docs)
	ids := make([]string, 0, len(docs))
	questions := make(map[string]TypesafeQuestion, len(docs))
	for i, d := range docs {
		if d == nil {
			continue
		}
		ids = append(ids, d.Path)
		questions[d.Path] = TypesafeQuestion{
			Type:         "noul",
			Instructions: docSearchJudgeInstructions(d, i),
		}
	}
	if len(ids) == 0 {
		return docs, nil
	}

	keepSet, err := a.judgeRelevanceQuestions(client, "KNOWLEDGE", "doc_search_typesafe", ids, state, questions)
	if err != nil {
		return nil, err
	}
	keep := make([]*knowledge.Doc, 0, len(docs))
	for _, d := range docs {
		if d != nil && keepSet[d.Path] {
			keep = append(keep, d)
		}
	}
	return keep, nil
}

// docSearchJudge returns the callback the DocSearchTool uses to vet its results,
// or nil when TypeSafe is not connected. "Connected" is the shared
// discoveryJudgeClient check (a keyed TypesafeClient from the factory); the
// client is resolved once per context-subagent dispatch, which is the same
// granularity at which the doc tools are built. The callback itself never fails:
// on any judge error it returns the full list (fail-open — the judge may only
// hide results, never make a search return fewer because of a failure).
func (a *Agent) docSearchJudge() DocSearchJudge {
	client := a.discoveryJudgeClient()
	if client == nil {
		return nil
	}
	return func(query string, docs []*knowledge.Doc) ([]*knowledge.Doc, error) {
		kept, err := a.judgeDocSearchResults(client, query, docs)
		if err != nil {
			a.emitDebug("KNOWLEDGE", fmt.Sprintf("doc_search_typesafe judge=%s failed (fail-open, all %d results kept): %v", client.Model, len(docs), err))
			return docs, nil
		}
		return kept, nil
	}
}
