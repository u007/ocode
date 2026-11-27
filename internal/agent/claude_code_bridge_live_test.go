package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// claudeLiveCall is one approval request the real claude CLI sent through the
// bridge, recorded by the live test's approve stub.
type claudeLiveCall struct {
	Name  string
	Input string
	// Answer is set for AskUserQuestion: the question text and the option label
	// the stub picked.
	Answer string
}

// TestClaudeCodeBridgeLive drives the real `claude` CLI through the approval
// bridge. It is gated: it runs only with OCODE_LIVE_CLAUDE=1 and a `claude`
// binary on PATH, because it spends real model tokens and needs a signed-in CLI.
func TestClaudeCodeBridgeLive(t *testing.T) {
	if os.Getenv("OCODE_LIVE_CLAUDE") != "1" {
		t.Skip("set OCODE_LIVE_CLAUDE=1 to run the live Claude Code bridge test")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("`claude` is not on PATH")
	}
	// TestMain points HOME at a temp tree, where the CLI finds no login. The live
	// CLI must read the real sign-in, so restore the real home for this test.
	u, err := user.Current()
	if err != nil {
		t.Fatalf("current user: %v", err)
	}
	setHomeTree(t, u.HomeDir)

	workDir := t.TempDir()
	if out, err := exec.Command("git", "init", workDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	var (
		mu    sync.Mutex
		calls []claudeLiveCall
	)
	approve := func(_ context.Context, toolName string, input json.RawMessage, _ string) (bool, json.RawMessage, string) {
		rec := claudeLiveCall{Name: toolName, Input: string(input)}
		defer func() {
			mu.Lock()
			calls = append(calls, rec)
			mu.Unlock()
		}()
		if toolName != "AskUserQuestion" {
			return true, nil, ""
		}
		var in map[string]any
		if err := json.Unmarshal(input, &in); err != nil {
			return false, nil, "live test could not parse AskUserQuestion input: " + err.Error()
		}
		questions, _ := in["questions"].([]any)
		if len(questions) == 0 {
			return false, nil, "live test: AskUserQuestion has no questions"
		}
		first, _ := questions[0].(map[string]any)
		question, _ := first["question"].(string)
		options, _ := first["options"].([]any)
		if question == "" || len(options) == 0 {
			return false, nil, "live test: AskUserQuestion first question has no text or options"
		}
		last, _ := options[len(options)-1].(map[string]any)
		label, _ := last["label"].(string)
		if label == "" {
			return false, nil, "live test: last AskUserQuestion option has no label"
		}
		rec.Answer = question + " => " + label
		in["answers"] = map[string]string{question: label}
		updated, err := json.Marshal(in)
		if err != nil {
			return false, nil, "live test: marshal updatedInput: " + err.Error()
		}
		return true, updated, ""
	}

	c := &GenericClient{
		Provider:            "claude-code",
		Model:               "claude-haiku-4-5",
		WorkDir:             workDir,
		PermissionModeFn:    func() PermissionMode { return PermissionModeNormal },
		ClaudeCodeApproveFn: approve,
	}
	// OCODE_LIVE_CLAUDE_BIN points at a specific binary (e.g. a logging wrapper).
	if bin := os.Getenv("OCODE_LIVE_CLAUDE_BIN"); bin != "" {
		c.claudeCLIPathOverride = bin
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	prompt := "Do these in order: 1) Use the Write tool to create notes.txt containing exactly: hello  " +
		"2) Use the AskUserQuestion tool to ask me 'Pick a fruit' with options Apple, Banana, Cherry in that order. " +
		"3) Reply with only the fruit I picked."
	msg, runErr := c.chatClaudeCode(ctx, []Message{{Role: "user", Content: prompt}}, nil)

	mu.Lock()
	recorded := append([]claudeLiveCall(nil), calls...)
	mu.Unlock()

	content := ""
	if msg != nil {
		content = msg.Content
	}
	dump := func() {
		for i, call := range recorded {
			t.Logf("call[%d] name=%q input=%s answer=%q", i, call.Name, call.Input, call.Answer)
		}
		t.Logf("returned content: %q", content)
	}

	if runErr != nil {
		dump()
		t.Fatalf("chatClaudeCode error: %v", runErr)
	}

	seen := map[string]bool{}
	for _, call := range recorded {
		seen[call.Name] = true
	}
	ok := true
	if !seen["Write"] {
		t.Errorf("stub never saw a Write call")
		ok = false
	}
	if !seen["AskUserQuestion"] {
		t.Errorf("stub never saw an AskUserQuestion call")
		ok = false
	}

	data, err := os.ReadFile(filepath.Join(workDir, "notes.txt"))
	if err != nil {
		t.Errorf("notes.txt not created: %v", err)
		ok = false
	} else if got := strings.TrimSpace(string(data)); got != "hello" {
		t.Errorf("notes.txt content = %q, want %q", got, "hello")
		ok = false
	}

	if !strings.Contains(content, "Cherry") {
		t.Errorf("final content does not contain Cherry: %q", content)
		ok = false
	}

	if !ok {
		dump()
	}
}
