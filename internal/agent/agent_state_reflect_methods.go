package agent

// reflectTail applies reflection against the agent-owned baseline. It takes
// the current canonical snapshot (from an external state source); compares
// with the agent's previous snapshot; emits a user message only on change;
// updates the agent's baseline; and appends strictly at the tail.
func (a *Agent) reflectTail(messages []Message, current ReflectState) []Message {
	msg, emitted := ReflectMessage(a.reflectState, current)
	if emitted {
		messages = append(messages, msg)
	}
	// Baseline always advances (even when unchanged) so stale state
	// doesn't leak into the next comparison.
	a.reflectState = current
	return messages
}

// SetReflectState updates the agent-owned baseline from an external source.
// It does not inject directly; the Step loop picks it up at the tail seam.
func (a *Agent) SetReflectState(current ReflectState) {
	a.reflectState = current
}
