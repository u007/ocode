package agent

import "time"

// RetryStatusEvent is emitted when a subagent is being retried.
type RetryStatusEvent struct {
	ID        string
	Name      string
	RetryCount int
	LastError  string
	RetryingAt time.Time
}

// IsRetryStatusEvent checks if an interface is a RetryStatusEvent.
func IsRetryStatusEvent(v interface{}) (*RetryStatusEvent, bool) {
	ev, ok := v.(*RetryStatusEvent)
	return ev, ok
}
