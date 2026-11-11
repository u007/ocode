package agent

import (
	"strings"
	"testing"
)

func TestAutoContinueTypesafeInvalidConfidenceFailsClosed(t *testing.T) {
	reply := `{"model":"jev-latest","answers":{"verdict":{"type":"choice","choice":"continue","probabilities":{"continue":0.5,"end":0.5},"confidence":1.5},"reason":{"type":"choice","choice":"mid_task","probabilities":{},"confidence":0.8}},"usage":{"input_tokens":7,"output_tokens":2}}`
	a, _ := newAutoContinueTypesafeJudge(t, reply)
	resume, detail, err := judgeSync(a, []Message{{Role: "assistant", Content: "x"}}, nil)
	if err != nil || resume {
		t.Fatalf("expected no resume, got resume=%v err=%v", resume, err)
	}
	if !strings.Contains(detail, "invalid verdict") {
		t.Fatalf("detail should report invalid verdict: %q", detail)
	}
}

func TestAutoContinueTypesafeUnnormalisedProbabilitiesFailClosed(t *testing.T) {
	reply := `{"model":"jev-latest","answers":{"verdict":{"type":"choice","choice":"continue","probabilities":{"continue":0.5,"end":0.6},"confidence":0.9},"reason":{"type":"choice","choice":"mid_task","probabilities":{},"confidence":0.8}},"usage":{"input_tokens":7,"output_tokens":2}}`
	a, _ := newAutoContinueTypesafeJudge(t, reply)
	resume, detail, err := judgeSync(a, []Message{{Role: "assistant", Content: "x"}}, nil)
	if err != nil || resume {
		t.Fatalf("expected no resume, got resume=%v err=%v", resume, err)
	}
	if !strings.Contains(detail, "invalid verdict") {
		t.Fatalf("detail should report invalid verdict: %q", detail)
	}
}

func TestAutoContinueTypesafeInvalidReasonKeyFailsClosed(t *testing.T) {
	reply := `{"model":"jev-latest","answers":{"verdict":{"type":"choice","choice":"continue","probabilities":{"continue":0.5,"end":0.5},"confidence":0.9},"reason":{"type":"choice","choice":"bogus_reason","probabilities":{},"confidence":0.8}},"usage":{"input_tokens":7,"output_tokens":2}}`
	a, _ := newAutoContinueTypesafeJudge(t, reply)
	resume, detail, err := judgeSync(a, []Message{{Role: "assistant", Content: "x"}}, nil)
	if err != nil || resume {
		t.Fatalf("expected no resume, got resume=%v err=%v", resume, err)
	}
	if !strings.Contains(detail, "invalid reason") {
		t.Fatalf("detail should report invalid reason: %q", detail)
	}
}
