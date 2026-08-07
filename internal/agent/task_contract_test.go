package agent

import (
	"errors"
	"strings"
	"testing"
)

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
