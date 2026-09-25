package agent

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestChatWithContextPerCallDeltaCallbackTakesPrecedence(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	stubLLMHTTP(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return statusResponse(http.StatusOK, openAIChatOKStream), nil
	}))

	client := &GenericClient{Provider: "opencode", Model: "gpt-test", BaseURL: "https://example.test/v1"}
	var sharedCalls atomic.Int32
	var perCallCalls atomic.Int32
	client.SetOnDelta(func(string, string) { sharedCalls.Add(1) })

	ctx := withDeltaCallback(context.Background(), func(string, string) { perCallCalls.Add(1) })
	done := make(chan error, 1)
	go func() {
		_, err := client.ChatWithContext(ctx, []Message{{Role: "user", Content: "hi"}}, nil)
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("provider request did not start")
	}
	// Simulate another caller clearing the shared hook while this request is
	// waiting for response headers. The per-call compaction hook must survive.
	client.SetOnDelta(nil)
	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ChatWithContext failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ChatWithContext did not finish")
	}
	if got := perCallCalls.Load(); got == 0 {
		t.Fatal("per-call delta callback received no streamed delta")
	}
	if got := sharedCalls.Load(); got != 0 {
		t.Fatalf("shared delta callback received %d deltas; per-call callback must be exclusive", got)
	}
}
