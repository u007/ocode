package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// waitCeiling caps any wait so the session cannot hang.
const waitCeiling = 10 * time.Minute

// waitPollInterval is how often a targeted wait re-checks the job.
const waitPollInterval = 250 * time.Millisecond

// WaitTool sleeps for a duration, or blocks until a named job completes.
type WaitTool struct {
	procs *tool.ProcessRegistry
	runs  *AgentRunRegistry
	agent *Agent // used to get the current stop channel
}

func (t WaitTool) Name() string { return "wait" }
func (t WaitTool) Description() string {
	return "Wait for a duration or until a background job finishes"
}
func (t WaitTool) Parallel() bool { return false }
func (t WaitTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "wait",
		"description": "Pause for a fixed duration, or (with 'for') block until a background process/agent completes. Capped at 10 minutes.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"seconds": map[string]interface{}{
					"type":        "integer",
					"description": "Seconds to wait. Ignored when 'minutes' is set.",
				},
				"minutes": map[string]interface{}{
					"type":        "integer",
					"description": "Minutes to wait. Takes precedence over 'seconds'.",
				},
				"for": map[string]interface{}{
					"type":        "string",
					"description": "Optional background process id (proc-N) or agent run id (agent-N) to block on. When set, returns as soon as that job completes.",
				},
			},
		},
	}
}

// resolveWaitDuration converts seconds/minutes into a capped duration.
func resolveWaitDuration(seconds, minutes int) (d time.Duration, clamped bool) {
	if minutes > 0 {
		d = time.Duration(minutes) * time.Minute
	} else {
		d = time.Duration(seconds) * time.Second
	}
	if d <= 0 {
		d = time.Second
	}
	if d > waitCeiling {
		return waitCeiling, true
	}
	return d, false
}

func (t WaitTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Seconds int    `json:"seconds"`
		Minutes int    `json:"minutes"`
		For     string `json:"for"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	d, clamped := resolveWaitDuration(params.Seconds, params.Minutes)

	if params.For != "" {
		return t.waitForJob(params.For, d, clamped), nil
	}

	// Plain duration sleep — interruptible via the agent's current stop channel.
	var stopCh <-chan struct{}
	if t.agent != nil {
		stopCh = t.agent.StopCh()
	}
	ticker := time.NewTicker(d)
	defer ticker.Stop()
	select {
	case <-ticker.C:
	case <-stopCh:
		return "wait cancelled", nil
	}
	msg := fmt.Sprintf("Waited %s.", d)
	if clamped {
		msg += " (clamped to the 10-minute ceiling)"
	}
	return msg, nil
}

// waitForJob blocks until the named job completes or the deadline passes,
// returning immediately if the agent is cancelled.
func (t WaitTool) waitForJob(id string, deadline time.Duration, clamped bool) string {
	var stopCh <-chan struct{}
	if t.agent != nil {
		stopCh = t.agent.StopCh()
	}
	end := time.Now().Add(deadline)
	ticker := time.NewTicker(waitPollInterval)
	defer ticker.Stop()
	for {
		if done, summary := t.jobDone(id); done {
			return summary
		}
		if time.Now().After(end) {
			suffix := ""
			if clamped {
				suffix = " (ceiling reached)"
			}
			return fmt.Sprintf("Timed out after %s waiting for %s; it is still running.%s", deadline, id, suffix)
		}
		select {
		case <-ticker.C:
		case <-stopCh:
			return fmt.Sprintf("wait for %s cancelled", id)
		}
	}
}

// jobDone reports whether the job has finished, with a result summary.
func (t WaitTool) jobDone(id string) (bool, string) {
	if strings.HasPrefix(id, "agent-") && t.runs != nil {
		run, ok := t.runs.Get(id)
		if !ok {
			return true, fmt.Sprintf("Error: unknown run id %q", id)
		}
		switch run.statusValue() {
		case RunDone:
			return true, fmt.Sprintf("[%s done]\n%s", id, run.Result)
		case RunFailed:
			return true, fmt.Sprintf("[%s failed]\n%s", id, run.Err)
		default:
			return false, ""
		}
	}
	if strings.HasPrefix(id, "proc-") && t.procs != nil {
		_, status, code, err := t.procs.Dump(id)
		if err != nil {
			return true, fmt.Sprintf("Error: %v", err)
		}
		if status == tool.ProcRunning {
			return false, ""
		}
		return true, fmt.Sprintf("[%s %s exit=%d]", id, status, code)
	}
	return true, fmt.Sprintf("Error: unknown job id %q", id)
}
