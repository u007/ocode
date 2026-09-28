package tool

import (
	"context"
	"errors"
	"testing"
)

// TestSearchJudgeContextRoundTrip pins the context seam: a judge attached with
// WithSearchResultJudge is returned verbatim by SearchJudgeFromContext and is
// callable through the retrieved value.
func TestSearchJudgeContextRoundTrip(t *testing.T) {
	type ctxKey struct{} // unrelated key must not shadow the judge
	called := false
	judge := SearchResultJudge(func(req SearchJudgeRequest) ([]SearchResult, int, error) {
		called = true
		if req.Tool != "grep" {
			t.Fatalf("judge received tool %q, want grep", req.Tool)
		}
		return req.Results, 0, nil
	})

	ctx := context.WithValue(context.Background(), ctxKey{}, "noise")
	ctx = WithSearchResultJudge(ctx, judge)

	got := SearchJudgeFromContext(ctx)
	if got == nil {
		t.Fatal("SearchJudgeFromContext returned nil for an attached judge")
	}
	kept, vetoed, err := got(SearchJudgeRequest{Tool: "grep", Results: []SearchResult{{Path: "a.go"}}})
	if err != nil {
		t.Fatalf("judge call returned error: %v", err)
	}
	if !called {
		t.Fatal("retrieved judge was not the attached function")
	}
	if len(kept) != 1 || vetoed != 0 {
		t.Fatalf("kept=%v vetoed=%d, want 1 kept / 0 vetoed", kept, vetoed)
	}
}

// TestSearchJudgeFromContextNilSafe pins the nil-handling contract the context
// helpers must mirror from workdir_ctx.go: a nil context, a context with no
// judge, and an explicitly attached nil judge all yield nil without panicking.
func TestSearchJudgeFromContextNilSafe(t *testing.T) {
	if got := SearchJudgeFromContext(nil); got != nil {
		t.Fatalf("SearchJudgeFromContext(nil) = %v, want nil", got)
	}
	if got := SearchJudgeFromContext(context.Background()); got != nil {
		t.Fatalf("SearchJudgeFromContext(empty ctx) = %v, want nil", got)
	}
	ctx := WithSearchResultJudge(context.Background(), nil)
	if got := SearchJudgeFromContext(ctx); got != nil {
		t.Fatalf("SearchJudgeFromContext(ctx with nil judge) = %v, want nil", got)
	}
	// A nil parent context must not panic; it behaves like an empty context.
	ctx = WithSearchResultJudge(nil, SearchResultJudge(func(SearchJudgeRequest) ([]SearchResult, int, error) {
		return nil, 0, nil
	}))
	if got := SearchJudgeFromContext(ctx); got == nil {
		t.Fatal("WithSearchResultJudge(nil, judge) dropped the judge")
	}
}

// TestSearchJudgeErrorIsDistinguishable pins the reason the signature keeps an
// error return: a judge that fails must be distinguishable from one that ran
// and kept everything, so the caller can render an unfiltered footer.
func TestSearchJudgeErrorIsDistinguishable(t *testing.T) {
	wantErr := errors.New("judge unavailable")
	judge := SearchResultJudge(func(SearchJudgeRequest) ([]SearchResult, int, error) {
		return nil, 0, wantErr
	})
	ctx := WithSearchResultJudge(context.Background(), judge)
	got := SearchJudgeFromContext(ctx)
	kept, vetoed, err := got(SearchJudgeRequest{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if kept != nil || vetoed != 0 {
		t.Fatalf("kept=%v vetoed=%d, want nil/0 on error", kept, vetoed)
	}
}
