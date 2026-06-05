package runcli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// captureOutputSummary calls outputSummary but captures stdout.
func captureOutputSummary(messages []agent.Message, sessionID, modelName string, startTime time.Time) (string, error) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w

	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		outC <- buf.String()
	}()

	err = outputSummary(messages, sessionID, modelName, startTime)

	w.Close()
	os.Stdout = old
	out := <-outC
	return out, err
}

func TestOutputSummary_textOnly(t *testing.T) {
	msgs := []agent.Message{
		{Role: "assistant", Content: "Hello, I have done the work."},
	}
	out, err := captureOutputSummary(msgs, "", "", time.Now())
	if err != nil {
		t.Fatalf("outputSummary error: %v", err)
	}
	if !strings.Contains(out, "OCODE RUN SUMMARY") {
		t.Errorf("expected header, got:\n%s", out)
	}
	if !strings.Contains(out, "Hello, I have done the work.") {
		t.Errorf("expected response text, got:\n%s", out)
	}
	if !strings.Contains(out, "No file changes") {
		t.Errorf("expected no-file-changes message, got:\n%s", out)
	}
}

func TestOutputSummary_writeTool(t *testing.T) {
	args, _ := json.Marshal(map[string]string{
		"path":    "main.go",
		"content": "package main",
	})
	msgs := []agent.Message{
		{
			Role: "assistant",
			ToolCalls: []agent.ToolCall{
				{ID: "call1", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "write", Arguments: string(args)}},
			},
		},
		{Role: "tool", ToolID: "call1", Content: "Successfully wrote main.go"},
		{Role: "assistant", Content: "Created the file."},
	}
	out, err := captureOutputSummary(msgs, "sess-1", "gpt-4o", time.Now().Add(-5*time.Second))
	if err != nil {
		t.Fatalf("outputSummary error: %v", err)
	}
	if !strings.Contains(out, "Files Created") {
		t.Errorf("expected Files Created section, got:\n%s", out)
	}
	if !strings.Contains(out, "main.go") {
		t.Errorf("expected main.go listed, got:\n%s", out)
	}
	if !strings.Contains(out, "gpt-4o") {
		t.Errorf("expected model name, got:\n%s", out)
	}
	if !strings.Contains(out, "sess-1") {
		t.Errorf("expected session ID, got:\n%s", out)
	}
}

func TestOutputSummary_deleteTool(t *testing.T) {
	args, _ := json.Marshal(map[string]string{
		"path": "old.go",
	})
	msgs := []agent.Message{
		{
			Role: "assistant",
			ToolCalls: []agent.ToolCall{
				{ID: "call1", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "delete", Arguments: string(args)}},
			},
		},
		{Role: "assistant", Content: "Removed the obsolete file."},
	}
	out, err := captureOutputSummary(msgs, "", "", time.Now())
	if err != nil {
		t.Fatalf("outputSummary error: %v", err)
	}
	if !strings.Contains(out, "Files Deleted") {
		t.Errorf("expected Files Deleted section, got:\n%s", out)
	}
	if !strings.Contains(out, "old.go") {
		t.Errorf("expected old.go listed, got:\n%s", out)
	}
}

func TestOutputSummary_editTool(t *testing.T) {
	args, _ := json.Marshal(map[string]string{
		"path":    "main.go",
		"search":  "foo",
		"replace": "bar",
	})
	msgs := []agent.Message{
		{
			Role: "assistant",
			ToolCalls: []agent.ToolCall{
				{ID: "call1", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "edit", Arguments: string(args)}},
			},
		},
		{Role: "assistant", Content: "Updated the file."},
	}
	out, err := captureOutputSummary(msgs, "", "", time.Now())
	if err != nil {
		t.Fatalf("outputSummary error: %v", err)
	}
	if !strings.Contains(out, "Files Modified") {
		t.Errorf("expected Files Modified section, got:\n%s", out)
	}
	if !strings.Contains(out, "main.go") {
		t.Errorf("expected main.go listed, got:\n%s", out)
	}
}

