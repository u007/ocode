package agent

import "time"

// RetryStatusEvent is emitted when an LLM call or subagent is being retried.
type RetryStatusEvent struct {
	ID         string
	Name       string
	RetryCount int
	MaxRetries int
	LastError  string
	RetryDelay time.Duration
	RetryingAt time.Time
	Kind       string // "llm" or "subagent"
}

// IsRetryStatusEvent checks if an interface is a RetryStatusEvent.
func IsRetryStatusEvent(v interface{}) (*RetryStatusEvent, bool) {
	ev, ok := v.(*RetryStatusEvent)
	return ev, ok
}
