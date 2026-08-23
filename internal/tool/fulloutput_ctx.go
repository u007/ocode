package tool

import "context"

// fullOutputRetainedKey marks a tool-execution context whose consumer renders
// the live stream as the transcript and therefore needs the canonical result
// to keep the FULL, uncapped output.
//
// This is deliberately decoupled from "is a streaming sink wired" (emit !=
// nil). The two are not the same question:
//
//   - The TUI streams chunks AND keeps them on screen as the transcript, then
//     reads the full text back from Message.DisplayContent. It opts in.
//   - The HTTP/SSE server streams chunks for live progress only. The browser
//     receives the authoritative result through the separately-truncated
//     tool_result frame; DisplayContent is json:"-" and never reaches it. So
//     retaining the uncapped text there is pure memory growth on the hot bash
//     path for a consumer that structurally cannot read it.
//
// The default is therefore bounded (cap applied) and retention is opt-in.
type fullOutputRetainedKey struct{}

// WithFullOutputRetained returns a context declaring whether the caller keeps
// the full, uncapped tool output. Only callers that display the streamed text
// as the transcript should pass true.
func WithFullOutputRetained(ctx context.Context, retained bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, fullOutputRetainedKey{}, retained)
}

// FullOutputRetained reports whether the context opted into keeping the full
// uncapped output. Absent the marker it returns false, so the bashMaxOutputLength
// cap applies by default.
func FullOutputRetained(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	retained, _ := ctx.Value(fullOutputRetainedKey{}).(bool)
	return retained
}
