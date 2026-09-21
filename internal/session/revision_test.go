package session

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStoredRevisionForDirTracksWrites is the core contract of the
// cross-process session-change signal: a stored write (by any writer) must
// move the revision, and a read-only pass must not.
func TestStoredRevisionForDirTracksWrites(t *testing.T) {
	projectRoot, dir := isolatedProjectRoot(t)
	id := "ses_revision-track"

	// No stored session → the stable "never revalidate" empty token.
	rev, err := storedRevisionForDir(dir, id)
	if err != nil {
		t.Fatalf("missing session: %v", err)
	}
	if rev != "" {
		t.Fatalf("missing session revision = %q, want empty", rev)
	}

	mustSeedSession(t, dir, id, "q0", "a0")
	r1, err := storedRevisionForDir(dir, id)
	if err != nil {
		t.Fatalf("seeded revision: %v", err)
	}
	if r1 == "" {
		t.Fatal("seeded revision is empty")
	}

	// A read-only pass must be stable — otherwise every poll would refetch.
	r1b, err := storedRevisionForDir(dir, id)
	if err != nil {
		t.Fatalf("re-read revision: %v", err)
	}
	if r1b != r1 {
		t.Fatalf("revision moved without a write: %q -> %q", r1, r1b)
	}

	// An append by another writer moves it.
	if err := saveToDir(dir, id, "", liveMsgs("q0", "a0", "q1"), nil, false, 0); err != nil {
		t.Fatalf("append: %v", err)
	}
	r2, err := storedRevisionForDir(dir, id)
	if err != nil {
		t.Fatalf("post-append revision: %v", err)
	}
	if r2 == r1 {
		t.Fatalf("append did not move revision (%q)", r1)
	}

	// A synchronous shrink (compaction/truncate) moves it again — the case the
	// reported bug is about: a /compact in one ocode process must be visible to
	// the other.
	if err := ReplaceForDir(projectRoot, id, "", liveMsgs("summary"), nil); err != nil {
		t.Fatalf("replace: %v", err)
	}
	r3, err := storedRevisionForDir(dir, id)
	if err != nil {
		t.Fatalf("post-replace revision: %v", err)
	}
	if r3 == r2 {
		t.Fatalf("replace did not move revision (%q)", r2)
	}
}

// TestStoredRevisionForDirFallsBackForSchemaLessSqlite: a `.sqlite` file that
// exists but has no readable meta schema must not error out — the caller falls
// back to the file token so the poll keeps working across a migration/corrupt
// file instead of disabling sync for that session.
func TestStoredRevisionForDirFallsBackForSchemaLessSqlite(t *testing.T) {
	_, dir := isolatedProjectRoot(t)
	id := "ses_revision-badsqlite"
	if err := os.WriteFile(filepath.Join(dir, id+".sqlite"), []byte("not a database"), 0o644); err != nil {
		t.Fatalf("write bogus sqlite: %v", err)
	}

	rev, err := storedRevisionForDir(dir, id)
	if err != nil {
		t.Fatalf("schema-less sqlite revision: %v", err)
	}
	if rev == "" {
		t.Fatal("schema-less sqlite should fall back to the file token, got empty")
	}
}

// TestFileStoredRevisionMovesWithContent pins the legacy-format token
// component: mtime+size changes when the file changes.
func TestFileStoredRevisionMovesWithContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ses_legacy.ojsonl")
	if err := os.WriteFile(path, []byte("line1\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	before, err := fileStoredRevision(path)
	if err != nil {
		t.Fatalf("fileStoredRevision: %v", err)
	}
	if err := os.WriteFile(path, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatalf("append: %v", err)
	}
	after, err := fileStoredRevision(path)
	if err != nil {
		t.Fatalf("fileStoredRevision after: %v", err)
	}
	if before == after {
		t.Fatalf("file token did not move with content (%q)", before)
	}
}
