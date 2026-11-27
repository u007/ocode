package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// digestLoopStep answers one model request. It receives the request context so
// a step can block until the per-call deadline fires.
type digestLoopStep func(ctx context.Context, msgs []Message) (*Message, error)

// digestLoopClient is a scripted tool-calling side client. Each request consumes
// the next step; the requests it saw are recorded for assertions.
type digestLoopClient struct {
	provider string
	model    string
	steps    []digestLoopStep

	mu       sync.Mutex
	requests []digestLoopRequest
}

type digestLoopRequest struct {
	messages  []Message
	toolNames []string
}

func (c *digestLoopClient) GetProvider() string {
	if c.provider != "" {
		return c.provider
	}
	return "testprov"
}

func (c *digestLoopClient) GetModel() string {
	if c.model != "" {
		return c.model
	}
	return "loop-model"
}

func (c *digestLoopClient) Chat(msgs []Message, tools []map[string]interface{}) (*Message, error) {
	return c.ChatWithContext(context.Background(), msgs, tools)
}

func (c *digestLoopClient) ChatWithContext(ctx context.Context, msgs []Message, tools []map[string]interface{}) (*Message, error) {
	c.mu.Lock()
	idx := len(c.requests)
	c.requests = append(c.requests, digestLoopRequest{
		messages:  append([]Message(nil), msgs...),
		toolNames: digestToolNames(tools),
	})
	c.mu.Unlock()
	if idx >= len(c.steps) {
		return nil, errors.New("digestLoopClient: unexpected model request")
	}
	return c.steps[idx](ctx, msgs)
}

func (c *digestLoopClient) requestCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.requests)
}

func digestToolNames(tools []map[string]interface{}) []string {
	names := make([]string, 0, len(tools))
	for _, def := range tools {
		if name, ok := def["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

func digestCall(id, name, args string) ToolCall {
	var c ToolCall
	c.ID = id
	c.Type = "function"
	c.Function.Name = name
	c.Function.Arguments = args
	return c
}

// digestCalls is a step that asks for the given tool calls in one response.
func digestCalls(calls ...ToolCall) digestLoopStep {
	return func(context.Context, []Message) (*Message, error) {
		return &Message{Role: "assistant", ToolCalls: calls}, nil
	}
}

// digestFinal is a step that answers with plain Markdown and no tool calls.
func digestFinal(content string) digestLoopStep {
	return func(context.Context, []Message) (*Message, error) {
		return &Message{Role: "assistant", Content: content}, nil
	}
}

// digestFakeTool is a registered tool with a scripted result. It counts its
// executions so a test can prove a call was or was not dispatched.
type digestFakeTool struct {
	name string
	run  func(args json.RawMessage) (string, error)

	mu    sync.Mutex
	calls int
}

func (f *digestFakeTool) Name() string        { return f.name }
func (f *digestFakeTool) Description() string { return "fake " + f.name }
func (f *digestFakeTool) Parallel() bool      { return false }
func (f *digestFakeTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        f.name,
		"description": "fake " + f.name,
		"parameters":  map[string]interface{}{"type": "object"},
	}
}

func (f *digestFakeTool) Execute(args json.RawMessage) (string, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.run == nil {
		return "ok", nil
	}
	return f.run(args)
}

func (f *digestFakeTool) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func digestLoopGuard() toolDigestContextGuard {
	return toolDigestContextGuard{
		Model:           "loop-model",
		CharsPerToken:   4,
		ContextWindow:   200000,
		ResponseReserve: 8192,
	}
}

func digestLoopPayload(t *testing.T, args string) *toolDigestPayload {
	t.Helper()
	p, err := buildToolDigestPayload(ToolCallDigestInput{
		Tool:    "bash",
		Command: "ls -la",
		Args:    json.RawMessage(args),
	}, digestTestIdentity)
	if err != nil {
		t.Fatalf("buildToolDigestPayload: %v", err)
	}
	return p
}

// digestLoopToolbox builds the runner toolbox the way production does, then
// registers any extra fake inspection tools on it.
func digestLoopToolbox(t *testing.T, payload *toolDigestPayload, guard toolDigestContextGuard, extra ...*digestFakeTool) *toolDigestToolbox {
	t.Helper()
	// Loop tests drive fake inspection tools, so the box holds only the real
	// call_parameters tool; the production catalog is TestInspectShell_ToolCatalog's job.
	box := &toolDigestToolbox{}
	box.add(newToolDigestParamsTool(payload, guard))
	for _, f := range extra {
		box.add(f)
	}
	return box
}

type digestUsageRecord struct {
	prompt, completion, cacheRead, cacheWrite int64
}

func digestUsageRecorder() (func(int64, int64, int64, int64, *float64), *[]digestUsageRecord) {
	var mu sync.Mutex
	var got []digestUsageRecord
	return func(p, c, cr, cw int64, _ *float64) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, digestUsageRecord{p, c, cr, cw})
	}, &got
}

