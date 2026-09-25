package session

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
)

func TestPendingRewindPreparePersistsOnDemandResource(t *testing.T) {
	messages := []agent.Message{
		{Role: "user", Content: "first", UserSeq: 1},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: "restore me", UserSeq: 2},
	}
	root, dir, id := seedPendingRewindSession(t, messages)

	if got := pendingRewindTableCount(t, dir, id); got != 0 {
		t.Fatalf("pending_rewinds should be on-demand, found %d rows before prepare", got)
	}
	beforeRevision, err := StoredRevisionForDir(root, id)
	if err != nil {
		t.Fatalf("StoredRevisionForDir before prepare: %v", err)
	}
	beforeMeta := readPendingRewindMeta(t, dir, id)
	beforeIndex := pendingRewindIndexMeta(t, dir, id)

	resource, err := PreparePendingRewindForDir(root, id, 2, "restore me", 2)
	if err != nil {
		t.Fatalf("PreparePendingRewindForDir: %v", err)
	}
	if resource.Status != PendingRewindStatusArmed {
		t.Fatalf("status = %q, want %q", resource.Status, PendingRewindStatusArmed)
	}
	if resource.SessionID != id || resource.TargetIndex != 2 || resource.UserSeq != 2 {
		t.Fatalf("resource identity mismatch: %+v", resource)
	}
	if resource.CommittedUserSeq != 0 {
		t.Fatalf("armed resource has committed_user_seq %d", resource.CommittedUserSeq)
	}
	if got := resource.ExpiresAt.Sub(resource.CreatedAt); got != 24*time.Hour {
		t.Fatalf("expiry = %v, want 24h", got)
	}
	rawToken, err := hex.DecodeString(resource.Token)
	if err != nil {
		t.Fatalf("token is not hex: %v", err)
	}
	if len(rawToken) != 32 {
		t.Fatalf("token carries %d bytes, want 256 bits", len(rawToken))
	}
	wantFingerprint, err := FingerprintRawMessages(messages)
	if err != nil {
		t.Fatalf("FingerprintRawMessages: %v", err)
	}
	if resource.Fingerprint != wantFingerprint {
		t.Fatalf("fingerprint = %x, want %x", resource.Fingerprint, wantFingerprint)
	}
	if got := pendingRewindTableCount(t, dir, id); got != 1 {
		t.Fatalf("pending row count = %d, want 1", got)
	}

	got, err := PendingRewindStatusForDir(root, id, resource.Token)
	if err != nil {
		t.Fatalf("PendingRewindStatusForDir: %v", err)
	}
	if got.Token != resource.Token || got.Status != resource.Status || got.Fingerprint != resource.Fingerprint {
		t.Fatalf("status round-trip mismatch: got %+v want %+v", got, resource)
	}

	afterRevision, err := StoredRevisionForDir(root, id)
	if err != nil {
		t.Fatalf("StoredRevisionForDir after prepare: %v", err)
	}
	if afterRevision != beforeRevision {
		t.Fatalf("prepare moved stored revision %q -> %q", beforeRevision, afterRevision)
	}
	if afterMeta := readPendingRewindMeta(t, dir, id); afterMeta != beforeMeta {
		t.Fatalf("prepare changed meta: before %+v after %+v", beforeMeta, afterMeta)
	}
	if afterIndex := pendingRewindIndexMeta(t, dir, id); !afterIndex.Equal(beforeIndex) {
		t.Fatalf("prepare changed index ordering timestamp: %v -> %v", beforeIndex, afterIndex)
	}
}

