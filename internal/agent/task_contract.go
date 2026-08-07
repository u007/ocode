package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// contractVerdict is the result of verifying a dispatch's final result against
// its expected_output contract.
//
//	CheckFailed: the verification machinery itself failed (LLM error, timeout,
//	             malformed response). Satisfied is meaningless in this state —
//	             the caller must surface the failure, never proceed on it.
//	Satisfied:   the result met the contract (only meaningful when !CheckFailed).
//	Deficiency:  what the verifier said is missing, or the failure reason when
//	             CheckFailed. Empty when Satisfied.
type contractVerdict struct {
	CheckFailed bool
	Satisfied   bool
	Deficiency  string
}

// errMalformedVerdict marks a verifier response that carried no usable VERDICT
// line. It is logged and treated as not-satisfied — never as satisfied.
var errMalformedVerdict = errors.New("malformed contract verdict response")

// stripLabelPrefix removes a label prefix (e.g. "VERDICT:") case-insensitively,
// preserving the remainder's original case.
func stripLabelPrefix(line, label string) string {
	upperLine := strings.ToUpper(strings.TrimSpace(line))
	upperLabel := strings.ToUpper(label)
	if strings.HasPrefix(upperLine, upperLabel) {
		return strings.TrimSpace(line)[len(label):]
	}
	return strings.TrimSpace(line)
}

const taskContractVerifyTimeout = 30 * time.Second

// verifierClient returns the LLM client used for contract verification: the
// configured small model when enabled and resolvable, else the session client.
func (t TaskTool) verifierClient() LLMClient {
	if t.mainAgent == nil {
		return nil
	}
	a := t.mainAgent
	if a.config == nil || !a.config.Ocode.SmallModelEnabled {
		return a.client
	}
	model := strings.TrimSpace(a.config.Ocode.SmallModel)
	if model == "" {
		model = ResolveSmallModel(a.config)
	}
	if model == "" {
		return a.client
	}
	if client := newClientFn(a.config, model); client != nil {
		return client
	}
	return a.client
}

// verifyContract runs one verification call: the contract plus the child's
// final result, verdict out. A malformed or missing response is a CheckFailed
// verdict (logged, never treated as satisfied). The prompt lives in
// prompts/task_verifier.txt with the other subagent prompts, not inlined here.
func (t TaskTool) verifyContract(contract, result string) contractVerdict {
	client := t.verifierClient()
	if client == nil {
		return contractVerdict{CheckFailed: true, Deficiency: "no LLM client available for contract verification"}
	}
	prompt := strings.NewReplacer(
		"{contract}", contract,
		"{result}", result,
	).Replace(taskVerifierPromptText)

	ctx, cancel := context.WithTimeout(context.Background(), taskContractVerifyTimeout)
	defer cancel()

	respCh := make(chan struct {
		msg *Message
		err error
	}, 1)
	go func() {
		msg, err := client.Chat([]Message{{Role: "user", Content: prompt}}, nil)
		respCh <- struct {
			msg *Message
			err error
		}{msg: msg, err: err}
	}()

	var text string
	select {
	case <-ctx.Done():
		emitDebug("CONTRACT", fmt.Sprintf("verification timed out: %v", ctx.Err()))
		return contractVerdict{CheckFailed: true, Deficiency: "contract verification timed out"}
	case out := <-respCh:
		if out.err != nil {
			emitDebug("CONTRACT", fmt.Sprintf("verification call failed: %v", out.err))
			return contractVerdict{CheckFailed: true, Deficiency: "contract verification failed: " + out.err.Error()}
		}
		if out.msg == nil || strings.TrimSpace(out.msg.Content) == "" {
			return contractVerdict{CheckFailed: true, Deficiency: "contract verification returned an empty response"}
		}
		text = out.msg.Content
	}

	satisfied, deficiency, perr := parseVerdictResponse(text)
	if perr != nil {
		emitDebug("CONTRACT", fmt.Sprintf("%v (raw: %.200s)", perr, text))
		return contractVerdict{CheckFailed: true, Deficiency: "contract verification returned an unparseable verdict"}
	}
	return contractVerdict{Satisfied: satisfied, Deficiency: deficiency}
}

