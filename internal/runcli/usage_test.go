package runcli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUsageTotals_recordSumsAndCountsCalls(t *testing.T) {
	var totals usageTotals
	totals.record(100, 20)
	totals.record(250, 35)

	in, out, calls := totals.snapshot()
	if in != 350 {
		t.Errorf("input tokens = %d, want 350", in)
	}
	if out != 55 {
		t.Errorf("output tokens = %d, want 55", out)
	}
	if calls != 2 {
		t.Errorf("model calls = %d, want 2", calls)
	}
}

func TestUsageTotals_recordIsConcurrencySafe(t *testing.T) {
	var totals usageTotals
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			totals.record(10, 1)
		}()
	}
	wg.Wait()

	in, out, calls := totals.snapshot()
	if in != 1000 || out != 100 || calls != 100 {
		t.Errorf("got in=%d out=%d calls=%d, want 1000/100/100", in, out, calls)
	}
}

func TestEmitUsageEvent_emitsSingleJSONLine(t *testing.T) {
	var totals usageTotals
	totals.record(1200, 300)
	totals.record(800, 150)

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		outC <- buf.String()
	}()

	emitErr := emitUsageEvent("sess-9", "opencode-go/deepseek-v4-flash", &totals)

	w.Close()
	os.Stdout = old
	out := <-outC

	if emitErr != nil {
		t.Fatalf("emitUsageEvent: %v", emitErr)
	}

	var ev struct {
		Type         string `json:"type"`
		SessionID    string `json:"sessionID"`
		InputTokens  int64  `json:"input_tokens"`
		OutputTokens int64  `json:"output_tokens"`
		TotalTokens  int64  `json:"total_tokens"`
		ModelCalls   int    `json:"model_calls"`
		Model        string `json:"model"`
	}
	if err := json.Unmarshal([]byte(out), &ev); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if ev.Type != "usage" {
		t.Errorf("type = %q, want usage", ev.Type)
	}
	if ev.SessionID != "sess-9" {
		t.Errorf("sessionID = %q, want sess-9", ev.SessionID)
	}
	if ev.InputTokens != 2000 {
		t.Errorf("input_tokens = %d, want 2000", ev.InputTokens)
	}
	if ev.OutputTokens != 450 {
		t.Errorf("output_tokens = %d, want 450", ev.OutputTokens)
	}
	if ev.TotalTokens != 2450 {
		t.Errorf("total_tokens = %d, want 2450", ev.TotalTokens)
	}
	if ev.ModelCalls != 2 {
		t.Errorf("model_calls = %d, want 2", ev.ModelCalls)
	}
	if ev.Model != "opencode-go/deepseek-v4-flash" {
		t.Errorf("model = %q, want opencode-go/deepseek-v4-flash", ev.Model)
	}
}

func TestOutputSummary_printsTokenLineWhenCallsRecorded(t *testing.T) {
	var totals usageTotals
	totals.record(500, 75)

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		outC <- buf.String()
	}()

	sumErr := outputSummary(nil, "", "", time.Now(), &totals)

	w.Close()
	os.Stdout = old
	out := <-outC

	if sumErr != nil {
		t.Fatalf("outputSummary: %v", sumErr)
	}
	if !strings.Contains(out, "500 in / 75 out (1 calls)") {
		t.Errorf("summary missing token line, got:\n%s", out)
	}
}

func TestOutputSummary_omitsTokenLineWhenNoCalls(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		outC <- buf.String()
	}()

	sumErr := outputSummary(nil, "", "", time.Now(), &usageTotals{})

	w.Close()
	os.Stdout = old
	out := <-outC

	if sumErr != nil {
		t.Fatalf("outputSummary: %v", sumErr)
	}
	if strings.Contains(out, "Tokens:") {
		t.Errorf("summary should omit token line with no calls, got:\n%s", out)
	}
}
