package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/redact"
	"github.com/u007/ocode/internal/tool"
)

const (
	// toolDigestFallbackContextWindow is used when the model's window is not
	// in the registry. It is deliberately conservative.
	toolDigestFallbackContextWindow int64 = 100000
	// toolDigestFallbackResponseReserve is the output room reserved when the
	// model's output limit is not in the registry.
	toolDigestFallbackResponseReserve int64 = 8192
)

// toolDigestToolbox is the fixed set of read-only tools the explainer loop may
// call. Only the tools added here are offered to the side model, and Execute
// refuses any other name.
type toolDigestToolbox struct {
	defs    []map[string]interface{}
	tools   map[string]tool.Tool
	cleanup []func()
}

// add registers t. Its definition is offered to the model and its name routes
// Execute.
func (b *toolDigestToolbox) add(t tool.Tool) {
	if b.tools == nil {
		b.tools = make(map[string]tool.Tool)
	}
	b.defs = append(b.defs, t.Definition())
	b.tools[t.Name()] = t
}

// addCleanup registers fn to run from Close.
func (b *toolDigestToolbox) addCleanup(fn func()) {
	b.cleanup = append(b.cleanup, fn)
}

// Definitions returns the tool definitions offered to the model.
func (b *toolDigestToolbox) Definitions() []map[string]interface{} {
	return b.defs
}

// Execute runs the named tool. It uses ExecuteCtx when the tool offers it, so
// cancellation reaches the tool, and Execute otherwise.
func (b *toolDigestToolbox) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	t, ok := b.tools[name]
	if !ok {
		return "", fmt.Errorf("tool %q is not in the explainer toolbox", name)
	}
	if ct, ok := t.(interface {
		ExecuteCtx(context.Context, json.RawMessage) (string, error)
	}); ok {
		return ct.ExecuteCtx(ctx, args)
	}
	return t.Execute(args)
}

// Close runs the registered cleanups in reverse order. It is idempotent: the
// cleanup list is cleared before it runs, so a second Close is a no-op.
func (b *toolDigestToolbox) Close() {
	cleanups := b.cleanup
	b.cleanup = nil
	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}
}

// toolDigestContextGuard budgets the explainer's model context. Its values are
// frozen when the guard is built, so one run uses one consistent window.
type toolDigestContextGuard struct {
	Model           string
	CharsPerToken   float64
	ContextWindow   int64
	ResponseReserve int64
}

// newToolDigestContextGuard resolves the window and output reserve for client.
// An unknown window falls back to toolDigestFallbackContextWindow; an unknown
// output limit falls back to toolDigestFallbackResponseReserve. The reserve is
// capped at the window. client must be non-nil.
func newToolDigestContextGuard(client LLMClient) toolDigestContextGuard {
	model := modelIDForClient(client)
	window := ModelWindow(model)
	if window <= 0 {
		window = toolDigestFallbackContextWindow
	}
	reserve := ModelMaxOutputTokens(model)
	if reserve <= 0 {
		reserve = toolDigestFallbackResponseReserve
	}
	if reserve > window {
		reserve = window
	}
	return toolDigestContextGuard{
		Model:           model,
		CharsPerToken:   charsPerTokenFor(client.GetProvider(), client.GetModel()),
		ContextWindow:   window,
		ResponseReserve: reserve,
	}
}

// admit reports whether messages, plus the response reserve, fit in the
// window. It never truncates: a candidate that does not fit is refused whole.
func (g toolDigestContextGuard) admit(messages []Message) error {
	tokens, _ := CurrentContextEstimate(messages, g.CharsPerToken)
	remaining := g.ContextWindow - g.ResponseReserve - int64(tokens)
	if remaining < 0 {
		return &toolDigestContextError{RemainingTokens: remaining}
	}
	return nil
}

// toolDigestContextError is the refusal from admit. RemainingTokens is the
// window left after the response reserve and the candidate; it is negative for
// a refusal, and the amount by which the candidate is over.
type toolDigestContextError struct {
	RemainingTokens int64
}

func (e *toolDigestContextError) Error() string {
	return fmt.Sprintf("explanation context budget exceeded: remaining_context_tokens=%d", e.RemainingTokens)
}

// toolDigestKey identifies one explainer run for coordination. A non-empty
// requestID keys on the caller's id, so a newer request for the same ask
// supersedes it; an empty one keys on payloadHash, the SHA-256 of the canonical
// MASKED input. Raw unmasked input is never hashed. The cache keys on the
// request id and payload (see toolDigestCacheKey).
func toolDigestKey(owner, requestID, payloadHash string) string {
	material := payloadHash
	if requestID != "" {
		material = requestID
	}
	sum := sha256.Sum256([]byte(owner + "\x00" + material))
	return hex.EncodeToString(sum[:])
}

// toolDigestCacheKey keys a cached explanation on the request id AND the masked
// payload. A provider that reuses a tool-call id across turns (call_0 every
// turn) then misses the cache when the call differs, so one call's explanation
// never answers another. A new id always starts a fresh run.
func toolDigestCacheKey(owner, requestID, payloadHash string) string {
	sum := sha256.Sum256([]byte(owner + "\x00" + requestID + "\x00" + payloadHash))
	return hex.EncodeToString(sum[:])
}

// toolDigestSnapshot is the agent state one explainer run reads. It is copied
// once, before the run starts, so the run never touches mutable Agent fields.
type toolDigestSnapshot struct {
	WorkDir           string
	SessionID         string
	Profile           string
	Config            config.Config
	RedactionHook     *redact.NetHook
	RedactionRegistry *redact.Registry
	Client            LLMClient
	OnSideUsage       func(promptTokens, completionTokens, cacheReadTokens, cacheWriteTokens int64, spend *float64)
}
