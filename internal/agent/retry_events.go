package agent

// RetryEvents returns the channel for retry status events.
func (a *Agent) RetryEvents() chan *RetryStatusEvent {
	return a.retryEvents
}

// EmitRetryStatus sends a retry status event to the TUI.
func (a *Agent) EmitRetryStatus(ev *RetryStatusEvent) {
	if a.retryEvents != nil {
		select {
		case a.retryEvents <- ev:
		default:
			// Channel full, drop event
		}
	}
}
