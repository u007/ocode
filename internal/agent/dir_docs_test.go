package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

func TestTrackDirMDTouch_LoadsOncePerSubdir(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "pkg", "widgets")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "CLAUDE.md"), []byte("widget rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(sub, "widget.go")
	if err := os.WriteFile(target, []byte("package widgets"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &Agent{workDir: root}
	args, _ := json.Marshal(map[string]string{"path": target})

	a.trackDirMDTouch("read", args)
	blocks := a.drainDirMDPending()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 pending block, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0], "widget rules") {
		t.Fatalf("block missing content: %q", blocks[0])
	}

	// Touching the same subdirectory again must not re-queue it.
	a.trackDirMDTouch("read", args)
	if blocks := a.drainDirMDPending(); len(blocks) != 0 {
		t.Fatalf("expected no re-queue on second touch, got %d", len(blocks))
	}
}

func TestTrackDirMDTouch_MultieditUsesFilePath(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "AGENTS.md"), []byte("pkg rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(sub, "widget.go")
	if err := os.WriteFile(target, []byte("package pkg"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &Agent{workDir: root}
	args, _ := json.Marshal(map[string]interface{}{
		"file_path": target,
		"edits":     []map[string]string{{"oldString": "pkg", "newString": "pkg"}},
	})
	a.trackDirMDTouch("multiedit", args)

	blocks := a.drainDirMDPending()
	if len(blocks) != 1 || !strings.Contains(blocks[0], "pkg rules") {
		t.Fatalf("expected multiedit to queue directory docs, got %#v", blocks)
	}
}

func TestTrackDirMDTouch_SkipsRootLevelDoc(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("root rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &Agent{workDir: root}
	args, _ := json.Marshal(map[string]string{"path": target})
	a.trackDirMDTouch("read", args)
	if blocks := a.drainDirMDPending(); len(blocks) != 0 {
		t.Fatalf("expected root-level doc to be skipped (already always-on), got %d", len(blocks))
	}
}

func TestInjectDirMDTail_NoopWhenNothingPending(t *testing.T) {
	a := &Agent{workDir: t.TempDir()}
	base := []Message{{Role: "user", Content: "hi"}}
	got := injectDirMDTail(base, a)
	if len(got) != len(base) {
		t.Fatalf("expected no-op, got %d messages", len(got))
	}
}

func TestInjectDirMDTail_EmitsAndDrains(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "svc")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "AGENTS.md"), []byte("svc rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &Agent{workDir: root}
	args, _ := json.Marshal(map[string]string{"path": filepath.Join(sub, "x.go")})
	a.trackDirMDTouch("read", args)

	base := []Message{{Role: "user", Content: "hi"}}
	got := injectDirMDTail(base, a)
	if len(got) != len(base)+1 {
		t.Fatalf("expected one appended message, got %d", len(got))
	}
	if got[len(got)-1].Role != "user" {
		t.Fatalf("expected volatile directory docs to use user role, got %q", got[len(got)-1].Role)
	}
	if !strings.Contains(got[len(got)-1].Content, "svc rules") {
		t.Fatalf("appended message missing content: %q", got[len(got)-1].Content)
	}

	// Next call with nothing new pending is a no-op.
	got2 := injectDirMDTail(base, a)
	if len(got2) != len(base) {
		t.Fatalf("expected no-op on second call, got %d", len(got2))
	}
}

func TestInjectDirMDTail_EmitsNewDocsAfterPriorInjection(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for dir, content := range map[string]string{first: "first rules", second: "second rules"} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := &Agent{workDir: root}
	firstArgs, _ := json.Marshal(map[string]string{"path": filepath.Join(first, "a.go")})
	secondArgs, _ := json.Marshal(map[string]string{"path": filepath.Join(second, "b.go")})
	a.trackDirMDTouch("read", firstArgs)
	messages := injectDirMDTail([]Message{{Role: "user", Content: "first"}}, a)
	a.trackDirMDTouch("read", secondArgs)
	messages = append(messages, Message{Role: "tool", Content: "read result"})
	messages = injectDirMDTail(messages, a)
	if !strings.Contains(messages[len(messages)-1].Content, "second rules") {
		t.Fatalf("expected newly queued docs after prior injection, got %q", messages[len(messages)-1].Content)
	}
}

type dirDocsStepClient struct {
	responses []*Message
	calls     [][]Message
}

func (c *dirDocsStepClient) Chat(messages []Message, _ []map[string]interface{}) (*Message, error) {
	c.calls = append(c.calls, append([]Message(nil), messages...))
	response := c.responses[len(c.calls)-1]
	return response, nil
}

func (c *dirDocsStepClient) GetProvider() string { return "mock" }
func (c *dirDocsStepClient) GetModel() string    { return "mock-model" }

func TestAgentStepInjectsToolDiscoveredDocsBeforeNextLLMCall(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "svc")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "CLAUDE.md"), []byte("svc rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(sub, "service.go")
	if err := os.WriteFile(target, []byte("package svc"), 0o644); err != nil {
		t.Fatal(err)
	}
	readArgs, err := json.Marshal(map[string]string{"path": target})
	if err != nil {
		t.Fatal(err)
	}

	client := &dirDocsStepClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{{
			ID: "read-call",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "read", Arguments: string(readArgs)},
		}}},
		{Role: "assistant", Content: "done"},
	}}
	a := NewAgent(client, nil, nil, nil)
	a.workDir = root
	a.Permissions().SetRule("read", PermissionAllow)
	a.AddTools([]tool.Tool{&MockTool{name: "read", result: "file contents"}})

	if _, err := a.Step([]Message{{Role: "user", Content: "read the service"}}); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 2 {
		t.Fatalf("expected two LLM calls, got %d", len(client.calls))
	}
	var found bool
	for _, message := range client.calls[1] {
		if message.Role == "user" && strings.Contains(message.Content, "svc rules") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("next LLM call did not receive discovered directory docs: %#v", client.calls[1])
	}
}
