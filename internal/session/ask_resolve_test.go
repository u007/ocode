package session

// Regression tests for resolving an ask sentinel (PERMISSION_ASK /
// QUESTION_PROMPT) that is already on disk. The server's resolve handlers
// rewrite the sentinel row's content in memory with the user's answer and
// then continue the turn. Without rewriting the stored row too, every later
// save of that transcript overlaps the stored sentinel with different bytes:
// the sync save conflicts (ErrTranscriptConflict) and live snapshots drop,
// so the transcript freezes on disk at the sentinel while memory moves on
// (ses_2026-09-10-153254-b55bec37: memory 66 rows, disk stuck at 62).

import (
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

func askTranscript(sentinel string) []agent.Message {
	call := agent.ToolCall{ID: "call_1", Type: "function"}
	call.Function.Name = "question"
	return []agent.Message{
		{Role: "user", Content: "do it", UserSeq: 1},
		{Role: "assistant", ToolCalls: []agent.ToolCall{call}},
		{Role: "tool", ToolID: "call_1", Content: sentinel},
	}
}

func TestRewriteAskResultUnfreezesTranscript(t *testing.T) {
	for _, sentinel := range []string{tool.SentinelQuestionPrompt + "\n[]", tool.SentinelPermissionAsk + "{}"} {
		t.Run(sentinel[:8], func(t *testing.T) {
			projectRoot, sessionsDir := isolatedProjectRoot(t)
			id := "ses_ask-resolve"
			if err := SaveForDir(projectRoot, id, "", askTranscript(sentinel), nil); err != nil {
				t.Fatalf("seed: %v", err)
			}

			answered := askTranscript(sentinel)
			answered[2].Content = `[{"answer":"yes"}]`
			continued := append(append([]agent.Message(nil), answered...),
				agent.Message{Role: "assistant", Content: "done"})

			// The incident: the continuation's sync save conflicts on the
			// rewritten sentinel row and the disk transcript freezes.
			if err := SaveForDir(projectRoot, id, "", continued, nil); !isConflictErr(err) {
				t.Fatalf("expected the unrewritten continuation save to conflict, got: %v", err)
			}

			// Rewriting the stored sentinel row first makes disk and memory
			// agree, so the continuation persists (sync and live).
			if err := RewriteAskResultForDir(projectRoot, id, 2, answered[2]); err != nil {
				t.Fatalf("RewriteAskResultForDir: %v", err)
			}
			assertContents(t, readStoredContents(t, sessionsDir, id), "do it", "", `[{"answer":"yes"}]`)

			if err := saveToDir(sessionsDir, id, "", continued, nil, true, 0); err != nil {
				t.Fatalf("live save after rewrite: %v", err)
			}
			assertContents(t, readStoredContents(t, sessionsDir, id), "do it", "", `[{"answer":"yes"}]`, "done")

			more := append(append([]agent.Message(nil), continued...), agent.Message{Role: "user", Content: "thanks", UserSeq: 2})
			if err := SaveForDir(projectRoot, id, "", more, nil); err != nil {
				t.Fatalf("sync save after rewrite: %v", err)
			}
			assertContents(t, readStoredContents(t, sessionsDir, id), "do it", "", `[{"answer":"yes"}]`, "done", "thanks")
		})
	}
}

func TestRewriteAskResultRejectsNonAskRow(t *testing.T) {
	projectRoot, _ := isolatedProjectRoot(t)
	id := "ses_ask-resolve-guard"
	msgs := askTranscript(tool.SentinelQuestionPrompt + "\n[]")
	if err := SaveForDir(projectRoot, id, "", msgs, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Wrong seq (a user row), wrong tool id, and out-of-range seq all fail
	// loudly instead of rewriting history.
	if err := RewriteAskResultForDir(projectRoot, id, 0, agent.Message{Role: "tool", ToolID: "call_1", Content: "x"}); err == nil {
		t.Fatal("expected error rewriting a non-ask row")
	}
	if err := RewriteAskResultForDir(projectRoot, id, 2, agent.Message{Role: "tool", ToolID: "call_other", Content: "x"}); err == nil {
		t.Fatal("expected error on tool id mismatch")
	}
	if err := RewriteAskResultForDir(projectRoot, id, 9, agent.Message{Role: "tool", ToolID: "call_1", Content: "x"}); err == nil {
		t.Fatal("expected error on missing seq")
	}
	assertContents(t, readStoredContents(t, projectRootSessionsDir(t, projectRoot), id), "do it", "", tool.SentinelQuestionPrompt+"\n[]")
}

func projectRootSessionsDir(t *testing.T, projectRoot string) string {
	t.Helper()
	dir, err := GetStorageDirForPath(projectRoot)
	if err != nil {
		t.Fatalf("GetStorageDirForPath: %v", err)
	}
	return dir
}
