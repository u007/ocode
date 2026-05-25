package runcli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jamesmercstudio/ocode/internal/agent"
)

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
}

func TestResolvePromptFromArg(t *testing.T) {
	p, err := resolvePrompt("hello", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != "hello" {
		t.Errorf("expected 'hello', got %q", p)
	}
}

func TestResolvePromptFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(path, []byte("file content"), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := resolvePrompt("", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != "file content" {
		t.Errorf("expected 'file content', got %q", p)
	}
}

func TestResolvePromptFromNonexistentFile(t *testing.T) {
	_, err := resolvePrompt("", "/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestResolvePromptEmpty(t *testing.T) {
	p, err := resolvePrompt("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != "" {
		t.Errorf("expected empty string, got %q", p)
	}
}

func TestResolveRunInputFromPositionalArgs(t *testing.T) {
	got, err := resolveRunInput("", []string{"Explain", "closures"}, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Explain closures" {
		t.Fatalf("expected positional args to be joined, got %q", got)
	}
}

func TestResolveRunInputSingleFileFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(path, []byte("from file"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveRunInput("", nil, []string{path}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "from file" {
		t.Fatalf("expected file fallback content, got %q", got)
	}
}

func TestResolveRunInputAppendsAttachedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "context.txt")
	if err := os.WriteFile(path, []byte("extra context"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveRunInput("base prompt", nil, []string{path}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "base prompt\nAttached file context.txt:\nextra context"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestResolveCommandPromptWithArgsPlaceholder(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir)
	cmdDir := filepath.Join(dir, ".opencode", "commands")
	if err := os.MkdirAll(cmdDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: greet\ndescription: greet command\n---\nSay hello to {{args}}"
	if err := os.WriteFile(filepath.Join(cmdDir, "greet.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveCommandPrompt("greet", "Ada")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Say hello to Ada" {
		t.Fatalf("expected replaced prompt, got %q", got)
	}
}

func TestResolveCommandPromptAppendsArgsForCommandsWithoutPlaceholder(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir)
	cmdDir := filepath.Join(dir, ".opencode", "commands")
	if err := os.MkdirAll(cmdDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: summarize\ndescription: summarize command\n---\nSummarize the following"
	if err := os.WriteFile(filepath.Join(cmdDir, "summarize.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveCommandPrompt("summarize", "notes here")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Summarize the following\n\nArguments:\nnotes here"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestOutputJSONEvents(t *testing.T) {
	var tc agent.ToolCall
	tc.ID = "call-1"
	tc.Function.Name = "todowrite"
	tc.Function.Arguments = `{"todoText":"ship it"}`
	messages := []agent.Message{
		{Role: "assistant", ToolCalls: []agent.ToolCall{tc}},
		{Role: "tool", ToolID: "call-1", Content: "ok"},
		{Role: "assistant", Content: "Done."},
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	if err := outputJSONEvents(messages, "sess-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = w.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("expected 2 json lines, got %d: %s", len(lines), buf.String())
	}

	var toolEvent map[string]any
	if err := json.Unmarshal(lines[0], &toolEvent); err != nil {
		t.Fatal(err)
	}
	if toolEvent["type"] != "tool_use" {
		t.Fatalf("expected first event type tool_use, got %v", toolEvent["type"])
	}

	var textEvent map[string]any
	if err := json.Unmarshal(lines[1], &textEvent); err != nil {
		t.Fatal(err)
	}
	if textEvent["type"] != "text" {
		t.Fatalf("expected second event type text, got %v", textEvent["type"])
	}
}

func TestOutputJSONEventsIncludesRawInputOnInvalidJSON(t *testing.T) {
	// When ToolCall.Arguments is not valid JSON, the emitted event should
	// surface the raw payload under "input_raw" so downstream consumers can
	// see what the model actually produced.
	var tc agent.ToolCall
	tc.ID = "call-1"
	tc.Function.Name = "bash"
	tc.Function.Arguments = `{not-json`
	messages := []agent.Message{
		{Role: "assistant", ToolCalls: []agent.ToolCall{tc}},
		{Role: "tool", ToolID: "call-1", Content: "ok"},
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	if err := outputJSONEvents(messages, "sess-x"); err != nil {
		t.Fatalf("outputJSONEvents: %v", err)
	}
	_ = w.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event); err != nil {
		t.Fatalf("decode: %v\n%s", err, buf.String())
	}
	part, _ := event["part"].(map[string]any)
	state, _ := part["state"].(map[string]any)
	if state["input_raw"] != `{not-json` {
		t.Fatalf("expected input_raw to surface raw payload, got %v", state["input_raw"])
	}
	if _, present := state["input"]; present {
		t.Fatalf("expected no input key when JSON invalid, got %v", state["input"])
	}
}