func digestToolMessage(t *testing.T, msgs []Message, id string) string {
	t.Helper()
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolID == id {
			return m.Content
		}
	}
	t.Fatalf("no tool message for call %q", id)
	return ""
}

func digestPtr(v int64) *int64 { return &v }

// withModelCallTimeout shortens the per-call model deadline for one test.
func withModelCallTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := toolDigestModelCallTimeout
	toolDigestModelCallTimeout = d
	t.Cleanup(func() { toolDigestModelCallTimeout = prev })
}

// TestRunToolDigestLoop_MultiStepToolUse: read, then grep, then the final
// Markdown, in three model calls. Headings, lists and fences survive, and the
// evidence is secret-masked before the next request.
func TestRunToolDigestLoop_MultiStepToolUse(t *testing.T) {
	payload := digestLoopPayload(t, `{"command":"ls -la"}`)
	guard := digestLoopGuard()
	read := &digestFakeTool{name: "read", run: func(json.RawMessage) (string, error) {
		return "token=AKIAIOSFODNN7EXAMPLE\nfunc main() {}", nil
	}}
	grep := &digestFakeTool{name: "grep", run: func(json.RawMessage) (string, error) { return "main.go:1: func main", nil }}
	box := digestLoopToolbox(t, payload, guard, read, grep)

	final := "## What it does\n\n- Lists the directory.\n- Reads nothing else.\n\n```text\nls -la\n```\n"
	client := &digestLoopClient{steps: []digestLoopStep{
		digestCalls(digestCall("c1", "read", `{"path":"main.go"}`)),
		digestCalls(digestCall("c2", "grep", `{"pattern":"main"}`)),
		digestFinal(final),
	}}
	usage, _ := digestUsageRecorder()

	got, err := runToolDigestLoop(context.Background(), client, box, "SYSTEM", []Message{{Role: "user", Content: "manifest"}}, guard, usage, ToolCallDigestInput{Tool: "bash"})
	if err != nil {
		t.Fatalf("runToolDigestLoop: %v", err)
	}
	if got.Summary != strings.TrimSpace(final) {
		t.Fatalf("summary lost Markdown structure:\n got: %q\nwant: %q", got.Summary, strings.TrimSpace(final))
	}
	if got.Model != "testprov/loop-model" {
		t.Fatalf("model = %q", got.Model)
	}
	if n := client.requestCount(); n != 3 {
		t.Fatalf("model requests = %d, want 3", n)
	}
	if read.count() != 1 || grep.count() != 1 {
		t.Fatalf("read ran %d times, grep %d; want 1 each", read.count(), grep.count())
	}
	last := client.requests[2].messages
	readResult := digestToolMessage(t, last, "c1")
	if strings.Contains(readResult, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("evidence reached the model unmasked: %q", readResult)
	}
	if !strings.Contains(readResult, "func main") {
		t.Fatalf("evidence body missing: %q", readResult)
	}
}