func TestPendingRewindPrepareDoesNotUpgradeLegacyMetaSchema(t *testing.T) {
	root, dir := isolatedPendingRewindProject(t)
	id := NewSessionID()
	now := time.Now()
	db, err := openDB(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE meta (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			title_generated INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			metadata_json TEXT NOT NULL DEFAULT '{}'
		);
		CREATE TABLE messages (
			seq INTEGER PRIMARY KEY,
			data TEXT NOT NULL
		)`); err != nil {
		db.Close()
		t.Fatalf("create legacy sqlite schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO meta (id, created_at, updated_at) VALUES (?, ?, ?)`, id, now, now); err != nil {
		db.Close()
		t.Fatalf("insert legacy meta: %v", err)
	}
	messageJSON, err := json.Marshal(agent.Message{Role: "user", Content: "legacy schema"})
	if err != nil {
		db.Close()
		t.Fatalf("marshal legacy message: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO messages (seq, data) VALUES (0, ?)`, string(messageJSON)); err != nil {
		db.Close()
		t.Fatalf("insert legacy message: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy sqlite: %v", err)
	}
	if err := refreshIndexRow(dir, id); err != nil {
		t.Fatalf("refresh legacy index row: %v", err)
	}

	before, err := StoredRevisionForDir(root, id)
	if err != nil {
		t.Fatalf("StoredRevisionForDir before prepare: %v", err)
	}
	if _, err := PreparePendingRewindForDir(root, id, 0, "legacy schema", 0); err == nil {
		t.Fatal("prepare accepted schema-less sqlite whose revision depends on file mtime/size")
	}
	after, err := StoredRevisionForDir(root, id)
	if err != nil {
		t.Fatalf("StoredRevisionForDir after prepare: %v", err)
	}
	if after != before {
		t.Fatalf("prepare upgraded legacy meta schema and moved revision %q -> %q", before, after)
	}
}

func TestPendingRewindPrepareReplacesSingleResourceWithoutUpsert(t *testing.T) {
	messages := []agent.Message{
		{Role: "user", Content: "one", UserSeq: 1},
		{Role: "assistant", Content: "reply"},
		{Role: "user", Content: "two", UserSeq: 2},
	}
	root, dir, id := seedPendingRewindSession(t, messages)

	first, err := PreparePendingRewindForDir(root, id, 0, "one", 1)
	if err != nil {
		t.Fatalf("first prepare: %v", err)
	}
	second, err := PreparePendingRewindForDir(root, id, 2, "two", 2)
	if err != nil {
		t.Fatalf("replacement prepare: %v", err)
	}
	if first.Token == second.Token {
		t.Fatal("replacement reused the old token")
	}
	if _, err := PendingRewindStatusForDir(root, id, first.Token); !errors.Is(err, ErrPendingRewindNotFound) {
		t.Fatalf("old token status error = %v, want ErrPendingRewindNotFound", err)
	}
	if got := pendingRewindTableCount(t, dir, id); got != 1 {
		t.Fatalf("replacement left %d rows, want 1", got)
	}

	source, err := os.ReadFile("pending_rewind.go")
	if err != nil {
		t.Fatalf("read pending_rewind.go source guard: %v", err)
	}
	upsert := regexp.MustCompile(`(?is)ON\s+CONFLICT\b.*\bDO\s+UPDATE\b`)
	if upsert.Match(source) {
		t.Fatal("pending rewind replacement uses forbidden INSERT ... ON CONFLICT ... DO UPDATE")
	}
}

func TestPendingRewindCancelPreservesRevisionAndSessionOrdering(t *testing.T) {
	messages := []agent.Message{{Role: "user", Content: "cancel me", UserSeq: 1}}
	root, dir, id := seedPendingRewindSession(t, messages)
	otherID := "ses_pending-rewind-other"
	otherMessages := []agent.Message{{Role: "user", Content: "other", UserSeq: 1}}
	seedPendingRewindSessionAt(t, dir, otherID, otherMessages, time.Now().Add(-time.Hour))

	beforeRefs, err := ListRefsForDir(root)
	if err != nil {
		t.Fatalf("ListRefsForDir before: %v", err)
	}
	beforeRevision, err := StoredRevisionForDir(root, id)
	if err != nil {
		t.Fatalf("StoredRevisionForDir before: %v", err)
	}
	beforeMeta := readPendingRewindMeta(t, dir, id)
	beforeIndex := pendingRewindIndexMeta(t, dir, id)
	resource, err := PreparePendingRewindForDir(root, id, 0, "cancel me", 1)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := CancelPendingRewindForDir(root, id, resource.Token); err != nil {
		t.Fatalf("CancelPendingRewindForDir: %v", err)
	}
	if _, err := PendingRewindStatusForDir(root, id, resource.Token); !errors.Is(err, ErrPendingRewindNotFound) {
		t.Fatalf("cancelled token status error = %v, want ErrPendingRewindNotFound", err)
	}

	afterRevision, err := StoredRevisionForDir(root, id)
	if err != nil {
		t.Fatalf("StoredRevisionForDir after: %v", err)
	}
	if afterRevision != beforeRevision {
		t.Fatalf("cancel moved stored revision %q -> %q", beforeRevision, afterRevision)
	}
	if afterMeta := readPendingRewindMeta(t, dir, id); afterMeta != beforeMeta {
		t.Fatalf("cancel changed meta: before %+v after %+v", beforeMeta, afterMeta)
	}
	if afterIndex := pendingRewindIndexMeta(t, dir, id); !afterIndex.Equal(beforeIndex) {
		t.Fatalf("cancel changed index ordering timestamp: %v -> %v", beforeIndex, afterIndex)
	}
	afterRefs, err := ListRefsForDir(root)
	if err != nil {
		t.Fatalf("ListRefsForDir after: %v", err)
	}
	if !samePendingRewindRefOrder(beforeRefs, afterRefs) {
		t.Fatalf("cancel changed session ordering: before %v after %v", pendingRewindRefIDs(beforeRefs), pendingRewindRefIDs(afterRefs))
	}
}

func TestPendingRewindExpiryAndCommittedDiscovery(t *testing.T) {
	t.Run("missing table is not found", func(t *testing.T) {
		root, _, id := seedPendingRewindSession(t, []agent.Message{{Role: "user", Content: "hello", UserSeq: 1}})
		if _, err := PendingRewindStatusForDir(root, id, "not-a-token"); !errors.Is(err, ErrPendingRewindNotFound) {
			t.Fatalf("status error = %v, want ErrPendingRewindNotFound", err)
		}
	})

	t.Run("expired armed is invalid", func(t *testing.T) {
		messages := []agent.Message{
			{Role: "user", Content: "hello", UserSeq: 1},
			{Role: "assistant", Content: "reply"},
		}
		root, dir, id := seedPendingRewindSession(t, messages)
		resource, err := PreparePendingRewindForDir(root, id, 0, "hello", 1)
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		expirePendingRewind(t, dir, id, time.Now().Add(-time.Minute))
		expired, err := PendingRewindStatusForDir(root, id, resource.Token)
		if !errors.Is(err, ErrPendingRewindExpired) {
			t.Fatalf("status error = %v, want ErrPendingRewindExpired", err)
		}
		if expired == nil || expired.Status != PendingRewindStatusArmed {
			t.Fatalf("expired armed status = %+v, want armed resource", expired)
		}
		before := mustLoadRawPendingRewindMessages(t, dir, id)
		if _, err := CommitPendingRewindForDir(root, id, resource.Token, "replacement"); !errors.Is(err, ErrPendingRewindExpired) {
			t.Fatalf("commit error = %v, want ErrPendingRewindExpired", err)
		}
		assertPendingRewindMessages(t, before, mustLoadRawPendingRewindMessages(t, dir, id))
	})

	t.Run("expired committed remains discoverable", func(t *testing.T) {
		messages := []agent.Message{
			{Role: "user", Content: "hello", UserSeq: 1},
			{Role: "assistant", Content: "reply"},
		}
		root, dir, id := seedPendingRewindSession(t, messages)
		resource, err := PreparePendingRewindForDir(root, id, 0, "hello", 1)
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		commit, err := CommitPendingRewindForDir(root, id, resource.Token, "edited hello")
		if err != nil {
			t.Fatalf("commit: %v", err)
		}
		if _, err := PreparePendingRewindForDir(root, id, 0, "edited hello", 1); !errors.Is(err, ErrPendingRewindAlreadyCommitted) {
			t.Fatalf("prepare during committed recovery window = %v, want ErrPendingRewindAlreadyCommitted", err)
		}
		retained, err := PendingRewindStatusForDir(root, id, resource.Token)
		if err != nil {
			t.Fatalf("retained committed status: %v", err)
		}
		if retained.Status != PendingRewindStatusCommitted {
			t.Fatalf("retained status = %q, want committed", retained.Status)
		}
		expirePendingRewind(t, dir, id, time.Now().Add(-time.Hour))
		got, err := PendingRewindStatusForDir(root, id, resource.Token)
		if err != nil {
			t.Fatalf("committed status after expiry: %v", err)
		}
		if got.Status != PendingRewindStatusCommitted || got.CommittedUserSeq != commit.Rewind.CommittedUserSeq {
			t.Fatalf("committed status = %+v, want committed seq %d", got, commit.Rewind.CommittedUserSeq)
		}
	})
}

func TestPendingRewindCommitResolvesUserSeqAndCommitsAtomically(t *testing.T) {
	messages := []agent.Message{
		{Role: "user", Content: "keep", UserSeq: 4},
		{Role: "assistant", Content: "before target"},
		{Role: "user", Content: "replace target", UserSeq: 7},
		{Role: "assistant", Content: "discard me"},
		{Role: "user", Content: "later", UserSeq: 9},
	}
	root, dir, id := seedPendingRewindSession(t, messages)
	beforeMeta := readPendingRewindMeta(t, dir, id)

	// A deliberately wrong absolute index proves durable user_seq wins.
	resource, err := PreparePendingRewindForDir(root, id, 999, "replace target", 7)
	if err != nil {
		t.Fatalf("prepare by user_seq: %v", err)
	}
	if resource.TargetIndex != 2 {
		t.Fatalf("resolved target index = %d, want 2", resource.TargetIndex)
	}
	commit, err := CommitPendingRewindForDir(root, id, resource.Token, "edited replacement")
	if err != nil {
		t.Fatalf("CommitPendingRewindForDir: %v", err)
	}
	if commit.Rewind.Status != PendingRewindStatusCommitted || commit.Rewind.CommittedUserSeq != 5 {
		t.Fatalf("committed resource = %+v, want user_seq 5", commit.Rewind)
	}
	if len(commit.KeptPrefix) != 2 || commit.KeptPrefix[0].Content != "keep" || commit.KeptPrefix[1].Content != "before target" {
		t.Fatalf("kept prefix = %+v", commit.KeptPrefix)
	}

	raw := mustLoadRawPendingRewindMessages(t, dir, id)
	want := []agent.Message{
		{Role: "user", Content: "keep", UserSeq: 4},
		{Role: "assistant", Content: "before target"},
		{Role: "user", Content: "edited replacement", UserSeq: 5},
	}
	assertPendingRewindMessages(t, want, raw)
	afterMeta := readPendingRewindMeta(t, dir, id)
	if afterMeta.HistoryGen != beforeMeta.HistoryGen+1 {
		t.Fatalf("history_gen = %d, want %d", afterMeta.HistoryGen, beforeMeta.HistoryGen+1)
	}
	if !afterMeta.UpdatedAt.After(beforeMeta.UpdatedAt) {
		t.Fatalf("updated_at did not advance: %v -> %v", beforeMeta.UpdatedAt, afterMeta.UpdatedAt)
	}

	// A response-loss retry is refused with the committed resource so callers
	// can recover acceptance without appending or starting a second turn.
	retry, err := CommitPendingRewindForDir(root, id, resource.Token, "edited replacement")
	if !errors.Is(err, ErrPendingRewindAlreadyCommitted) {
		t.Fatalf("commit retry error = %v, want ErrPendingRewindAlreadyCommitted", err)
	}
	if retry == nil || retry.Rewind.CommittedUserSeq != commit.Rewind.CommittedUserSeq {
		t.Fatalf("commit retry result = %+v, want committed seq %d", retry, commit.Rewind.CommittedUserSeq)
	}
	assertPendingRewindMessages(t, want, mustLoadRawPendingRewindMessages(t, dir, id))
}

func TestPendingRewindLegacyTargetRequiresExactIndexAndContent(t *testing.T) {
	messages := []agent.Message{
		{Role: "user", Content: "legacy target"},
		{Role: "assistant", Content: "discard me"},
	}
	root, dir, id := seedPendingRewindSession(t, messages)
	if _, err := PreparePendingRewindForDir(root, id, 0, "different content", 0); !errors.Is(err, ErrPendingRewindInvalidTarget) {
		t.Fatalf("wrong-content prepare error = %v, want ErrPendingRewindInvalidTarget", err)
	}
	if got := pendingRewindTableCount(t, dir, id); got != 0 {
		t.Fatalf("invalid prepare persisted %d resources, want 0", got)
	}
	resource, err := PreparePendingRewindForDir(root, id, 0, "legacy target", 0)
	if err != nil {
		t.Fatalf("legacy prepare: %v", err)
	}
	if resource.TargetIndex != 0 || resource.UserSeq != 0 {
		t.Fatalf("legacy target = %+v", resource)
	}
	if _, err := CommitPendingRewindForDir(root, id, resource.Token, "edited legacy target"); err != nil {
		t.Fatalf("legacy commit: %v", err)
	}
	assertPendingRewindMessages(t, []agent.Message{{Role: "user", Content: "edited legacy target", UserSeq: 1}}, mustLoadRawPendingRewindMessages(t, dir, id))
}

func TestPendingRewindFingerprintUsesOnlyRawMessages(t *testing.T) {
	messages := []agent.Message{
		{Role: "user", Content: "same transcript", UserSeq: 1},
		{Role: "assistant", Content: "same reply"},
	}
	root, _, id := seedPendingRewindSession(t, messages)
	resource, err := PreparePendingRewindForDir(root, id, 0, "same transcript", 1)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := ReplaceForDir(root, id, "changed title", messages, map[string]any{"unrelated": "metadata"}); err != nil {
		t.Fatalf("metadata-only replace: %v", err)
	}
	if _, err := CommitPendingRewindForDir(root, id, resource.Token, "edited same transcript"); err != nil {
		t.Fatalf("metadata change invalidated message fingerprint: %v", err)
	}
}

func TestPendingRewindFingerprintMismatchMarksStaleWithoutChangingMessages(t *testing.T) {
	messages := []agent.Message{
		{Role: "user", Content: "target", UserSeq: 1},
		{Role: "assistant", Content: "old reply"},
	}
	root, dir, id := seedPendingRewindSession(t, messages)
	resource, err := PreparePendingRewindForDir(root, id, 0, "target", 1)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	changed := append(slices.Clone(messages), agent.Message{Role: "assistant", Content: "unseen append"})
	if err := ReplaceForDir(root, id, "", changed, nil); err != nil {
		t.Fatalf("append after prepare: %v", err)
	}
	before := mustLoadRawPendingRewindMessages(t, dir, id)
	if _, err := CommitPendingRewindForDir(root, id, resource.Token, "must not commit"); !errors.Is(err, ErrPendingRewindStale) {
		t.Fatalf("stale commit error = %v, want ErrPendingRewindStale", err)
	}
	assertPendingRewindMessages(t, before, mustLoadRawPendingRewindMessages(t, dir, id))
	status, err := PendingRewindStatusForDir(root, id, resource.Token)
	if err != nil {
		t.Fatalf("stale status: %v", err)
	}
	if status.Status != PendingRewindStatusStale {
		t.Fatalf("status = %q, want stale", status.Status)
	}
	if _, err := CommitPendingRewindForDir(root, id, resource.Token, "still must not commit"); !errors.Is(err, ErrPendingRewindStale) {
		t.Fatalf("second stale commit error = %v, want ErrPendingRewindStale", err)
	}
	assertPendingRewindMessages(t, before, mustLoadRawPendingRewindMessages(t, dir, id))
}

func TestPendingRewindCommitRollsBackEntirelyOnInsertFailure(t *testing.T) {
	messages := []agent.Message{
		{Role: "user", Content: "before", UserSeq: 1},
		{Role: "assistant", Content: "before reply"},
		{Role: "user", Content: "target", UserSeq: 2},
	}
	root, dir, id := seedPendingRewindSession(t, messages)
	resource, err := PreparePendingRewindForDir(root, id, 2, "target", 2)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	installPendingRewindFailInsertTrigger(t, dir, id, 2)
	beforeMessages := mustLoadRawPendingRewindMessages(t, dir, id)
	beforeMeta := readPendingRewindMeta(t, dir, id)

	if _, err := CommitPendingRewindForDir(root, id, resource.Token, "must roll back"); err == nil {
		t.Fatal("commit succeeded despite induced insert failure")
	}
	assertPendingRewindMessages(t, beforeMessages, mustLoadRawPendingRewindMessages(t, dir, id))
	if afterMeta := readPendingRewindMeta(t, dir, id); afterMeta != beforeMeta {
		t.Fatalf("failed commit changed meta: before %+v after %+v", beforeMeta, afterMeta)
	}
	status, err := PendingRewindStatusForDir(root, id, resource.Token)
	if err != nil {
		t.Fatalf("status after rollback: %v", err)
	}
	if status.Status != PendingRewindStatusArmed {
		t.Fatalf("status after rollback = %q, want armed", status.Status)
	}

	dropPendingRewindFailInsertTrigger(t, dir, id)
	if _, err := CommitPendingRewindForDir(root, id, resource.Token, "retry succeeds"); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
}

func TestPendingRewindCommitSucceedsWhenIndexRefreshFails(t *testing.T) {
	messages := []agent.Message{
		{Role: "user", Content: "before", UserSeq: 1},
		{Role: "assistant", Content: "before reply"},
		{Role: "user", Content: "target", UserSeq: 2},
		{Role: "assistant", Content: "target reply"},
	}
	root, dir, id := seedPendingRewindSession(t, messages)
	resource, err := PreparePendingRewindForDir(root, id, 2, "target", 2)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	// The per-session SQLite transaction is authoritative. A broken shared
	// project index is a nonessential listing failure after that transaction,
	// not a reason to report that the committed user message was not accepted.
	if err := os.WriteFile(filepath.Join(dir, "index.sqlite"), []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatalf("corrupt project index: %v", err)
	}

	commit, err := CommitPendingRewindForDir(root, id, resource.Token, "edited target")
	if err != nil {
		t.Fatalf("commit returned an error after its durable transaction succeeded: %v", err)
	}
	if commit.Rewind.Status != PendingRewindStatusCommitted {
		t.Fatalf("commit status = %q, want committed", commit.Rewind.Status)
	}
	assertPendingRewindMessages(t, []agent.Message{
		{Role: "user", Content: "before", UserSeq: 1},
		{Role: "assistant", Content: "before reply"},
		{Role: "user", Content: "edited target", UserSeq: 2},
	}, mustLoadRawPendingRewindMessages(t, dir, id))
}

func TestPendingRewindLegacyMigrationPreservesRawRows(t *testing.T) {
	toolCall := agent.ToolCall{ID: "call_1", Type: "function"}
	toolCall.Function.Name = "bash"
	toolCall.Function.Arguments = "{}"
	messages := []agent.Message{
		{Role: "user", Content: "legacy target", UserSeq: 1},
		{Role: "assistant", ToolCalls: []agent.ToolCall{toolCall}},
		{Role: "tool", ToolID: "call_1", Content: "result"},
		{Role: "tool", ToolID: "orphan", Content: "must survive"},
	}

	for _, format := range []string{"json", "ojsonl"} {
		t.Run(format, func(t *testing.T) {
			root, dir := isolatedPendingRewindProject(t)
			id := NewSessionID()
			expected := slices.Clone(messages)
			targetUserSeq := 1
			legacyPath := filepath.Join(dir, id+".json")
			if format == "ojsonl" {
				// The legacy ojsonl record predates durable user_seq fields, so
				// this format intentionally exercises index+content fallback.
				targetUserSeq = 0
				expected[0].UserSeq = 0
				legacyPath = ojsonlSessionPath(dir, id)
				if err := saveOjsonl(dir, id, "legacy", messages, nil); err != nil {
					t.Fatalf("seed ojsonl: %v", err)
				}
			} else {
				data, err := json.Marshal(Session{ID: id, Title: "legacy", Messages: messages, Metadata: map[string]any{"kept": true}})
				if err != nil {
					t.Fatalf("marshal legacy json: %v", err)
				}
				if err := os.WriteFile(legacyPath, data, 0o600); err != nil {
					t.Fatalf("write legacy json: %v", err)
				}
			}

			if _, err := PreparePendingRewindForDir(root, id, 0, "legacy target", targetUserSeq); err != nil {
				t.Fatalf("prepare migrates %s: %v", format, err)
			}
			if fileExists(legacyPath) {
				t.Fatalf("legacy %s source still exists after verified migration", format)
			}
			if !fileExists(sqliteSessionPath(dir, id)) {
				t.Fatalf("legacy %s did not migrate to sqlite", format)
			}
			got := mustLoadRawPendingRewindMessages(t, dir, id)
			assertPendingRewindMessages(t, expected, got)
		})
	}
}

func TestPendingRewindRekeyMovesResourceAcrossSessionDatabase(t *testing.T) {
	messages := []agent.Message{
		{Role: "user", Content: "before", UserSeq: 1},
		{Role: "assistant", Content: "reply"},
		{Role: "user", Content: "target", UserSeq: 2},
	}
	root, dir, oldID := seedPendingRewindSession(t, messages)
	db, err := openDB(sqliteSessionPath(dir, oldID))
	if err != nil {
		t.Fatalf("open sqlite for non-canonical raw row: %v", err)
	}
	if _, err := db.Exec(`UPDATE messages SET data = ? WHERE seq = 0`, ` { "role" : "user", "content" : "before", "user_seq" : 1 } `); err != nil {
		db.Close()
		t.Fatalf("write non-canonical raw row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite after non-canonical raw row: %v", err)
	}
	prepared, err := PreparePendingRewindForDir(root, oldID, 2, "target", 2)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	newID, err := RekeyForDir(root, oldID)
	if err != nil {
		t.Fatalf("RekeyForDir: %v", err)
	}
	if _, err := PendingRewindStatusForDir(root, oldID, prepared.Token); !errors.Is(err, ErrPendingRewindNotFound) {
		t.Fatalf("old session token error = %v, want ErrPendingRewindNotFound", err)
	}
	moved, err := PendingRewindStatusForDir(root, newID, prepared.Token)
	if err != nil {
		t.Fatalf("moved resource status: %v", err)
	}
	newRaw := mustLoadRawPendingRewindMessages(t, dir, newID)
	wantFingerprint, err := FingerprintRawMessages(newRaw)
	if err != nil {
		t.Fatalf("fingerprint rekeyed transcript: %v", err)
	}
	if moved.SessionID != newID || moved.Token != prepared.Token || moved.TargetIndex != 2 || moved.UserSeq != 2 || moved.Fingerprint != wantFingerprint {
		t.Fatalf("moved resource mismatch: got %+v want rekeyed fingerprint %x", moved, wantFingerprint)
	}
	if got := pendingRewindTableCount(t, dir, newID); got != 1 {
		t.Fatalf("rekeyed database has %d pending rows, want 1", got)
	}
	if _, err := CommitPendingRewindForDir(root, newID, prepared.Token, "edited target"); err != nil {
		t.Fatalf("commit moved resource: %v", err)
	}
}

func TestRawTranscriptFingerprintUsesSequenceAndLengthBoundaries(t *testing.T) {
	rows := []rawTranscriptRow{
		{Seq: 1, Data: `{"role":"user","content":"ab"}`},
		{Seq: 2, Data: `{"role":"assistant","content":"c"}`},
	}
	want := fingerprintRawTranscriptRows(rows)
	if got := fingerprintRawTranscriptRows(slices.Clone(rows)); got != want {
		t.Fatalf("same rows produced different fingerprints %x != %x", got, want)
	}
	changedBoundary := []rawTranscriptRow{
		{Seq: 1, Data: `{"role":"user","content":"a"}`},
		{Seq: 2, Data: `{"role":"assistant","content":"bc"}`},
	}
	if got := fingerprintRawTranscriptRows(changedBoundary); got == want {
		t.Fatal("length boundaries did not distinguish split JSON message data")
	}
	changedSequence := []rawTranscriptRow{
		{Seq: 1, Data: `{"role":"user","content":"ab"}`},
		{Seq: 22, Data: `{"role":"assistant","content":"c"}`},
	}
	if got := fingerprintRawTranscriptRows(changedSequence); got == want {
		t.Fatal("sequence boundaries did not affect fingerprint")
	}
}

type pendingRewindMetaSnapshot struct {
	UpdatedAt  time.Time
	HistoryGen int64
}

func seedPendingRewindSession(t *testing.T, messages []agent.Message) (string, string, string) {
	t.Helper()
	root, dir := isolatedPendingRewindProject(t)
	id := NewSessionID()
	seedPendingRewindSessionAt(t, dir, id, messages, time.Now())
	return root, dir, id
}

func isolatedPendingRewindProject(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	dir, err := GetStorageDirForPath(root)
	if err != nil {
		t.Fatalf("GetStorageDirForPath: %v", err)
	}
	return root, dir
}

func seedPendingRewindSessionAt(t *testing.T, dir, id string, messages []agent.Message, updatedAt time.Time) {
	t.Helper()
	if err := writeSqliteSessionFull(dir, Session{
		ID:        id,
		Title:     "pending rewind test",
		Messages:  messages,
		CreatedAt: updatedAt,
		UpdatedAt: updatedAt,
	}); err != nil {
		t.Fatalf("writeSqliteSessionFull(%s): %v", id, err)
	}
	if err := refreshIndexRow(dir, id); err != nil {
		t.Fatalf("refreshIndexRow(%s): %v", id, err)
	}
}

func pendingRewindTableCount(t *testing.T, dir, id string) int {
	t.Helper()
	db, err := openDBRaw(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("openDBRaw: %v", err)
	}
	defer db.Close()
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'pending_rewinds'`).Scan(&exists); err != nil {
		t.Fatalf("inspect pending_rewinds table: %v", err)
	}
	if exists == 0 {
		return 0
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pending_rewinds`).Scan(&count); err != nil {
		t.Fatalf("count pending_rewinds: %v", err)
	}
	return count
}