func TestOutputSummary_multiFileEditTool(t *testing.T) {
	edits := []map[string]interface{}{
		{"path": "a.go", "search": "x", "replace": "y"},
		{"path": "b.go", "search": "x", "replace": "y"},
	}
	args, _ := json.Marshal(map[string]interface{}{
		"edits": edits,
	})
	msgs := []agent.Message{
		{
			Role: "assistant",
			ToolCalls: []agent.ToolCall{
				{ID: "call1", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "multi_file_edit", Arguments: string(args)}},
			},
		},
	}
	out, err := captureOutputSummary(msgs, "", "", time.Now())
	if err != nil {
		t.Fatalf("outputSummary error: %v", err)
	}
	if !strings.Contains(out, "Files Modified") {
		t.Errorf("expected Files Modified section, got:\n%s", out)
	}
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "b.go") {
		t.Errorf("expected a.go and b.go listed, got:\n%s", out)
	}
}

func TestOutputSummary_applyPatch(t *testing.T) {
	patchText := `*** Begin Patch
*** Add File: newfile.txt
+Hello
*** Update File: existing.go
@@ func foo():
-bar
+baz
*** Delete File: old.go
*** End Patch`
	args, _ := json.Marshal(map[string]string{
		"patchText": patchText,
	})
	msgs := []agent.Message{
		{
			Role: "assistant",
			ToolCalls: []agent.ToolCall{
				{ID: "call1", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "apply_patch", Arguments: string(args)}},
			},
		},
	}
	out, err := captureOutputSummary(msgs, "", "", time.Now())
	if err != nil {
		t.Fatalf("outputSummary error: %v", err)
	}
	if !strings.Contains(out, "Files Created") {
		t.Errorf("expected Files Created for patch add, got:\n%s", out)
	}
	if !strings.Contains(out, "Files Modified") {
		t.Errorf("expected Files Modified for patch update, got:\n%s", out)
	}
	if !strings.Contains(out, "Files Deleted") {
		t.Errorf("expected Files Deleted for patch delete, got:\n%s", out)
	}
	if !strings.Contains(out, "newfile.txt") {
		t.Errorf("expected newfile.txt, got:\n%s", out)
	}
	if !strings.Contains(out, "existing.go") {
		t.Errorf("expected existing.go, got:\n%s", out)
	}
	if !strings.Contains(out, "old.go") {
		t.Errorf("expected old.go, got:\n%s", out)
	}
}

func TestOutputSummary_writeThenDelete(t *testing.T) {
	writeArgs, _ := json.Marshal(map[string]string{
		"path":    "temporary.txt",
		"content": "temp data",
	})
	deleteArgs, _ := json.Marshal(map[string]string{
		"path": "temporary.txt",
	})
	msgs := []agent.Message{
		{
			Role: "assistant",
			ToolCalls: []agent.ToolCall{
				{ID: "w1", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "write", Arguments: string(writeArgs)}},
			},
		},
		{Role: "tool", ToolID: "w1", Content: "wrote"},
		{
			Role: "assistant",
			ToolCalls: []agent.ToolCall{
				{ID: "d1", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "delete", Arguments: string(deleteArgs)}},
			},
		},
	}
	out, err := captureOutputSummary(msgs, "", "", time.Now())
	if err != nil {
		t.Fatalf("outputSummary error: %v", err)
	}
	if strings.Contains(out, "Files Created") {
		t.Errorf("write-then-delete should NOT show under Created, got:\n%s", out)
	}
	if !strings.Contains(out, "Files Deleted") {
		t.Errorf("write-then-delete should show under Deleted, got:\n%s", out)
	}
	if !strings.Contains(out, "temporary.txt") {
		t.Errorf("expected temporary.txt in Deleted, got:\n%s", out)
	}
}

func TestOutputSummary_durationDisplay(t *testing.T) {
	start := time.Now().Add(-2*time.Second - 500*time.Millisecond)
	msgs := []agent.Message{
		{Role: "assistant", Content: "done"},
	}
	out, err := captureOutputSummary(msgs, "", "", start)
	if err != nil {
		t.Fatalf("outputSummary error: %v", err)
	}
	if !strings.Contains(out, "Duration:") {
		t.Errorf("expected duration info, got:\n%s", out)
	}
}