// TestRunToolDigestLoop_MultiPageSequential: two pages in one response run in
// call order, and a third page in a later response reaches a later offset. The
// concatenated pages are byte-identical to the canonical payload prefix.
func TestRunToolDigestLoop_MultiPageSequential(t *testing.T) {
	big := strings.Repeat("abcdefghij", 4096) // 40 KiB of ASCII, so three full pages exist
	payload := digestLoopPayload(t, `{"blob":"`+big+`"}`)
	guard := digestLoopGuard()
	box := digestLoopToolbox(t, payload, guard)

	pageArgs := func(off, n int) string {
		b, _ := json.Marshal(map[string]int{"offset_bytes": off, "max_bytes": n})
		return string(b)
	}
	client := &digestLoopClient{steps: []digestLoopStep{
		digestCalls(
			digestCall("p1", "call_parameters", pageArgs(0, 8192)),
			digestCall("p2", "call_parameters", pageArgs(8192, 8192)),
		),
		digestCalls(digestCall("p3", "call_parameters", pageArgs(16384, 8192))),
		digestFinal("## What it does\n\nPages were read in order."),
	}}
	usage, _ := digestUsageRecorder()

	if _, err := runToolDigestLoop(context.Background(), client, box, "SYSTEM", []Message{{Role: "user", Content: "manifest"}}, guard, usage, ToolCallDigestInput{Tool: "bash"}); err != nil {
		t.Fatalf("runToolDigestLoop: %v", err)
	}
	var joined strings.Builder
	wantOffsets := []int{0, 8192, 16384}
	for i, id := range []string{"p1", "p2", "p3"} {
		var page toolDigestPageResult
		if err := json.Unmarshal([]byte(digestToolMessage(t, client.requests[len(client.requests)-1].messages, id)), &page); err != nil {
			t.Fatalf("page %s is not JSON: %v", id, err)
		}
		if page.OffsetBytes != wantOffsets[i] {
			t.Fatalf("page %s offset = %d, want %d", id, page.OffsetBytes, wantOffsets[i])
		}
		if page.TotalBytes != len(payload.canonical) || page.SHA256 != payload.sha256 {
			t.Fatalf("page %s manifest mismatch: total=%d sha=%s", id, page.TotalBytes, page.SHA256)
		}
		joined.WriteString(page.Text)
	}
	if joined.String() != string(payload.canonical[:24576]) {
		t.Fatal("sequential pages are not an exact window of the canonical payload")
	}
}

// TestRunToolDigestLoop_ModelTimeoutNotCompletedStep: a model call past its
// deadline is an explicit error, is not a completed step, and records no usage.
func TestRunToolDigestLoop_ModelTimeoutNotCompletedStep(t *testing.T) {
	withModelCallTimeout(t, 20*time.Millisecond)
	payload := digestLoopPayload(t, `{"command":"ls"}`)
	guard := digestLoopGuard()
	box := digestLoopToolbox(t, payload, guard)
	client := &digestLoopClient{steps: []digestLoopStep{
		func(ctx context.Context, _ []Message) (*Message, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
		digestFinal("must not be reached"),
	}}
	usage, got := digestUsageRecorder()

	_, err := runToolDigestLoop(context.Background(), client, box, "SYSTEM", []Message{{Role: "user", Content: "m"}}, guard, usage, ToolCallDigestInput{Tool: "bash"})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a model-call timeout", err)
	}
	if n := client.requestCount(); n != 1 {
		t.Fatalf("requests = %d, want 1: a timed-out step must not be followed by another", n)
	}
	if len(*got) != 0 {
		t.Fatalf("usage recorded for a timed-out call: %+v", *got)
	}
}

// TestRunToolDigestLoop_ToolCatalog: the model is offered exactly the toolbox
// definitions, and a name outside that list is refused as a tool error.
func TestRunToolDigestLoop_ToolCatalog(t *testing.T) {
	payload := digestLoopPayload(t, `{"command":"ls"}`)
	guard := digestLoopGuard()
	read := &digestFakeTool{name: "read"}
	grep := &digestFakeTool{name: "grep"}
	box := digestLoopToolbox(t, payload, guard, read, grep)
	want := []string{"call_parameters", "read", "grep"}

	client := &digestLoopClient{steps: []digestLoopStep{
		digestCalls(digestCall("x1", "write", `{"path":"a"}`)),
		digestFinal("## What it does\n\nNothing ran."),
	}}
	usage, _ := digestUsageRecorder()
	if _, err := runToolDigestLoop(context.Background(), client, box, "SYSTEM", []Message{{Role: "user", Content: "m"}}, guard, usage, ToolCallDigestInput{Tool: "bash"}); err != nil {
		t.Fatalf("runToolDigestLoop: %v", err)
	}
	for i, req := range client.requests {
		if strings.Join(req.toolNames, ",") != strings.Join(want, ",") {
			t.Fatalf("request %d advertised %v, want %v", i+1, req.toolNames, want)
		}
	}
	if write := digestToolMessage(t, client.requests[1].messages, "x1"); !strings.Contains(write, "tool error") || !strings.Contains(write, `"write"`) {
		t.Fatalf("a call outside the catalog must come back as a tool error naming it, got %q", write)
	}
	if read.count()+grep.count() != 0 {
		t.Fatal("an unadvertised call must not dispatch any registered tool")
	}
}

