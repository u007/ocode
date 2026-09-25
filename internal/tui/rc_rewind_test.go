package tui

import (
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/server"
	"github.com/u007/ocode/internal/session"
)

// rcRewindFixture seeds a durable session under an isolated project root and
// returns the model plus the armed rewind token. It exercises the TUI's own
// commit path (commitRCRequestRewind) the same way a bridged web/desktop /rc
// send does, without running the agent turn.
func rcRewindFixture(t *testing.T, messages []agent.Message, targetIndex int, targetContent string, userSeq int) (model, string, string) {
	t.Helper()
	// Manage HOME ourselves instead of t.TempDir: background session workers
	// (live-persist, index refresh) can still be writing their sqlite WAL when
	// t.TempDir's RemoveAll runs, which fails the test with "directory not
	// empty". Drain first, then remove best-effort so a late writer cannot
	// fail an otherwise-green test.
	home, err := os.MkdirTemp("", "ocode-rc-rewind-home-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Setenv("HOME", home)
	t.Cleanup(func() {
		_ = session.FlushAll(2 * time.Second)
		_ = os.RemoveAll(home)
	})
	root := t.TempDir()
	id := session.NewSessionID()
	if err := session.SaveForDir(root, id, "rc rewind test", messages, nil); err != nil {
		t.Fatalf("SaveForDir: %v", err)
	}
	resource, err := session.PreparePendingRewindForDir(root, id, targetIndex, targetContent, userSeq)
	if err != nil {
		t.Fatalf("PreparePendingRewindForDir: %v", err)
	}

	m := replacementTestModel()
	m.workDir = root
	m.sessionID = id
	// Seed the in-memory transcript with the same rows so the rebuild is a
	// genuine replacement, not an append onto an empty model.
	for _, am := range messages {
		if am.Role == "user" {
			copyMsg := am
			m.messages = append(m.messages, message{role: roleUser, text: am.Content, raw: &copyMsg})
			continue
		}
		m.appendAgentMessage(am)
	}
	return m, id, resource.Token
}

func TestCommitRCRequestRewindReplacesTranscriptDurably(t *testing.T) {
	messages := []agent.Message{
		{Role: "user", Content: "keep me", UserSeq: 1},
		{Role: "assistant", Content: "answer before target"},
		{Role: "user", Content: "replace me", UserSeq: 2},
		{Role: "assistant", Content: "discard me"},
	}
	m, id, token := rcRewindFixture(t, messages, 2, "replace me", 2)

	seq, err := m.commitRCRequestRewind(server.RCRequest{Content: "edited", RewindToken: token})
	if err != nil {
		t.Fatalf("commitRCRequestRewind: %v", err)
	}
	if seq != 2 {
		t.Fatalf("committed user seq = %d, want 2 (next after kept prefix's user seq 1)", seq)
	}

	loaded, err := session.LoadForDir(m.workDir, id)
	if err != nil {
		t.Fatalf("LoadForDir: %v", err)
	}
	var got []string
	for _, msg := range loaded.Messages {
		got = append(got, msg.Role+":"+msg.Content)
	}
	want := []string{
		"user:keep me",
		"assistant:answer before target",
		"user:edited",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("durable transcript = %v, want %v", got, want)
	}

	// The visible transcript mirrors the kept prefix only; the caller appends
	// the new user row once via the normal rc path.
	var visible []string
	for _, msg := range m.messages {
		visible = append(visible, msg.text)
	}
	wantVisible := []string{"keep me", "answer before target"}
	if !reflect.DeepEqual(visible, wantVisible) {
		t.Fatalf("visible transcript = %v, want %v", visible, wantVisible)
	}
	if len(m.messages) != 2 || m.messages[0].role != roleUser || m.messages[1].role != roleAssistant {
		t.Fatalf("visible roles = %+v, want [user assistant]", m.messages)
	}
}

func TestCommitRCRequestRewindFailureLeavesTranscriptUntouched(t *testing.T) {
	messages := []agent.Message{
		{Role: "user", Content: "keep me", UserSeq: 1},
		{Role: "assistant", Content: "answer before target"},
		{Role: "user", Content: "replace me", UserSeq: 2},
		{Role: "assistant", Content: "discard me"},
	}
	m, id, token := rcRewindFixture(t, messages, 2, "replace me", 2)

	// Consume the token once so the second commit fails as already-committed.
	if _, err := m.commitRCRequestRewind(server.RCRequest{Content: "first edit", RewindToken: token}); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	before, err := session.LoadForDir(m.workDir, id)
	if err != nil {
		t.Fatalf("LoadForDir before retry: %v", err)
	}

	_, err = m.commitRCRequestRewind(server.RCRequest{Content: "second edit", RewindToken: token})
	if !errors.Is(err, session.ErrPendingRewindAlreadyCommitted) {
		t.Fatalf("second commit error = %v, want ErrPendingRewindAlreadyCommitted", err)
	}

	after, err := session.LoadForDir(m.workDir, id)
	if err != nil {
		t.Fatalf("LoadForDir after retry: %v", err)
	}
	if !reflect.DeepEqual(messageContents(before.Messages), messageContents(after.Messages)) {
		t.Fatalf("failed retry changed transcript: before %v after %v",
			messageContents(before.Messages), messageContents(after.Messages))
	}
}

func messageContents(messages []agent.Message) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		out = append(out, msg.Role+":"+msg.Content)
	}
	return out
}