// parseVerdictResponse extracts the verdict from a verifier response. The
// verifier is told to emit exactly "VERDICT: SATISFIED" or "VERDICT:
// NOT_SATISFIED" plus a "DEFICIENCY:" line. Anything without a VERDICT line is
// malformed; a SATISFIED verdict with an unknown/missing shape is still
// accepted only when the VERDICT line literally says SATISFIED.
func parseVerdictResponse(text string) (satisfied bool, deficiency string, err error) {
	hasVerdict := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		upper := strings.ToUpper(trimmed)
		switch {
		case strings.HasPrefix(upper, "VERDICT:"):
			hasVerdict = true
			v := strings.ToUpper(strings.TrimSpace(stripLabelPrefix(trimmed, "VERDICT:")))
			if v == "SATISFIED" {
				return true, "", nil
			}
			// NOT_SATISFIED or any unknown value: not satisfied. An unknown
			// value still has a VERDICT line, so it is not "malformed" — the
			// deficiency (if any) below decides the message.
			satisfied = false
		case strings.HasPrefix(upper, "DEFICIENCY:"):
			deficiency = strings.TrimSpace(stripLabelPrefix(trimmed, "DEFICIENCY:"))
		}
	}
	if !hasVerdict {
		return false, "", errMalformedVerdict
	}
	return satisfied, deficiency, nil
}

// contractRetryMsg builds the deficiency feedback appended to the child's
// transcript for the single in-place retry.
func contractRetryMsg(contract, deficiency string) string {
	msg := fmt.Sprintf("Your previous result did not satisfy the required output contract: %q", contract)
	if strings.TrimSpace(deficiency) != "" {
		msg += "\n\nThe specific gap: " + strings.TrimSpace(deficiency)
	}
	msg += "\n\nPlease address the gap and return a corrected final result. Do not repeat work that already satisfies the contract."
	return msg
}

// contractWarningPrefix builds the explicit warning prefix placed on a result
// whose contract was not met (or could not be verified). The full child result
// follows after the blank line.
func contractWarningPrefix(contract, deficiency string) string {
	return fmt.Sprintf("⚠ Output contract not met: %q\nDeficiency: %s\n\nThe sub-agent's full result follows (contract failed; treat as unverified):", contract, deficiency)
}

// verifyAndRetryContract runs the verify-and-retry-once sequence for a
// dispatch that carries a contract (t.contract != ""). It must be called while
// the child subAgent is still live — inside runSyncDispatch / the
// runBackgroundDispatch goroutine, ahead of the shutdownTransient defer.
//
// The retry steps the SAME live child (executeSubAgentWithTranscript) with the
// deficiency appended to its full transcript — never a fresh dispatch (that
// would rebuild the child and trip the re-dispatch guard). Returns the final
// result (warning-prefixed when the contract was not met or the check itself
// failed) and the verdict to record on the run. When no contract applies, the
// result is returned unchanged.
func (t TaskTool) verifyAndRetryContract(specName string, subAgent *Agent, messages, resp []Message, result string, run *AgentRun) (string, []Message, contractVerdict) {
	if t.contract == "" {
		return result, resp, contractVerdict{}
	}

	v := t.verifyContract(t.contract, result)
	if v.CheckFailed {
		// The machinery failed (LLM error / timeout / malformed verdict).
		// Retrying against a broken verifier is pointless; surface the
		// failure loudly and keep the partial result visible.
		deficiency := "verification failed: " + v.Deficiency
		if run != nil {
			run.SetContractVerdict(false, deficiency)
		}
		return contractWarningPrefix(t.contract, deficiency) + "\n\n" + result, resp, v
	}
	if v.Satisfied {
		if run != nil {
			run.SetContractVerdict(true, "")
		}
		return result, resp, v
	}

	// Not satisfied: retry once, in place. The child sees its own full
	// prior transcript (messages + resp) plus the specific gap.
	retryMsg := contractRetryMsg(t.contract, v.Deficiency)
	retryMsgs := append(append(append([]Message{}, messages...), resp...), Message{Role: "user", Content: retryMsg})
	result2, resp2, err2 := t.executeSubAgentWithTranscript(specName, subAgent, retryMsgs)
	if err2 != nil {
		deficiency := fmt.Sprintf("retry failed: %v (original gap: %s)", err2, v.Deficiency)
		if run != nil {
			run.SetContractVerdict(false, deficiency)
		}
		return contractWarningPrefix(t.contract, deficiency) + "\n\n" + result, resp, contractVerdict{CheckFailed: true, Deficiency: deficiency}
	}
	result = result2
	resp = resp2

	v2 := t.verifyContract(t.contract, result)
	satisfied := !v2.CheckFailed && v2.Satisfied
	deficiency := v2.Deficiency
	if v2.CheckFailed {
		// Re-verification broke after a successful retry. The child did not
		// demonstrably meet the contract — fail loud, treat as not met.
		deficiency = "re-verification failed after retry: " + v2.Deficiency
	}
	if run != nil {
		run.SetContractVerdict(satisfied, deficiency)
	}
	if satisfied {
		return result, resp, v2
	}
	return contractWarningPrefix(t.contract, deficiency) + "\n\n" + result, resp, contractVerdict{Satisfied: false, Deficiency: deficiency}
}
