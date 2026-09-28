package tool

import (
	"context"
	"fmt"
	"strings"
)

// searchJudgeMaxCandidates is the cost cap on one judge request. Results past
// the cap are kept unjudged, never dropped: the cap is a cost control and must
// not decide relevance. The pre-judge order is each tool's own output order (for
// glob that is modification time), and the footer discloses the unjudged count
// whenever a footer is rendered.
const searchJudgeMaxCandidates = 40

// searchJudgeSampleLines bounds how many matching lines per file are put into a
// candidate's summary for the judge.
const searchJudgeSampleLines = 5

// searchJudgeOutcome is the tool-facing result of one judge pass.
type searchJudgeOutcome struct {
	// Kept is the result set to render. On a judge error it is the full input
	// (fail-open); otherwise it is the kept candidates followed by the unjudged
	// remainder, in pre-judge order.
	Kept []SearchResult
	// Vetoed is how many judged candidates the judge removed.
	Vetoed int
	// Judged is how many candidates were sent to the judge (at most the cap).
	Judged int
	// Unjudged is how many results were past the cap and kept without judging.
	Unjudged int
	// Err is non-nil when the judge failed. The caller must then render Kept
	// unfiltered and disclose Err in a footer.
	Err error
}

// KeptPaths returns the set of paths in Kept, so a tool holding a richer
// per-file struct can filter its own collection without a second mapping.
func (o searchJudgeOutcome) KeptPaths() map[string]bool {
	set := make(map[string]bool, len(o.Kept))
	for _, r := range o.Kept {
		set[r.Path] = true
	}
	return set
}

// AllVetoed reports whether the judge ran, removed at least one candidate, and
// left nothing to render. The caller renders the actionable all-filtered
// message instead of an empty list.
func (o searchJudgeOutcome) AllVetoed() bool {
	return o.Err == nil && o.Vetoed > 0 && len(o.Kept) == 0
}

// Footer returns the disclosure to append after the rendered result set, or ""
// when there is nothing to say. A judge error is always disclosed (fail-open is
// only useful if it is visible); a judged run with no vetoes appends nothing, so
// the common case stays byte-identical to the unjudged output.
func (o searchJudgeOutcome) Footer() string {
	if o.Err != nil {
		return fmt.Sprintf("\n\n[relevance judge unavailable — results are unfiltered: %v]", o.Err)
	}
	if o.Vetoed == 0 {
		return ""
	}
	msg := fmt.Sprintf("[relevance judge: %d of %d result(s) omitted as out of scope for this intent]", o.Vetoed, o.Judged)
	if o.Unjudged > 0 {
		msg += fmt.Sprintf("; %d beyond the judge cap were not judged", o.Unjudged)
	}
	return "\n\n" + msg
}

// runSearchJudge applies the context judge to results, applying the candidate
// cap. A nil judge (TypeSafe not connected) returns results untouched, which is
// how every judge-absent path stays byte-identical to the pre-judge behaviour.
// Results past the cap are appended to Kept unjudged, so the cap can never drop
// a result. On a judge error the whole input is returned with Err set: the
// caller renders everything and discloses the failure.
func runSearchJudge(ctx context.Context, toolName, intent string, query map[string]string, results []SearchResult) searchJudgeOutcome {
	judge := SearchJudgeFromContext(ctx)
	if judge == nil || len(results) == 0 {
		return searchJudgeOutcome{Kept: results}
	}
	send := results
	unjudged := 0
	if len(send) > searchJudgeMaxCandidates {
		unjudged = len(send) - searchJudgeMaxCandidates
		send = send[:searchJudgeMaxCandidates]
	}
	kept, vetoed, err := judge(SearchJudgeRequest{
		Tool:    toolName,
		Intent:  intent,
		Query:   query,
		Results: send,
	})
	if err != nil {
		return searchJudgeOutcome{Kept: results, Judged: len(send), Unjudged: unjudged, Err: err}
	}
	kept = append(kept, results[len(send):]...)
	return searchJudgeOutcome{Kept: kept, Vetoed: vetoed, Judged: len(send), Unjudged: unjudged}
}

// writeSearchResultBlock renders one file's result in the given output_mode,
// writing no inter-file separator (the caller owns separators, because grep
// separates blocks with a blank line and rgrep does not). Content lines are the
// already-normalized "<line>:<text>" entries; list modes write a single line.
// grep and rgrep share this so the per-mode shape cannot drift between them.
func writeSearchResultBlock(b *strings.Builder, mode, path string, count int, lines []string) {
	switch mode {
	case "files_with_matches":
		b.WriteString(path)
	case "count":
		b.WriteString(fmt.Sprintf("%s: %d", path, count))
	case "content":
		for _, line := range lines {
			b.WriteString(fmt.Sprintf("%s:%s\n", path, line))
		}
	}
}

// searchJudgeAllVetoedMessage is rendered when every judged result was vetoed.
// It carries an actionable tail so the model narrows the search or fixes its
// intent instead of widening the pattern and re-running in a loop.
func searchJudgeAllVetoedMessage(n int, narrowHint string) string {
	return fmt.Sprintf("Found %d matching file(s), but none are in scope for this intent (relevance judge omitted all %d).\nNarrow %s, or re-run with an intent that matches what these files contain.", n, n, narrowHint)
}
