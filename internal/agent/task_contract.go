package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/crashguard"
)

// contractVerdict is the result of verifying a dispatch's final result against
// its expected_output contract.
//
//	CheckFailed: the verification machinery itself failed (LLM error, timeout,
//	             malformed response). Satisfied is meaningless in this state —
//	             the caller must surface the failure, never proceed on it.
//	TimedOut:    CheckFailed because a caller-configured deadline elapsed
//	             (Agent.RequestTimeout). It is a proper subset of CheckFailed:
//	             no judgement was produced, but the reason is specifically a
//	             timeout and must be reported as one.
//	Satisfied:   the result met the contract (only meaningful when !CheckFailed).
//	Deficiency:  what the verifier said is missing, or the failure reason when
//	             CheckFailed. Empty when Satisfied.
type contractVerdict struct {
	CheckFailed bool
	TimedOut    bool
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

// taskContractResultBudget bounds how much of the child's final result is fed
// to the verifier, in runes. The full result can be tens of thousands of runes
// (a whole review report); the verifier only needs enough to judge shape, and
// an unbounded prompt makes the single verification call needlessly slow. When
// the result exceeds the budget the head and
// tail are kept (reports front-load scope and back-load conclusions) with an
// explicit, machine-recognizable marker in between. The marker is spelled out
// in task_verifier.txt so the verifier does not misread the elision itself as a
// truncated result.
const taskContractResultBudget = 12000

// verifierTruncationMarker is inserted between the kept head and tail of an
// oversized result. Kept in sync with the instruction in task_verifier.txt.
const verifierTruncationMarker = "\n\n[... verification input truncated; middle of the result omitted ...]\n\n"

// truncateVerifierResult bounds the result handed to the verifier. It is a
// no-op for results within budget.
func truncateVerifierResult(result string) string {
	runes := []rune(result)
	if len(runes) <= taskContractResultBudget {
		return result
	}
	// Keep a larger head than tail: contracts overwhelmingly ask about the
	// shape of the front matter (report sections, file lists, findings), and
	// the tail is only kept so a closing summary is still visible.
	headLen := taskContractResultBudget * 3 / 4
	tailLen := taskContractResultBudget - headLen
	head := string(runes[:headLen])
	tail := string(runes[len(runes)-tailLen:])
	return head + verifierTruncationMarker + tail
}

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
		// Inherit the main conversation identity (opencode* request affinity).
		return a.bindOpenCodeSessionID(client)
	}
	return a.client
}

