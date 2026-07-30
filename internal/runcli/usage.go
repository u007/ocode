package runcli

import (
	"encoding/json"
	"os"
	"sync"
)

// usageTotals accumulates token usage across every model call in a headless
// run. The provider's usage callback fires from the SSE-reading goroutine, so
// every field is guarded.
type usageTotals struct {
	mu           sync.Mutex
	inputTokens  int64
	outputTokens int64
	modelCalls   int
}

// record adds one model call's absolute token counts to the running totals.
func (u *usageTotals) record(inputTokens, outputTokens int64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.inputTokens += inputTokens
	u.outputTokens += outputTokens
	u.modelCalls++
}

func (u *usageTotals) snapshot() (int64, int64, int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.inputTokens, u.outputTokens, u.modelCalls
}

// emitUsageEvent writes the run's single trailing usage event as one JSON line.
func emitUsageEvent(sessionID, modelName string, totals *usageTotals) error {
	in, out, calls := totals.snapshot()
	return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
		"type":          "usage",
		"sessionID":     sessionID,
		"input_tokens":  in,
		"output_tokens": out,
		"total_tokens":  in + out,
		"model_calls":   calls,
		"model":         modelName,
	})
}
