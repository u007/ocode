package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

// slowBatchClient makes each summary call take most of the configured idle
// window. The old implementation shared one context across all batches, so a
// slow second batch inherited the deadline created for the first batch.
type slowBatchClient struct {
	mu        sync.Mutex
	calls     int
	delay     time.Duration
	responses []string
}

func (c *slowBatchClient) Chat(_ []Message, _ []map[string]interface{}) (*Message, error) {
	c.mu.Lock()
	index := c.calls
	c.calls++
	c.mu.Unlock()
	time.Sleep(c.delay)
	content := validSummaryText(fmt.Sprintf("batch%d", index+1))
	if index < len(c.responses) {
		content = c.responses[index]
	}
	return &Message{Role: "assistant", Content: content}, nil
}

func (c *slowBatchClient) GetProvider() string { return "mock" }
func (c *slowBatchClient) GetModel() string    { return "mock-compact" }

func TestRunCompactGivesEachBatchFreshTimeout(t *testing.T) {
	client := &slowBatchClient{delay: 1200 * time.Millisecond}
	a := &Agent{client: client}
	rt := compactRuntime{
		Enabled:               true,
		KeepRecentTurns:       1,
		SummaryTimeoutSeconds: 2,
		SummaryMaxRetries:     0,
		MaxSummaryInputTokens: 3000,
	}
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "original ask"},
	}
	for i := 0; i < 6; i++ {
		msgs = append(msgs, Message{Role: "assistant", Content: strings.Repeat("x", 2000)})
	}
	msgs = append(msgs,
		Message{Role: "user", Content: "recent tail"},
		Message{Role: "assistant", Content: "tail response"},
	)

	result := a.runCompact(msgs, rt, "", false)
	if !result.OK {
		t.Fatalf("expected each batch to receive a fresh timeout, got error: %v", result.Err)
	}
}

func TestRunCompactHandlesSynthetic400kTokenMiddle(t *testing.T) {
	client := &slowBatchClient{}
	a := &Agent{client: client}
	rt := compactRuntime{
		Enabled:                         true,
		KeepRecentTurns:                 1,
		SummaryTimeoutSeconds:           2,
		SummaryFirstTokenTimeoutSeconds: 2,
		SummaryMaxRetries:               0,
		MaxSummaryInputTokens:           50000,
	}
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "original ask"},
	}
	// 500 * 4000 characters is roughly 500k estimated tokens with the
	// conservative four-chars-per-token heuristic, well above the reported
	// 400k failure range.
	for i := 0; i < 500; i++ {
		msgs = append(msgs, Message{Role: "assistant", Content: strings.Repeat("x", 4000)})
	}
	msgs = append(msgs,
		Message{Role: "user", Content: "recent tail"},
		Message{Role: "assistant", Content: "tail response"},
	)

	result := a.runCompact(msgs, rt, "", false)
	if !result.OK {
		t.Fatalf("synthetic 400k-token compaction failed: %v", result.Err)
	}
	client.mu.Lock()
	calls := client.calls
	client.mu.Unlock()
	if calls < 5 {
		t.Fatalf("large middle was not split into multiple summary batches: %d calls", calls)
	}
}

func TestInactivityContextGivesFirstTokenItsOwnWindow(t *testing.T) {
	ctx, cancel, reset := inactivityContextWithParent(t.Context(), 100*time.Millisecond, 300*time.Millisecond)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatalf("first-token window expired too early: %v", contextCause(ctx))
	case <-time.After(180 * time.Millisecond):
	}

	reset()
	select {
	case <-ctx.Done():
		t.Fatalf("idle window should start only after the first token: %v", contextCause(ctx))
	case <-time.After(50 * time.Millisecond):
	}

	select {
	case <-ctx.Done():
		if !errors.Is(contextCause(ctx), ErrCompactionTimeout) {
			t.Fatalf("context ended with %v, want ErrCompactionTimeout", contextCause(ctx))
		}
	case <-time.After(180 * time.Millisecond):
		t.Fatal("idle window did not expire after the first token")
	}
}

func TestRunCompactOverallCapStopsAnActiveBatch(t *testing.T) {
	oldCap := compactOverallCap
	compactOverallCap = 100 * time.Millisecond
	defer func() { compactOverallCap = oldCap }()

	pipeReader, pipeWriter := io.Pipe()
	writerDone := make(chan struct{})
	stubLLMHTTP(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestDone := req.Context().Done()
		go func() {
			defer close(writerDone)
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if _, err := io.WriteString(pipeWriter, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"); err != nil {
						return
					}
				case <-time.After(1 * time.Second):
					return
				case <-requestDone:
					_ = pipeWriter.CloseWithError(context.Canceled)
					return
				}
			}
		}()
		return &http.Response{StatusCode: http.StatusOK, Body: pipeReader, Header: make(http.Header)}, nil
	}))

	client := &GenericClient{Provider: "opencode", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	a := &Agent{client: client}
	rt := compactRuntime{
		Enabled:                         true,
		KeepRecentTurns:                 1,
		SummaryTimeoutSeconds:           1,
		SummaryFirstTokenTimeoutSeconds: 1,
		MaxSummaryInputTokens:           50000,
	}
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "ask"},
		{Role: "assistant", Content: strings.Repeat("x", 1000)},
		{Role: "user", Content: "tail"},
	}
	before := append([]Message(nil), msgs...)

	result := a.runCompact(msgs, rt, "", false)
	_ = pipeReader.Close()
	_ = pipeWriter.Close()
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("summary stream writer did not stop after the overall cap")
	}
	if result.OK {
		t.Fatal("overall cap unexpectedly produced a successful compaction")
	}
	if !errors.Is(result.Err, ErrCompactionTimeout) {
		t.Fatalf("overall cap error = %v, want ErrCompactionTimeout", result.Err)
	}
	if len(msgs) != len(before) || msgs[2].Content != before[2].Content {
		t.Fatal("failed compaction mutated the input transcript")
	}
}

func TestResolveCompactRuntimeNormalizesFirstTokenTimeout(t *testing.T) {
	client := &slowBatchClient{}
	a := &Agent{client: client, config: &config.Config{}}
	a.config.Ocode.Compact.Enabled = true
	a.config.Ocode.Compact.SummaryTimeoutSeconds = 2
	a.config.Ocode.Compact.SummaryFirstTokenTimeoutSeconds = 0

	rt := a.resolveCompactRuntime(true)
	if got := rt.SummaryFirstTokenTimeoutSeconds; got != 300 {
		t.Fatalf("runtime first-token timeout = %d, want 300 for <=0", got)
	}

	a.config.Ocode.Compact.SummaryFirstTokenTimeoutSeconds = 45
	rt = a.resolveCompactRuntime(true)
	if got := rt.SummaryFirstTokenTimeoutSeconds; got != 45 {
		t.Fatalf("runtime first-token timeout = %d, want explicit 45", got)
	}
}
