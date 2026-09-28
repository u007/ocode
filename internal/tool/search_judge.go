package tool

import "context"

// SearchResult is one file-level search hit the judge can score.
//
// It is the structured form of a single file's matches, produced by the
// code-search tools (grep/rgrep/glob) between collection and formatting so a
// relevance judge can filter the set before the tool renders it. One result
// carries a whole file: a veto takes every matching line with it.
type SearchResult struct {
	// Path is the display path, exactly as the tool would render it (project
	// relative when no path argument was given, re-prefixed or absolute
	// otherwise — see searchDisplayPath / rgDisplayPath).
	Path string
	// Summary is a bounded sample of what matched: the first few lines with
	// their line numbers, so the judge can see where the file matched and not
	// merely that it did.
	Summary string
	// Count is the match count; 0 for a plain listing (glob).
	Count int
}

// SearchJudgeRequest is one search call's judge input.
type SearchJudgeRequest struct {
	// Tool is the invoking tool name: "grep" | "rgrep" | "glob".
	Tool string
	// Intent is the caller's required intent argument. An empty Intent means
	// the judge must not be consulted at all (see the agent-side judge), not
	// that everything is out of scope.
	Intent string
	// Query carries the non-secret scalar arguments that describe the search
	// (pattern / path / include). It must never carry secret material: the
	// judge request is a network round trip to a third-party model.
	Query map[string]string
	// Results is the candidate set in the tool's pre-judge output order.
	Results []SearchResult
}

// SearchResultJudge filters Results against Intent, returning the subset to
// render, how many were vetoed, and any error. On a non-nil error the caller
// must render every result unfiltered.
//
// The error return is deliberate: without it, "the judge errored and the caller
// kept everything" would be indistinguishable from "the judge ran and
// everything was in scope". Both render as a complete list with no footer, so
// neither the model nor the logs could tell a judged result from an unjudged
// one. The error is what lets the tool disclose a failed judge (fail-open with
// a visible footer). A judge can only ever hide a result, never add one.
type SearchResultJudge func(SearchJudgeRequest) (kept []SearchResult, vetoed int, err error)

// searchResultJudgeKey carries the per-agent relevance judge through the
// tool-execution context so code-search tools can filter their result set
// without a judge field on the tool struct.
//
// The context seam is load-bearing, not stylistic: sub-agents and transient
// advisor agents are handed the parent agent's tool objects (see ask.go,
// subagent.go, advisor_tool.go), so a judge stored on the tool struct would be
// overwritten with the child's judge when a child is built — and for the
// transient advisor that judge belongs to an agent shut down immediately
// after its single call. The parent's later searches would then judge against
// a dead agent, and because the search tools declare Parallel() the write is a
// data race. Carrying the judge on the context, exactly as the session's
// project root is carried (workdir_ctx.go), means each agent attaches its own
// judge for its own call: no shared mutable state, no clobbering, no race.
type searchResultJudgeKey struct{}

// WithSearchResultJudge returns a context carrying judge for search tools.
// A nil ctx is treated as context.Background(). A nil judge is stored as-is
// and reads back as nil (unfiltered), matching WithWorkDir's tolerance for
// zero values.
func WithSearchResultJudge(ctx context.Context, judge SearchResultJudge) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, searchResultJudgeKey{}, judge)
}

// SearchJudgeFromContext returns the judge stored by WithSearchResultJudge, or
// nil when none is attached. A nil context is safe and returns nil; a nil
// result means "no judge, render everything" — never "veto everything".
func SearchJudgeFromContext(ctx context.Context) SearchResultJudge {
	if ctx == nil {
		return nil
	}
	judge, _ := ctx.Value(searchResultJudgeKey{}).(SearchResultJudge)
	return judge
}
