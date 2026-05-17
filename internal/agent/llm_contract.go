package agent

type StreamEventKind string

const (
	StreamEventTextDelta     StreamEventKind = "text_delta"
	StreamEventThinkingDelta StreamEventKind = "thinking_delta"
	StreamEventToolCallDelta StreamEventKind = "tool_call_delta"
	StreamEventUsage         StreamEventKind = "usage"
	StreamEventDone          StreamEventKind = "done"
	StreamEventError         StreamEventKind = "error"
)

type StreamEvent struct {
	Kind     StreamEventKind
	Text     string
	ToolCall *ToolCall
	Usage    *TokenUsage
	Err      error
}

type StreamEmitFunc func(StreamEvent) error

type StreamingLLMClient interface {
	LLMClient
	Stream(messages []Message, tools []map[string]interface{}, emit StreamEmitFunc) (*Message, error)
}

type ProviderCapability string

const (
	ProviderCapabilityTools     ProviderCapability = "tools"
	ProviderCapabilityStreaming ProviderCapability = "streaming"
	ProviderCapabilityThinking  ProviderCapability = "thinking"
	ProviderCapabilityImages    ProviderCapability = "images"
	ProviderCapabilityUsage     ProviderCapability = "usage"
)
