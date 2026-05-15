package tool

import (
	"encoding/json"
	"testing"
)

func TestTodoWriteAndReadUseSessionState(t *testing.T) {
	SetTodoSession("session-1")
	ResetTodoState()

	writer := TodoWriteTool{}
	args, err := json.Marshal(map[string]string{"todoText": "- [ ] first\n- [x] second"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Execute(args); err != nil {
		t.Fatal(err)
	}

	reader := TodoReadTool{}
	got, err := reader.Execute(nil)
	if err != nil {
		t.Fatal(err)
	}

	if got != "- [ ] first\n- [x] second" {
		t.Fatalf("expected in-memory todo state, got %q", got)
	}
}

func TestTodoWriteRequiresSession(t *testing.T) {
	ResetTodoState()
	SetTodoSession("")

	args, err := json.Marshal(map[string]string{"todoText": "- [ ] first"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (TodoWriteTool{}).Execute(args); err == nil {
		t.Fatal("expected todo write to require an active session")
	}
}

func TestResetTodoStateClearsReadOutput(t *testing.T) {
	SetTodoSession("session-1")
	ResetTodoState()

	writer := TodoWriteTool{}
	args, err := json.Marshal(map[string]string{"todoText": "- [ ] first"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Execute(args); err != nil {
		t.Fatal(err)
	}

	ResetTodoState()
	got, err := TodoReadTool{}.Execute(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "No todo list found" {
		t.Fatalf("expected reset todo state to clear read output, got %q", got)
	}
}