func TestOutputSummary_toolCounts(t *testing.T) {
	wa, _ := json.Marshal(map[string]string{"path": "a.go", "content": "a"})
	wb, _ := json.Marshal(map[string]string{"path": "b.go", "content": "b"})
	ra, _ := json.Marshal(map[string]string{"path": "a.go"})
	msgs := []agent.Message{
		{
			Role: "assistant",
			ToolCalls: []agent.ToolCall{
				{ID: "w1", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "write", Arguments: string(wa)}},
				{ID: "w2", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "write", Arguments: string(wb)}},
			},
		},
		{
			Role: "assistant",
			ToolCalls: []agent.ToolCall{
				{ID: "r1", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "read", Arguments: string(ra)}},
			},
		},
	}
	out, err := captureOutputSummary(msgs, "", "", time.Now())
	if err != nil {
		t.Fatalf("outputSummary error: %v", err)
	}
	if !strings.Contains(out, "write") || !strings.Contains(out, "read") {
		t.Errorf("expected tool counts for write and read, got:\n%s", out)
	}
}

func TestParsePatchOps_valid(t *testing.T) {
	patch := `*** Begin Patch
*** Add File: new.txt
+content
*** Update File: old.go
@@ line:
-removed
+added
*** Delete File: gone.go
*** End Patch`
	ops := parsePatchOps(patch)
	if len(ops) != 3 {
		t.Fatalf("expected 3 ops, got %d: %v", len(ops), ops)
	}
	if ops[0].action != "add" || ops[0].path != "new.txt" {
		t.Errorf("expected add new.txt, got %+v", ops[0])
	}
	if ops[1].action != "update" || ops[1].path != "old.go" {
		t.Errorf("expected update old.go, got %+v", ops[1])
	}
	if ops[2].action != "delete" || ops[2].path != "gone.go" {
		t.Errorf("expected delete gone.go, got %+v", ops[2])
	}
}

func TestParsePatchOps_invalid(t *testing.T) {
	tests := []struct {
		name  string
		patch string
	}{
		{"missing markers", "some random text"},
		{"no begin", "*** End Patch"},
		{"no end", "*** Begin Patch\nsome content"},
		{"begin after end", "*** End Patch\n*** Begin Patch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops := parsePatchOps(tt.patch)
			if len(ops) != 0 {
				t.Errorf("expected 0 ops for %q, got %d", tt.name, len(ops))
			}
		})
	}
}

func TestParsePatchOps_weirdWhitespace(t *testing.T) {
	patch := "*** Begin Patch\n*** Add File:  spaced out.txt  \n*** End Patch"
	ops := parsePatchOps(patch)
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	if ops[0].path != "spaced out.txt" {
		t.Errorf("expected 'spaced out.txt', got %q", ops[0].path)
	}
}

func TestOutputSummary_invalidToolArgs(t *testing.T) {
	// Malformed JSON should not crash.
	msgs := []agent.Message{
		{
			Role: "assistant",
			ToolCalls: []agent.ToolCall{
				{ID: "bad", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "write", Arguments: "{invalid json}"}},
			},
		},
	}
	_, err := captureOutputSummary(msgs, "", "", time.Now())
	if err != nil {
		t.Fatalf("outputSummary should not error on invalid tool args: %v", err)
	}
}

func BenchmarkOutputSummary(b *testing.B) {
	args, _ := json.Marshal(map[string]string{"path": "main.go", "content": "package main"})
	msgs := []agent.Message{
		{
			Role: "assistant",
			ToolCalls: []agent.ToolCall{
				{ID: "c1", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "write", Arguments: string(args)}},
				{ID: "c2", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "edit", Arguments: string(args)}},
			},
		},
		{Role: "assistant", Content: "Done."},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = outputSummary(msgs, "", "", time.Now())
	}
}
