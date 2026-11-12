package tool

import "context"

// toolCallIDKey carries the ID of the model's tool call through a tool's
// context, so a tool whose side channels must be keyed per call (MCP Apps
// results, MCP images) can keep each call's outcome apart from every other
// call of the same tool, in this session or another one.
type toolCallIDKey struct{}

// WithToolCallID returns ctx carrying the tool call ID. An empty ID is not
// stored: ToolCallIDFrom then reports none.
func WithToolCallID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, toolCallIDKey{}, id)
}

// ToolCallIDFrom returns the tool call ID carried by ctx, or false when the
// call did not come from the model's tool-call loop.
func ToolCallIDFrom(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	id, ok := ctx.Value(toolCallIDKey{}).(string)
	return id, ok && id != ""
}
