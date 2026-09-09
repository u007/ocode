package server

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// questionThenReplyClient: call 1 raises a `question` tool call (turn 1
// pauses on the prompt); every later call answers with plain text.
type questionThenReplyClient struct{ calls int }

func (f *questionThenReplyClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	f.calls++
	if f.calls == 1 {
		return &agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "q-call-1", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: "question", Arguments: `{"questions":[{"header":"Deploy target","question":"Where should I deploy?","options":[{"label":"Staging","description":"Push to staging"}]}]}`}}}}, nil
	}
	return &agent.Message{Role: "assistant", Content: "streamed reply after typed answer"}, nil
}
func (f *questionThenReplyClient) GetProvider() string { return "fake" }
func (f *questionThenReplyClient) GetModel() string    { return "fake-model" }

// A user who types a chat message while a `question` prompt is pending
// (instead of using the dialog) starts an ordinary async turn. The turn's
// output must survive in memory and on disk — regression for the
// desktop "streamed reply vanished at turn end" report (2026-09-09).
func TestChatMessageWhileQuestionPendingKeepsTurnOutput(t *testing.T) {
	t.Run("resident agent", func(t *testing.T) { runQuestionFollowup(t, false) })
	t.Run("agent rebuilt from disk", func(t *testing.T) { runQuestionFollowup(t, true) })
}

// A successful rebase can insert another writer's rows before this turn's
// suffix. The resident transcript must adopt that merged view; otherwise the
// next turn sees a false base divergence and can lose its new response.
func TestPersistTurnTranscriptUpdatesResidentAfterRebase(t *testing.T) {
	h := NewHandler()
	projectRoot := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, projectRoot)

	base := []agent.Message{{Role: "user", Content: "base", UserSeq: 1}}
	if err := session.SaveForDir(projectRoot, id, "", base, nil); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	as := &agentSession{messages: append([]agent.Message(nil), base...)}
	// Simulate another writer appending between this turn's load and save.
	if err := session.AppendUserMessageForDir(projectRoot, id, "foreign"); err != nil {
		t.Fatalf("foreign append: %v", err)
	}
	as.messages = append(as.messages, agent.Message{Role: "assistant", Content: "first reply"})
	h.persistTurnTranscript(id, as, len(base), "test-rebase")

	if got := messageContents(as.messages); !reflect.DeepEqual(got, []string{"base", "foreign", "first reply"}) {
		t.Fatalf("resident transcript after rebase = %v", got)
	}

	// A normal subsequent turn now uses the merged resident transcript as its
	// base. Persist its user message through the same pre-persist path used by
	// the desktop/web async handlers, then persist its response.
	if err := session.AppendUserMessageForDir(projectRoot, id, "second"); err != nil {
		t.Fatalf("second user append: %v", err)
	}
	as.messages = append(as.messages,
		agent.Message{Role: "user", Content: "second", UserSeq: session.NextUserSeq(as.messages)},
		agent.Message{Role: "assistant", Content: "second reply"},
	)
	h.persistTurnTranscript(id, as, len(as.messages)-1, "test-second-turn")

	loaded, err := session.LoadForDir(projectRoot, id)
	if err != nil {
		t.Fatalf("load final transcript: %v", err)
	}
	if got := messageContents(loaded.Messages); !reflect.DeepEqual(got, []string{"base", "foreign", "first reply", "second", "second reply"}) {
		t.Fatalf("final transcript = %v", got)
	}
}

func messageContents(messages []agent.Message) []string {
	contents := make([]string, 0, len(messages))
	for _, message := range messages {
		contents = append(contents, message.Content)
	}
	return contents
}

func runQuestionFollowup(t *testing.T, releaseBetweenTurns bool) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	ag := agent.NewAgent(&questionThenReplyClient{}, []tool.Tool{&tool.QuestionTool{}}, nil, nil)
	defer ag.Shutdown()
	as := &agentSession{agent: ag, model: "fake-model"}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()
	h.sessions.setAgent(id, as)

	// Turn 1: pauses on the question prompt.
	if _, err := h.runTurn(id, as, "where should I deploy?", turnOptions{}); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if err := session.FlushForDir(proj, id, 10*time.Second); err != nil {
		t.Fatalf("flush turn 1: %v", err)
	}
	as.mu.Lock()
	if tail := as.messages[len(as.messages)-1]; !strings.HasPrefix(tail.Content, tool.SentinelQuestionPrompt) {
		t.Fatalf("turn 1 tail = %q, want question sentinel", tail.Content)
	}
	as.mu.Unlock()

	if releaseBetweenTurns {
		// Idle eviction / tab close / app restart: the next turn rebuilds
		// the agent from the on-disk transcript (bootstrapEntryAgent →
		// session.LoadForDir). Model that by swapping in the loader's view.
		loaded, err := session.LoadForDir(proj, id)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		as.mu.Lock()
		as.messages = loaded.Messages
		as.mu.Unlock()
	}

	// Turn 2: the user types a chat message instead of answering the dialog
	// (the web/desktop async path: persist user message, then run the turn).
	job, err := h.dispatchTurn(id, "fake-model", "staging please", turnOptions{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	<-job.persistAck
	if job.err != nil {
		t.Fatalf("persist: %v", job.err)
	}
	h.turnJobsWG.Wait()
	if err := session.FlushForDir(proj, id, 10*time.Second); err != nil {
		t.Fatalf("flush turn 2: %v", err)
	}

	as = h.lookupAgentSession(id)
	as.mu.Lock()
	mem := append([]agent.Message(nil), as.messages...)
	as.mu.Unlock()
	if last := mem[len(mem)-1]; last.Role != "assistant" || last.Content != "streamed reply after typed answer" {
		t.Fatalf("in-memory tail = %+v, want the turn-2 assistant reply (transcript had %d msgs)", last, len(mem))
	}

	loaded, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if last := loaded.Messages[len(loaded.Messages)-1]; last.Role != "assistant" || last.Content != "streamed reply after typed answer" {
		t.Fatalf("on-disk tail = %+v, want the turn-2 assistant reply (%d msgs)", last, len(loaded.Messages))
	}
}