// TestRunToolDigestLoop_ToolErrorsRecovered: unknown names, bad parameter
// arguments and unregistered inspection names each return a tool error and the
// loop continues to the final answer.
func TestRunToolDigestLoop_ToolErrorsRecovered(t *testing.T) {
	payload := digestLoopPayload(t, `{"command":"ls"}`)
	guard := digestLoopGuard()
	box := digestLoopToolbox(t, payload, guard)

	client := &digestLoopClient{steps: []digestLoopStep{
		digestCalls(
			digestCall("e1", "bash", `{"command":"rm -rf /"}`),
			digestCall("e2", "call_parameters", `{"offset_bytes":-1,"max_bytes":10}`),
			digestCall("e3", "read", `{"path":"x"}`),
		),
		digestFinal("## What it does\n\nRecovered from every tool error."),
	}}
	usage, _ := digestUsageRecorder()
	got, err := runToolDigestLoop(context.Background(), client, box, "SYSTEM", []Message{{Role: "user", Content: "m"}}, guard, usage, ToolCallDigestInput{Tool: "bash"})
	if err != nil {
		t.Fatalf("a tool error must not abort the run: %v", err)
	}
	if !strings.Contains(got.Summary, "Recovered") {
		t.Fatalf("summary = %q", got.Summary)
	}
	for _, id := range []string{"e1", "e2", "e3"} {
		if c := digestToolMessage(t, client.requests[1].messages, id); !strings.Contains(c, "tool error") {
			t.Fatalf("call %s result = %q, want a tool error", id, c)
		}
	}
}

// TestRunToolDigestLoop_FinalStepNudge: a model that inspects up to the
// second-to-last step is told to answer on the last one, and the run succeeds.
func TestRunToolDigestLoop_FinalStepNudge(t *testing.T) {
	payload := digestLoopPayload(t, `{"command":"ls"}`)
	guard := digestLoopGuard()
	box := digestLoopToolbox(t, payload, guard, &digestFakeTool{name: "read"})

	steps := make([]digestLoopStep, 0, toolDigestModelSteps)
	for i := 0; i < toolDigestModelSteps-1; i++ {
		steps = append(steps, digestCalls(digestCall("n"+string(rune('a'+i)), "read", `{}`)))
	}
	steps = append(steps, digestFinal("## What it does\n\nAnswered after the nudge."))
	client := &digestLoopClient{steps: steps}
	usage, _ := digestUsageRecorder()
	got, err := runToolDigestLoop(context.Background(), client, box, "SYSTEM", []Message{{Role: "user", Content: "m"}}, guard, usage, ToolCallDigestInput{Tool: "bash"})
	if err != nil {
		t.Fatalf("final-step nudge must let the model answer: %v", err)
	}
	if !strings.Contains(got.Summary, "Answered after the nudge") {
		t.Fatalf("summary = %q", got.Summary)
	}
	if n := client.requestCount(); n != toolDigestModelSteps {
		t.Fatalf("requests = %d, want %d", n, toolDigestModelSteps)
	}
	for i, req := range client.requests {
		last := req.messages[len(req.messages)-1]
		hasNudge := last.Role == "user" && strings.Contains(last.Content, "No more inspections")
		if i == toolDigestModelSteps-1 && !hasNudge {
			t.Fatalf("final request must end with the answer-only nudge; last = %+v", last)
		}
		if i < toolDigestModelSteps-1 && hasNudge {
			t.Fatalf("request %d must not carry the nudge", i+1)
		}
	}
}

