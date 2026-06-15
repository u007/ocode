package agent

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/memory"
)

type memoryMaintenanceStubClient struct {
	mu        sync.Mutex
	calls     [][]Message
	responses []*Message
}

func (c *memoryMaintenanceStubClient) Chat(messages []Message, tools []map[string]interface{}) (*Message, error) {
	c.mu.Lock()
	copyMsgs := make([]Message, len(messages))
	copy(copyMsgs, messages)
	c.calls = append(c.calls, copyMsgs)
	idx := len(c.calls) - 1
	var resp *Message
	if idx < len(c.responses) {
		resp = c.responses[idx]
	} else if len(c.responses) > 0 {
		resp = c.responses[len(c.responses)-1]
	}
	c.mu.Unlock()
	if resp == nil {
		return &Message{Role: "assistant", Content: `{"action":"noop","scope":"project","body":"","reason":"default noop"}`}, nil
	}
	clone := *resp
	return &clone, nil
}

func (c *memoryMaintenanceStubClient) GetProvider() string { return "stub" }
func (c *memoryMaintenanceStubClient) GetModel() string    { return "stub" }

func TestRunMemoryMaintenanceAppliesProjectUpdate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workDir := t.TempDir()
	paths, err := memory.ResolvePaths(workDir)
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	for _, path := range []string{paths.User, paths.Project, paths.Global} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(paths.Project), 0o755); err != nil {
		t.Fatalf("MkdirAll project: %v", err)
	}
	if err := os.WriteFile(paths.Project, []byte("old project note\n"), 0o644); err != nil {
		t.Fatalf("WriteFile project: %v", err)
	}
	if err := os.WriteFile(paths.User, []byte("user preference\n"), 0o644); err != nil {
		t.Fatalf("WriteFile user: %v", err)
	}
	if err := os.WriteFile(paths.Global, []byte("global lesson\n"), 0o644); err != nil {
		t.Fatalf("WriteFile global: %v", err)
	}

	client := &memoryMaintenanceStubClient{responses: []*Message{{
		Role:    "assistant",
		Content: `{"action":"update","scope":"project","body":"project memory\n- durable decision\n","reason":"new repo-specific fact"}`,
	}}}

	prevNewClientFn := newClientFn
	newClientFn = func(cfg *config.Config, model string) LLMClient { return client }
	t.Cleanup(func() { newClientFn = prevNewClientFn })

	a := &Agent{config: &config.Config{Ocode: config.OcodeConfig{SmallModel: "stub-model", SmallModelEnabled: true}}}
	a.SetMemoryEnabled(true)

	req := MemoryMaintenanceRequest{
		WorkDir: workDir,
		Job: JobEvent{
			Kind:   "agent",
			Name:   "task",
			Status: "done",
			Result: "finished with a durable repo decision",
		},
		RecentMessages: []Message{{Role: "user", Content: "please remember this repo decision"}},
	}
	if _, err := memory.ResolvePaths(workDir); err != nil {
		t.Fatalf("ResolvePaths sanity check: %v", err)
	}
	a.runMemoryMaintenance(req)

	data, err := os.ReadFile(paths.Project)
	if err != nil {
		t.Fatalf("ReadFile project: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "project memory") || !strings.Contains(got, "durable decision") {
		t.Fatalf("project memory not updated, got:\n%s", got)
	}
	if strings.Contains(got, "old project note") {
		t.Fatalf("expected old project note to be replaced, got:\n%s", got)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.calls) == 0 {
		t.Fatal("expected memory maintenance to call the small model")
	}
	if len(client.calls[0]) != 1 || client.calls[0][0].Role != "system" {
		t.Fatalf("expected memory maintenance prompt to be sent as a single system message, got %+v", client.calls[0])
	}
	prompt := client.calls[0][0].Content
	for _, want := range []string{"Completed job:", "kind: agent", "Current memory snapshot:", "## Project memory", "Hard cap:"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected %q in prompt:\n%s", want, prompt)
		}
	}
}

func TestRunMemoryMaintenanceCompressesOversizedScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workDir := t.TempDir()
	paths, err := memory.ResolvePaths(workDir)
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.Project), 0o755); err != nil {
		t.Fatalf("MkdirAll project: %v", err)
	}
	oversized := strings.Repeat("x", memoryMaintenanceHardLimitBytes+2048)
	if err := os.WriteFile(paths.Project, []byte(oversized), 0o644); err != nil {
		t.Fatalf("WriteFile project: %v", err)
	}

	client := &memoryMaintenanceStubClient{responses: []*Message{
		{Role: "assistant", Content: `{"action":"noop","scope":"project","body":"","reason":"defer"}`},
		{Role: "assistant", Content: `{"action":"compress","scope":"project","body":"project memory\n- compressed durable summary\n","reason":"below cap"}`},
	}}

	prevNewClientFn := newClientFn
	newClientFn = func(cfg *config.Config, model string) LLMClient { return client }
	t.Cleanup(func() { newClientFn = prevNewClientFn })

	a := &Agent{config: &config.Config{Ocode: config.OcodeConfig{SmallModel: "stub-model", SmallModelEnabled: true}}}
	a.SetMemoryEnabled(true)

	req := MemoryMaintenanceRequest{WorkDir: workDir, Job: JobEvent{Kind: "process", Name: "bash", Status: "exited", Result: "exit 0"}}
	a.runMemoryMaintenance(req)

	data, err := os.ReadFile(paths.Project)
	if err != nil {
		t.Fatalf("ReadFile project: %v", err)
	}
	if len(data) > memoryMaintenanceHardLimitBytes {
		t.Fatalf("project memory still oversized: %d bytes", len(data))
	}
	if !strings.Contains(string(data), "compressed durable summary") {
		t.Fatalf("expected compressed summary in project memory, got:\n%s", string(data))
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.calls) < 2 {
		t.Fatalf("expected planner + compression calls, got %d", len(client.calls))
	}
	if len(client.calls[1]) != 1 || client.calls[1][0].Role != "system" {
		t.Fatalf("expected forced compression prompt to be sent as a single system message, got %+v", client.calls[1])
	}
	compressPrompt := client.calls[1][0].Content
	if !strings.Contains(compressPrompt, "Forced action: compress") || !strings.Contains(compressPrompt, "Forced scope: project") {
		t.Fatalf("expected forced compression prompt, got:\n%s", compressPrompt)
	}
}
