package agent

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

func TestCanonicalToolCallKeyEquivalentJSON(t *testing.T) {
	first, ok := canonicalToolCallKey("read", json.RawMessage(`{"path":"x","line":1}`))
	if !ok {
		t.Fatal("first arguments should be valid")
	}
	second, ok := canonicalToolCallKey("read", json.RawMessage(" { \"line\": 1, \"path\": \"x\" } "))
	if !ok {
		t.Fatal("second arguments should be valid")
	}
	if first != second {
		t.Fatalf("equivalent arguments produced different keys: %q != %q", first, second)
	}
}

func TestCanonicalToolCallKeyRejectsTrailingAndDuplicateJSON(t *testing.T) {
	for _, raw := range []string{
		`{} {}`,
		`{"path":"x"} trailing`,
		`{"path":"x","path":"y"}`,
	} {
		if _, ok := canonicalToolCallKey("read", json.RawMessage(raw)); ok {
			t.Errorf("malformed or ambiguous arguments were canonicalized: %q", raw)
		}
	}
}

func TestCanonicalToolCallKeyDistinguishesToolsAndNumbers(t *testing.T) {
	read, ok := canonicalToolCallKey("read", json.RawMessage(`{"n":1}`))
	if !ok {
		t.Fatal("read arguments should be valid")
	}
	readFloat, ok := canonicalToolCallKey("read", json.RawMessage(`{"n":1.0}`))
	if !ok {
		t.Fatal("float arguments should be valid")
	}
	other, ok := canonicalToolCallKey("write", json.RawMessage(`{"n":1}`))
	if !ok {
		t.Fatal("write arguments should be valid")
	}
	if read == readFloat || read == other {
		t.Fatal("distinct numeric spelling or tool names were deduplicated")
	}
}
func TestActiveAgentDispatchesConcurrentReservationAndRelease(t *testing.T) {
	dispatches := newActiveAgentDispatches()
	const workers = 32
	key := activeAgentDispatchKey("general", "same prompt", "same context")
	winnerRelease, ok := dispatches.acquire(key)
	if !ok {
		t.Fatal("initial reservation failed")
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	acquired := 0
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, ok := dispatches.acquire(key)
			if !ok {
				return
			}
			mu.Lock()
			acquired++
			mu.Unlock()
			release()
		}()
	}
	wg.Wait()
	if acquired != 0 {
		t.Fatalf("duplicate reservations succeeded while winner was active: %d", acquired)
	}
	winnerRelease()
	if _, ok := dispatches.acquire(key); !ok {
		t.Fatal("released key was not reusable")
	}
}

func TestActiveAgentDispatchKeyNormalizesPromptAndContext(t *testing.T) {
	if got, want := activeAgentDispatchKey("general", "  same prompt\n", " context "), "7:general11:same prompt7:context"; got != want {
		t.Fatalf("key = %q, want %q", got, want)
	}
	if activeAgentDispatchKey("general", "same prompt", "a") == activeAgentDispatchKey("general", "same prompt", "b") {
		t.Fatal("different contexts must not share an active key")
	}
}

func TestAgentRunRegistrySharesActiveDispatchesFromZeroValueParent(t *testing.T) {
	parent := &AgentRunRegistry{}
	child := &AgentRunRegistry{}
	child.ShareLimiterFrom(parent)

	key := activeAgentDispatchKey("general", "same prompt", "same context")
	release, ok := parent.acquireActiveDispatch(key)
	if !ok {
		t.Fatal("parent reservation failed")
	}
	defer release()
	if _, ok := child.acquireActiveDispatch(key); ok {
		t.Fatal("child registry did not share parent's active dispatch set")
	}
}

func TestDuplicateToolCallIndicesPreserveFirstAcrossPartitions(t *testing.T) {
	calls := []ToolCall{
		{ID: "parallel-winner", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "read", Arguments: `{"path":"x"}`}},
		{ID: "sequential-duplicate", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "read", Arguments: " { \"path\": \"x\" } "}},
		{ID: "different-args", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "read", Arguments: `{"path":"y"}`}},
	}
	rejected, order := duplicateToolCallIndices(calls)
	if len(order) != 1 || order[0] != 1 || rejected[1] != 0 {
		t.Fatalf("rejected=%v order=%v, want index 1 rejected in favor of 0", rejected, order)
	}
	if _, duplicate := rejected[2]; duplicate {
		t.Fatal("different arguments were deduplicated")
	}
}

type dedupStepClient struct {
	mu        sync.Mutex
	responses []*Message
	calls     int
}

func (c *dedupStepClient) Chat([]Message, []map[string]interface{}) (*Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.calls >= len(c.responses) {
		return &Message{Role: "assistant", Content: "done"}, nil
	}
	response := c.responses[c.calls]
	c.calls++
	return response, nil
}

func (c *dedupStepClient) GetProvider() string { return "mock" }
func (c *dedupStepClient) GetModel() string    { return "mock-model" }

type dedupParallelTool struct {
	calls *int32
}

func (t dedupParallelTool) Name() string        { return "dedup_parallel" }
func (t dedupParallelTool) Description() string { return "test duplicate suppression" }
func (t dedupParallelTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": t.Name(), "description": t.Description()}
}
func (t dedupParallelTool) Execute(json.RawMessage) (string, error) {
	atomic.AddInt32(t.calls, 1)
	return "winner-result", nil
}
func (t dedupParallelTool) Parallel() bool { return true }

func TestStepRejectsDuplicateParallelCallAndSharesWinnerResult(t *testing.T) {
	var calls int32
	first := ToolCall{ID: "winner", Type: "function"}
	first.Function.Name = "dedup_parallel"
	first.Function.Arguments = `{"value":"same"}`
	second := first
	second.ID = "duplicate"
	client := &dedupStepClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{first, second}},
		{Role: "assistant", Content: "done"},
	}}
	a := NewAgent(client, []tool.Tool{dedupParallelTool{calls: &calls}}, nil, nil)
	a.Permissions().SetMode(PermissionModeYOLO)
	messages, err := a.Step([]Message{{Role: "user", Content: "run it"}})
	if err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("tool execution count = %d, want 1", got)
	}
	var winner, duplicate *Message
	for i := range messages {
		if messages[i].Role != "tool" {
			continue
		}
		switch messages[i].ToolID {
		case "winner":
			winner = &messages[i]
		case "duplicate":
			duplicate = &messages[i]
		}
	}
	if winner == nil || winner.Content != "winner-result" {
		t.Fatalf("winner result = %#v, want winner-result", winner)
	}
	if duplicate == nil || !strings.Contains(duplicate.Content, "Rejected duplicate") || !strings.Contains(duplicate.Content, "winner-result") || !strings.Contains(duplicate.Content, "winner") {
		t.Fatalf("duplicate result = %#v, want rejection reason and winner result", duplicate)
	}
}