// TestRunToolDigestLoop_StepAndInspectionExhaustion: all model steps without a
// final answer, and twelve inspection calls without one, are distinct errors.
func TestRunToolDigestLoop_StepAndInspectionExhaustion(t *testing.T) {
	guard := digestLoopGuard()

	t.Run("all steps", func(t *testing.T) {
		payload := digestLoopPayload(t, `{"command":"ls"}`)
		read := &digestFakeTool{name: "read"}
		box := digestLoopToolbox(t, payload, guard, read)
		var steps []digestLoopStep
		for i := 0; i < toolDigestModelSteps; i++ {
			steps = append(steps, digestCalls(digestCall("s"+string(rune('a'+i)), "read", `{}`)))
		}
		client := &digestLoopClient{steps: steps}
		usage, _ := digestUsageRecorder()
		_, err := runToolDigestLoop(context.Background(), client, box, "SYSTEM", []Message{{Role: "user", Content: "m"}}, guard, usage, ToolCallDigestInput{Tool: "bash"})
		if err == nil || !strings.Contains(err.Error(), "model steps") {
			t.Fatalf("err = %v, want the model-step limit", err)
		}
		if n := client.requestCount(); n != toolDigestModelSteps {
			t.Fatalf("requests = %d, want %d", n, toolDigestModelSteps)
		}
	})

	t.Run("twelve inspections", func(t *testing.T) {
		payload := digestLoopPayload(t, `{"command":"ls"}`)
		read := &digestFakeTool{name: "read"}
		box := digestLoopToolbox(t, payload, guard, read)
		seven := make([]ToolCall, 0, 7)
		for i := 0; i < 7; i++ {
			seven = append(seven, digestCall("a"+string(rune('0'+i)), "read", `{}`))
		}
		five := make([]ToolCall, 0, 5)
		for i := 0; i < 5; i++ {
			five = append(five, digestCall("b"+string(rune('0'+i)), "read", `{}`))
		}
		client := &digestLoopClient{steps: []digestLoopStep{
			digestCalls(seven...),
			digestCalls(five...),
			digestCalls(digestCall("c0", "read", `{}`)),
			digestFinal("## What it does\n\nAnswered from twelve inspections."),
		}}
		usage, _ := digestUsageRecorder()
		if _, err := runToolDigestLoop(context.Background(), client, box, "SYSTEM", []Message{{Role: "user", Content: "m"}}, guard, usage, ToolCallDigestInput{Tool: "bash"}); err != nil {
			t.Fatalf("runToolDigestLoop: %v; the 13th inspection must not end the run", err)
		}
		if read.count() != toolDigestInspectionCalls {
			t.Fatalf("read executed %d times, want %d", read.count(), toolDigestInspectionCalls)
		}
		if refused := digestToolMessage(t, client.requests[3].messages, "c0"); !strings.Contains(refused, "answer") {
			t.Fatalf("13th inspection was not refused with an answer-now result: %q", refused)
		}
	})
}