// verifierContext returns the context for one verification call. Verification
// is bounded exactly like every other LLM call in the process — by the client's
// own pre-stream timeout (llmRequestTimeout) and the per-read stream idle
// watchdog — and NOT by a hardcoded total-call deadline.
//
// There is deliberately no default wall-clock cap. A former 30s constant
// abandoned verifier calls that were merely slow (the dominant cause of
// spurious "contract ✗" badges), and because LLMClient.Chat takes no context it
// could not even cancel the underlying request — it just abandoned a live
// goroutine and recorded a false check failure. A total-call deadline is applied
// only when the caller explicitly configured one (Agent.RequestTimeout, e.g.
// headless `ocode run`).
func verifierContext(a *Agent) (context.Context, context.CancelFunc) {
	if a != nil && a.RequestTimeout > 0 {
		return context.WithTimeout(context.Background(), a.RequestTimeout)
	}
	return context.Background(), func() {}
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
		"{result}", truncateVerifierResult(result),
	).Replace(taskVerifierPromptText)

	ctx, cancel := verifierContext(t.mainAgent)
	defer cancel()

	respCh := make(chan struct {
		msg *Message
		err error
	}, 1)
	crashguard.Go(func() {
		msg, err := client.Chat([]Message{{Role: "user", Content: prompt}}, nil)
		respCh <- struct {
			msg *Message
			err error
		}{msg: msg, err: err}
	})

	var text string
	select {
	case <-ctx.Done():
		// Only reachable when Agent.RequestTimeout was explicitly set; there is
		// no default deadline, so this is not the normal path. Report it as a
		// timeout specifically (TimedOut) so callers can say "timed out" rather
		// than a generic verification failure.
		deficiency := "verification cancelled"
		if t.mainAgent != nil && t.mainAgent.RequestTimeout > 0 {
			deficiency = fmt.Sprintf("timed out after %s (Agent.RequestTimeout)", t.mainAgent.RequestTimeout)
		}
		t.mainAgent.emitDebug("CONTRACT", fmt.Sprintf("verification aborted: %v", ctx.Err()))
		return contractVerdict{CheckFailed: true, TimedOut: true, Deficiency: deficiency}
	case out := <-respCh:
		if out.err != nil {
			t.mainAgent.emitDebug("CONTRACT", fmt.Sprintf("verification call failed: %v", out.err))
			return contractVerdict{CheckFailed: true, Deficiency: "contract verification failed: " + out.err.Error()}
		}
		if out.msg == nil || strings.TrimSpace(out.msg.Content) == "" {
			return contractVerdict{CheckFailed: true, Deficiency: "contract verification returned an empty response"}
		}
		text = out.msg.Content
	}

	satisfied, deficiency, perr := parseVerdictResponse(text)
	if perr != nil {
		t.mainAgent.emitDebug("CONTRACT", fmt.Sprintf("%v (raw: %.200s)", perr, text))
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

// contractUnverifiedPrefix builds the prefix for a result whose contract could
// NOT be checked because the verification machinery itself failed (timeout, LLM
// error, empty or malformed verdict). It is deliberately distinct from
// contractWarningPrefix: the child did not fail the contract, the check never
// produced a judgement. Wording matters — the parent model reads this text and
// must not downgrade a possibly-fine result.
func contractUnverifiedPrefix(contract, reason string) string {
	return fmt.Sprintf("⚠ Output contract NOT verified: %q\nVerification could not run: %s\n\nThis is a verification failure, not a contract failure — the sub-agent's result was never judged. Its full result follows (treat as unverified):", contract, reason)
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
		// failure loudly and keep the partial result visible. This is NOT a
		// contract failure — record CheckFailed so every surface can say so.
		// A caller-configured timeout is reported as a timeout, not wrapped in
		// the generic "verification failed" wording.
		deficiency := "verification failed: " + v.Deficiency
		if v.TimedOut {
			deficiency = v.Deficiency
		}
		if run != nil {
			run.SetContractVerdict(ContractOutcome{CheckFailed: true, TimedOut: v.TimedOut, Deficiency: deficiency})
		}
		return contractUnverifiedPrefix(t.contract, deficiency) + "\n\n" + result, resp, v
	}
	if v.Satisfied {
		if run != nil {
			run.SetContractVerdict(ContractOutcome{Satisfied: true})
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
			run.SetContractVerdict(ContractOutcome{CheckFailed: true, Deficiency: deficiency})
		}
		return contractUnverifiedPrefix(t.contract, deficiency) + "\n\n" + result, resp, contractVerdict{CheckFailed: true, Deficiency: deficiency}
	}
	result = result2
	resp = resp2

	v2 := t.verifyContract(t.contract, result)
	satisfied := !v2.CheckFailed && v2.Satisfied
	deficiency := v2.Deficiency
	if v2.CheckFailed {
		// Re-verification broke after a successful retry. The child did not
		// demonstrably meet the contract, but the check also did not complete;
		// record it as a check failure so surfaces say "unverified", not "failed".
		// A timeout is kept as a timeout rather than a generic re-verify failure.
		deficiency = "re-verification failed after retry: " + v2.Deficiency
		if v2.TimedOut {
			deficiency = v2.Deficiency
		}
		if run != nil {
			run.SetContractVerdict(ContractOutcome{CheckFailed: true, TimedOut: v2.TimedOut, Deficiency: deficiency})
		}
		return contractUnverifiedPrefix(t.contract, deficiency) + "\n\n" + result, resp, contractVerdict{CheckFailed: true, TimedOut: v2.TimedOut, Deficiency: deficiency}
	}
	if run != nil {
		run.SetContractVerdict(ContractOutcome{Satisfied: satisfied, Deficiency: deficiency})
	}
	if satisfied {
		return result, resp, v2
	}
	return contractWarningPrefix(t.contract, deficiency) + "\n\n" + result, resp, contractVerdict{Satisfied: false, Deficiency: deficiency}
}
