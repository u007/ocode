package tool

import "context"

// SearchPolicy narrows one search call. It rides on the context, so a call
// without it runs exactly as it always has; only a caller that sets it (the
// permission explainer's read-only file wrappers) changes behavior.
type SearchPolicy struct {
	// Blocked reports whether a path must be left out of results and never
	// opened. Walks pass absolute paths; ripgrep results pass the paths they
	// print. A blocked directory is skipped whole.
	Blocked func(path string) bool
	// IgnoreGlobs are ripgrep globs, without the leading "!", excluded from
	// every rg run. They are matched case-insensitively and placed after the
	// include glob so they take precedence over it.
	IgnoreGlobs []string
}

type searchPolicyKey struct{}

// WithSearchPolicy returns ctx carrying p for the search tools.
func WithSearchPolicy(ctx context.Context, p SearchPolicy) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, searchPolicyKey{}, p)
}

// searchPolicyFromContext returns the policy stored by WithSearchPolicy, or the
// zero policy, which blocks nothing.
func searchPolicyFromContext(ctx context.Context) SearchPolicy {
	if ctx == nil {
		return SearchPolicy{}
	}
	p, _ := ctx.Value(searchPolicyKey{}).(SearchPolicy)
	return p
}

// blocks reports whether the policy excludes path.
func (p SearchPolicy) blocks(path string) bool {
	return p.Blocked != nil && p.Blocked(path)
}