// TestRunToolDigestLoop_ContextBudgetRefusal: a page that does not fit is not
// appended and returns a tool error naming remaining_context_tokens and the
// uninspected ranges. A smaller window then succeeds. No request over budget
// is ever sent.
func TestRunToolDigestLoop_ContextBudgetRefusal(t *testing.T) {
	payload := digestLoopPayload(t, `{"command":"`+strings.Repeat("z", 20000)+`"}`)
	// At one character per token every message carries about 300 tokens of
	// structure overhead, so the window must hold the three-message exchange
	// that precedes the accepted page, but not an 8 KiB page on top of it.
	guard := toolDigestContextGuard{Model: "loop-model", CharsPerToken: 1, ContextWindow: 4000, ResponseReserve: 100}
	box := digestLoopToolbox(t, payload, guard)
	client := &digestLoopClient{steps: []digestLoopStep{
		digestCalls(digestCall("big", "call_parameters", `{"offset_bytes":0,"max_bytes":8192}`)),
		digestCalls(digestCall("small", "call_parameters", `{"offset_bytes":0,"max_bytes":512}`)),
		digestFinal("## Risks / unknowns\n\nOnly the first 512 bytes were inspected."),
	}}
	usage, _ := digestUsageRecorder()

	if _, err := runToolDigestLoop(context.Background(), client, box, "SYSTEM", []Message{{Role: "user", Content: "m"}}, guard, usage, ToolCallDigestInput{Tool: "bash"}); err != nil {
		t.Fatalf("runToolDigestLoop: %v", err)
	}
	refusal := digestToolMessage(t, client.requests[1].messages, "big")
	for _, want := range []string{"remaining_context_tokens", "uninspected", "smaller"} {
		if !strings.Contains(refusal, want) {
			t.Fatalf("refusal missing %q: %q", want, refusal)
		}
	}
	if strings.Contains(refusal, `"text"`) {
		t.Fatalf("an over-budget page was appended: %q", refusal)
	}
	for i, req := range client.requests {
		if err := guard.admit(req.messages); err != nil {
			t.Fatalf("request %d was over budget: %v", i+1, err)
		}
	}
}

// TestRunToolDigestLoop_DecisionOnlyModelRejected: a typesafe route is refused
// before any request is sent.
func TestRunToolDigestLoop_DecisionOnlyModelRejected(t *testing.T) {
	payload := digestLoopPayload(t, `{"command":"ls"}`)
	guard := digestLoopGuard()
	box := digestLoopToolbox(t, payload, guard)
	client := &digestLoopClient{provider: "typesafe", model: "jev-latest", steps: []digestLoopStep{digestFinal("x")}}
	usage, _ := digestUsageRecorder()

	_, err := runToolDigestLoop(context.Background(), client, box, "SYSTEM", []Message{{Role: "user", Content: "m"}}, guard, usage, ToolCallDigestInput{Tool: "bash"})
	if err == nil || !strings.Contains(err.Error(), "decision-only") {
		t.Fatalf("err = %v, want a decision-only rejection", err)
	}
	if n := client.requestCount(); n != 0 {
		t.Fatalf("a decision-only route sent %d requests", n)
	}
}

// TestRunToolDigestLoop_UsageOnlyAndNoTranscriptSideEffects: usage fires once
// per response, including responses that carry tool calls, and the caller's
// initial messages are never mutated.
func TestRunToolDigestLoop_UsageOnlyAndNoTranscriptSideEffects(t *testing.T) {
	payload := digestLoopPayload(t, `{"command":"ls"}`)
	guard := digestLoopGuard()
	read := &digestFakeTool{name: "read"}
	box := digestLoopToolbox(t, payload, guard, read)
	withUsage := func(m *Message, p, c int64) *Message {
		m.Usage = &TokenUsage{PromptTokens: digestPtr(p), CompletionTokens: digestPtr(c)}
		return m
	}
	client := &digestLoopClient{steps: []digestLoopStep{
		func(context.Context, []Message) (*Message, error) {
			return withUsage(&Message{Role: "assistant", ToolCalls: []ToolCall{digestCall("u1", "read", `{}`)}}, 11, 3), nil
		},
		func(context.Context, []Message) (*Message, error) {
			return withUsage(&Message{Role: "assistant", Content: "## What it does\n\nDone."}, 20, 7), nil
		},
	}}
	usage, got := digestUsageRecorder()
	initial := []Message{{Role: "user", Content: "manifest"}}

	if _, err := runToolDigestLoop(context.Background(), client, box, "SYSTEM", initial, guard, usage, ToolCallDigestInput{Tool: "bash"}); err != nil {
		t.Fatalf("runToolDigestLoop: %v", err)
	}
	if len(*got) != 2 || (*got)[0].prompt != 11 || (*got)[1].completion != 7 {
		t.Fatalf("usage records = %+v, want one per response", *got)
	}
	if len(initial) != 1 || initial[0].Content != "manifest" {
		t.Fatalf("initial messages were mutated: %+v", initial)
	}
}

