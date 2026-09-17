package agent

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestVerifierContextNoDefaultDeadline locks in that verification is NOT bounded
// by a hardcoded total-call deadline (a former 30s constant abandoned slow but
// healthy verifier calls and recorded a false check failure). It is bounded like
// any other LLM call — client pre-stream timeout + stream idle watchdog — and
// only gains a total deadline when the caller explicitly set RequestTimeout.
func TestVerifierContextNoDefaultDeadline(t *testing.T) {
	ctx, cancel := verifierContext(&Agent{})
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("verifier must not impose a default total-call deadline")
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("default verifier context should be live, got %v", err)
	}

	ctx2, cancel2 := verifierContext(&Agent{RequestTimeout: 90 * time.Second})
	defer cancel2()
	d, ok := ctx2.Deadline()
	if !ok {
		t.Fatal("explicit Agent.RequestTimeout should bound the verifier")
	}
	if remaining := time.Until(d); remaining <= 0 || remaining > 90*time.Second {
		t.Fatalf("deadline = %v away, want ~90s", remaining)
	}

	ctx3, cancel3 := verifierContext(nil)
	defer cancel3()
	if _, ok := ctx3.Deadline(); ok {
		t.Fatal("nil agent should not produce a deadline")
	}
}

// verdictClient is a fake LLM client whose response is used as the verifier's
// output. Set callErr to simulate an LLM failure.
type verdictClient struct {
	resp    string
	callErr error
}

func (v *verdictClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	if v.callErr != nil {
		return nil, v.callErr
	}
	return &Message{Role: "assistant", Content: v.resp}, nil
}

func (v *verdictClient) GetProvider() string { return "mock" }
func (v *verdictClient) GetModel() string    { return "mock-model" }

func TestParseVerdictResponse(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		satisfied  bool
		deficiency string
		malformed  bool
	}{
		{"satisfied", "VERDICT: SATISFIED", true, "", false},
		{"not satisfied with reason", "VERDICT: NOT_SATISFIED\nDEFICIENCY: missing the file list", false, "missing the file list", false},
		{"not satisfied no reason", "VERDICT: NOT_SATISFIED", false, "", false},
		{"lowercase verdict", "verdict: satisfied", true, "", false},
		{"extra preamble", "Here is my assessment.\nVERDICT: NOT_SATISFIED\nDEFICIENCY: no patch included", false, "no patch included", false},
		{"no verdict line is malformed", "I think it looks fine", false, "", true},
		{"unknown verdict value is not satisfied", "VERDICT: MAYBE", false, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSatisfied, gotDeficiency, err := parseVerdictResponse(tt.text)
			if tt.malformed {
				if err == nil {
					t.Fatalf("expected malformed error, got nil (satisfied=%v)", gotSatisfied)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotSatisfied != tt.satisfied {
				t.Errorf("satisfied = %v, want %v", gotSatisfied, tt.satisfied)
			}
			if gotDeficiency != tt.deficiency {
				t.Errorf("deficiency = %q, want %q", gotDeficiency, tt.deficiency)
			}
		})
	}
}

func TestVerifyContract(t *testing.T) {
	tests := []struct {
		name        string
		resp        string
		callErr     error
		checkFailed bool
		satisfied   bool
		deficiency  string
	}{
		{"satisfied verdict", "VERDICT: SATISFIED", nil, false, true, ""},
		{"not satisfied with reason", "VERDICT: NOT_SATISFIED\nDEFICIENCY: missing file list", nil, false, false, "missing file list"},
		{"malformed verdict is check-failed, never satisfied", "gibberish", nil, true, false, ""},
		{"llm error is check-failed", "", errors.New("boom"), true, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskTool := TaskTool{
				mainAgent: &Agent{client: &verdictClient{resp: tt.resp, callErr: tt.callErr}},
			}
			v := taskTool.verifyContract("the full file list", "result text")
			if v.CheckFailed != tt.checkFailed {
				t.Errorf("CheckFailed = %v, want %v (verdict %+v)", v.CheckFailed, tt.checkFailed, v)
			}
			if v.CheckFailed {
				if v.Satisfied {
					t.Error("a check-failed verdict must never be satisfied")
				}
				return
			}
			if v.Satisfied != tt.satisfied {
				t.Errorf("Satisfied = %v, want %v", v.Satisfied, tt.satisfied)
			}
			if v.Deficiency != tt.deficiency {
				t.Errorf("Deficiency = %q, want %q", v.Deficiency, tt.deficiency)
			}
		})
	}
}

func TestContractRetryMsg(t *testing.T) {
	msg := contractRetryMsg("full file list", "only three files listed")
	if !strings.Contains(msg, "full file list") {
		t.Errorf("retry msg should name the contract, got %q", msg)
	}
	if !strings.Contains(msg, "only three files listed") {
		t.Errorf("retry msg should carry the deficiency, got %q", msg)
	}
}

// blockingVerdictClient sleeps in Chat for delay. LLMClient.Chat takes no
// context, so this is exactly the production shape: a caller-configured
// deadline must be detected by verifyContract's own select, not by cancelling
// the underlying call.
type blockingVerdictClient struct{ delay time.Duration }

func (b *blockingVerdictClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	time.Sleep(b.delay)
	return &Message{Role: "assistant", Content: "VERDICT: SATISFIED"}, nil
}
func (b *blockingVerdictClient) GetProvider() string { return "mock" }
func (b *blockingVerdictClient) GetModel() string    { return "mock-model" }

// TestVerifyContractReportsTimeoutWhenRequestTimeoutSet verifies that a
// caller-configured deadline (Agent.RequestTimeout) is returned as a TIMEOUT —
// CheckFailed + TimedOut with a timeout deficiency — not as a generic check
// failure, and promptly (the wrapper does not wait for the slow call).
func TestVerifyContractReportsTimeoutWhenRequestTimeoutSet(t *testing.T) {
	taskTool := TaskTool{
		mainAgent: &Agent{
			client:         &blockingVerdictClient{delay: 5 * time.Second},
			RequestTimeout: 40 * time.Millisecond,
		},
	}
	start := time.Now()
	v := taskTool.verifyContract("the full file list", "result text")
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("deadline not honoured: verifyContract took %s", elapsed)
	}
	if !v.CheckFailed || !v.TimedOut {
		t.Fatalf("verdict = %+v, want CheckFailed && TimedOut", v)
	}
	if v.Satisfied {
		t.Fatal("a timed-out verdict must never be satisfied")
	}
	if !strings.Contains(v.Deficiency, "timed out") {
		t.Fatalf("deficiency = %q, want a timeout statement", v.Deficiency)
	}
}

// TestContractVerdictLabelTimeout pins that status text reports a timeout as
// "timed out", not the generic "NOT verified".
func TestContractVerdictLabelTimeout(t *testing.T) {
	got := contractVerdictLabel(ContractOutcome{CheckFailed: true, TimedOut: true, Deficiency: "timed out after 5m0s (Agent.RequestTimeout)"})
	if got != "timed out" {
		t.Fatalf("label = %q, want %q", got, "timed out")
	}
	// A non-timeout check failure still says NOT verified, with the reason.
	got = contractVerdictLabel(ContractOutcome{CheckFailed: true, Deficiency: "verification failed: boom"})
	if !strings.HasPrefix(got, "NOT verified") || !strings.Contains(got, "boom") {
		t.Fatalf("label = %q, want NOT verified with reason", got)
	}
}