func readPendingRewindMeta(t *testing.T, dir, id string) pendingRewindMetaSnapshot {
	t.Helper()
	db, err := openDBRaw(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("openDBRaw: %v", err)
	}
	defer db.Close()
	var meta pendingRewindMetaSnapshot
	if err := db.QueryRow(`SELECT updated_at, history_gen FROM meta WHERE id = ?`, id).Scan(&meta.UpdatedAt, &meta.HistoryGen); err != nil {
		t.Fatalf("read session meta: %v", err)
	}
	return meta
}

func pendingRewindIndexMeta(t *testing.T, dir, id string) time.Time {
	t.Helper()
	metas, err := queryIndexMetas(dir)
	if err != nil {
		t.Fatalf("queryIndexMetas: %v", err)
	}
	for _, meta := range metas {
		if meta.ID == id {
			return meta.UpdatedAt
		}
	}
	t.Fatalf("session %s missing from index", id)
	return time.Time{}
}

func samePendingRewindRefOrder(a, b []Ref) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
	}
	return true
}

func pendingRewindRefIDs(refs []Ref) []string {
	ids := make([]string, len(refs))
	for i, ref := range refs {
		ids[i] = ref.ID
	}
	return ids
}

func expirePendingRewind(t *testing.T, dir, id string, at time.Time) {
	t.Helper()
	db, err := openDB(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE pending_rewinds SET expires_at = ? WHERE session_id = ?`, at, id); err != nil {
		t.Fatalf("expire pending rewind: %v", err)
	}
}

func mustLoadRawPendingRewindMessages(t *testing.T, dir, id string) []agent.Message {
	t.Helper()
	messages, err := loadRawMessages(dir, id)
	if err != nil {
		t.Fatalf("loadRawMessages: %v", err)
	}
	return messages
}

func assertPendingRewindMessages(t *testing.T, want, got []agent.Message) {
	t.Helper()
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal expected messages: %v", err)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal actual messages: %v", err)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("messages mismatch:\n got %s\nwant %s", gotJSON, wantJSON)
	}
}

func installPendingRewindFailInsertTrigger(t *testing.T, dir, id string, seq int) {
	t.Helper()
	db, err := openDB(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer db.Close()
	triggerSQL := fmt.Sprintf(`CREATE TRIGGER pending_rewind_fail_insert BEFORE INSERT ON messages WHEN NEW.seq = %d BEGIN SELECT RAISE(ABORT, 'induced pending rewind failure'); END`, seq)
	if _, err := db.Exec(triggerSQL); err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}
}

func dropPendingRewindFailInsertTrigger(t *testing.T, dir, id string) {
	t.Helper()
	db, err := openDB(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP TRIGGER pending_rewind_fail_insert`); err != nil {
		t.Fatalf("drop failure trigger: %v", err)
	}
}