// TestRunToolDigestLoop_EvidenceCaps: a 1 MiB result is capped to 8 KiB, and
// once the 40 KiB cumulative evidence is spent the next inspection is refused
// without being executed.
func TestRunToolDigestLoop_EvidenceCaps(t *testing.T) {
	payload := digestLoopPayload(t, `{"command":"ls"}`)
	guard := digestLoopGuard()
	huge := strings.Repeat("x", 1<<20)
	read := &digestFakeTool{name: "read", run: func(json.RawMessage) (string, error) { return huge, nil }}
	box := digestLoopToolbox(t, payload, guard, read)

	var six []ToolCall
	for i := 0; i < 6; i++ {
		six = append(six, digestCall("r"+string(rune('0'+i)), "read", `{}`))
	}
	client := &digestLoopClient{steps: []digestLoopStep{
		digestCalls(six...),
		digestFinal("## What it does\n\nEvidence was capped."),
	}}
	usage, _ := digestUsageRecorder()
	if _, err := runToolDigestLoop(context.Background(), client, box, "SYSTEM", []Message{{Role: "user", Content: "m"}}, guard, usage, ToolCallDigestInput{Tool: "bash"}); err != nil {
		t.Fatalf("runToolDigestLoop: %v", err)
	}
	first := digestToolMessage(t, client.requests[1].messages, "r0")
	if len(first) > toolDigestEvidenceResultBytes {
		t.Fatalf("one evidence result is %d bytes, over the %d cap", len(first), toolDigestEvidenceResultBytes)
	}
	if read.count() != 5 {
		t.Fatalf("read executed %d times, want 5 (40 KiB spent)", read.count())
	}
	if refused := digestToolMessage(t, client.requests[1].messages, "r5"); !strings.Contains(refused, "evidence") {
		t.Fatalf("sixth inspection was not refused on the evidence budget: %q", refused)
	}
}

// TestRunToolDigestLoop_ClosesToolbox: registered cleanups run exactly once on
// the success path and on the error path.
func TestRunToolDigestLoop_ClosesToolbox(t *testing.T) {
	guard := digestLoopGuard()
	cases := map[string]digestLoopStep{
		"success": digestFinal("## What it does\n\nFine."),
		"error": func(context.Context, []Message) (*Message, error) {
			return nil, errors.New("provider exploded")
		},
	}
	for name, step := range cases {
		t.Run(name, func(t *testing.T) {
			box := digestLoopToolbox(t, digestLoopPayload(t, `{"command":"ls"}`), guard)
			cleaned := 0
			box.addCleanup(func() { cleaned++ })
			client := &digestLoopClient{steps: []digestLoopStep{step}}
			usage, _ := digestUsageRecorder()
			_, _ = runToolDigestLoop(context.Background(), client, box, "SYSTEM", []Message{{Role: "user", Content: "m"}}, guard, usage, ToolCallDigestInput{Tool: "bash"})
			if cleaned != 1 {
				t.Fatalf("cleanup ran %d times, want exactly 1", cleaned)
			}
		})
	}
}

// TestSummarizeToolCallWithKey_MasksBeforeKey: two inputs that differ only in a
// secret mask to the same canonical payload, so they share one key and the
// second call is a cache hit. The raw secret never reaches the model.
func TestSummarizeToolCallWithKey_MasksBeforeKey(t *testing.T) {
	stub := &speechSummaryTestClient{reply: "## What it does\n\nExports a key."}
	a := newToolDigestTestAgent(t, stub)

	first := bashDigestInput("export OPENAI_API_KEY=sk-abcdefghijklmnopqrstuvwxyz0123456789 && ls")
	second := bashDigestInput("export OPENAI_API_KEY=sk-ZYXWVUTSRQPONMLKJIHGFEDCBA9876543210 && ls")
	if _, err := a.SummarizeToolCallWithKeyCtx(context.Background(), first, ""); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if strings.Contains(stub.gotUser, "sk-abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Fatal("raw secret reached the explaining model")
	}
	if _, err := a.SummarizeToolCallWithKeyCtx(context.Background(), second, ""); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if stub.calls != 1 {
		t.Fatalf("model calls = %d, want 1: masked-identical inputs must share a key", stub.calls)
	}
}
