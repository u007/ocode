package server

import (
	"encoding/json"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

// TestLiveResetCarriesAnsweredRowAfterAutoAnswer guards the liveReset call in
// the server auto-answer path: after the question judge answers the open ask,
// the live-persist view must be restarted from the answered transcript, so an
// intermediate save never carries the unanswered sentinel row. Removing the
// liveReset call (or its hook invocation) leaves the captured slice empty,
// failing this test.
func TestLiveResetCarriesAnsweredRowAfterAutoAnswer(t *testing.T) {
	url, _ := newSystemoneStub(t, questionDeterminedVerdictJSON)
	h, id, _ := autoContinueTestServer(t, url)
	qc := &questionAskThenReplyClient{}
	as := swapToQuestionAgent(t, h, id, qc, true)

	var captured [][]agent.Message
	as.liveResetHook = func(msgs []agent.Message) {
		cp := append([]agent.Message(nil), msgs...)
		captured = append(captured, cp)
	}

	if _, err := h.runTurn(id, as, "use Postgres for the store", turnOptions{}); err != nil {
		t.Fatalf("runTurn: %v", err)
	}

	if len(captured) == 0 {
		t.Fatal("liveReset was never invoked during the auto-answer turn")
	}
	last := captured[len(captured)-1]
	row := questionToolRow(last, "q-call-db")
	if row == nil {
		t.Fatalf("live-persist view has no answered question row: %+v", last)
	}
	var sets []tool.QuestionAnswerSet
	if err := json.Unmarshal([]byte(row.Content), &sets); err != nil {
		t.Fatalf("answered row is not an answer set: %v", err)
	}
	if len(sets) != 1 || len(sets[0].Answers) != 1 || sets[0].Answers[0].Label != "Postgres" {
		t.Fatalf("live-persist answered row = %+v, want Postgres", sets)
	}
	if tailIsQuestionAsk(last) {
		t.Fatal("live-persist view still carries the unanswered ask")
	}
}
