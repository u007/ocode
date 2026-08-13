package agent

import "errors"

// AskAsync runs a one-shot LLM completion in a background goroutine using the
// given message history and the main client. It does not mutate any agent or
// session state (no tools, no history persistence) — the caller owns the
// message list and the result. Delivers the result via onResult.
func (a *Agent) AskAsync(messages []Message, onResult func(content string, err error)) {
	if onResult == nil {
		return
	}
	go func() {
		if a.client == nil {
			onResult("", errors.New("no LLM client configured"))
			return
		}
		resp, err := a.client.Chat(messages, nil)
		if err != nil {
			onResult("", err)
			return
		}
		a.RecordSideUsageFromMessage(resp)
		onResult(resp.Content, nil)
	}()
}
