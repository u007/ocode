package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// numberedLines returns "line<from>\n" through "line<to>\n".
func numberedLines(from, to int) string {
	var b strings.Builder
	for i := from; i <= to; i++ {
		fmt.Fprintf(&b, "line%d\n", i)
	}
	return b.String()
}

// gitThreeChunkMerge builds a real conflicted f.txt with THREE conflict regions.
// The edits sit on lines 2, 15 and 28, so each pair is separated by twelve
// unchanged lines. Git coalesces conflicts separated by only a few lines
// (three lines folded all three edits into one block), so a narrow gap would
// not exercise per-block resolution.
func gitThreeChunkMerge(t *testing.T) string {
	t.Helper()
	return gitThreeChunkMergeAt(t, "f.txt")
}

// gitThreeChunkMergeAt is gitThreeChunkMerge with the conflicted file at rel,
// a repo-relative path that may sit in a subdirectory.
func gitThreeChunkMergeAt(t *testing.T, rel string) string {
	t.Helper()
	dir := t.TempDir()
	initGitRepo(t, dir)
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	base := numberedLines(1, 30)
	writeFile(t, path, base)
	run(t, dir, "git", "add", "--", rel)
	run(t, dir, "git", "commit", "-m", "base")
	run(t, dir, "git", "switch", "-c", "other")
	writeFile(t, path, strings.NewReplacer("line2\n", "THEIRS-2\n", "line15\n", "THEIRS-15\n", "line28\n", "THEIRS-28\n").Replace(base))
	run(t, dir, "git", "commit", "-am", "theirs")
	run(t, dir, "git", "switch", "-")
	writeFile(t, path, strings.NewReplacer("line2\n", "OURS-2\n", "line15\n", "OURS-15\n", "line28\n", "OURS-28\n").Replace(base))
	run(t, dir, "git", "commit", "-am", "ours")
	out, err := gitCombined(t, dir, "git", "merge", "other")
	if err == nil {
		t.Fatalf("expected the merge to conflict, but it succeeded:\n%s", out)
	}
	return dir
}

// --- pure parser ---

func TestParseConflictBlocks(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []conflictBlock
		wantErr string
	}{
		{
			name:    "plain block",
			content: "a\n<<<<<<< HEAD\nO1\nO2\n=======\nT1\n>>>>>>> topic\nz\n",
			want: []conflictBlock{{
				start: 1, end: 6, oursLabel: "HEAD", theirsLabel: "topic",
				ours: []string{"O1", "O2"}, theirs: []string{"T1"},
			}},
		},
		{
			name:    "diff3 base section is dropped, not folded into ours",
			content: "<<<<<<< HEAD\nO\n||||||| merged common ancestors\nB1\nB2\n=======\nT\n>>>>>>> topic\n",
			want: []conflictBlock{{
				start: 0, end: 7, oursLabel: "HEAD", theirsLabel: "topic",
				ours: []string{"O"}, theirs: []string{"T"},
			}},
		},
		{
			name:    "CRLF markers parse and content keeps its carriage returns",
			content: "<<<<<<< HEAD\r\nO\r\n=======\r\nT\r\n>>>>>>> topic\r\n",
			want: []conflictBlock{{
				start: 0, end: 4, oursLabel: "HEAD", theirsLabel: "topic",
				ours: []string{"O\r"}, theirs: []string{"T\r"},
			}},
		},
		{
			name:    "closing marker on the last line without a newline",
			content: "<<<<<<< HEAD\nO\n=======\nT\n>>>>>>> topic",
			want: []conflictBlock{{
				start: 0, end: 4, oursLabel: "HEAD", theirsLabel: "topic",
				ours: []string{"O"}, theirs: []string{"T"},
			}},
		},
		{
			name:    "empty ours side",
			content: "<<<<<<< HEAD\n=======\nT\n>>>>>>> topic\n",
			want: []conflictBlock{{
				start: 0, end: 3, oursLabel: "HEAD", theirsLabel: "topic",
				theirs: []string{"T"},
			}},
		},
		{
			name:    "bare separator outside a block is content",
			content: "Title\n=====\nbody\n=======\nmore\n",
			want:    nil,
		},
		{
			name:    "unterminated block is refused",
			content: "<<<<<<< HEAD\nO\n=======\nT\n",
			wantErr: "conflict block opened at line 1 is never closed",
		},
		{
			name:    "second separator makes the block ambiguous",
			content: "<<<<<<< HEAD\nTitle\n=======\nbody\n=======\nT\n>>>>>>> topic\n",
			wantErr: "has a second separator at line 5",
		},
		{
			name:    "stray closing marker is refused",
			content: "a\n>>>>>>> topic\n",
			wantErr: "stray closing marker at line 2",
		},
		{
			name:    "nested opening marker is refused",
			content: "<<<<<<< HEAD\nO\n<<<<<<< other\n=======\nT\n>>>>>>> topic\n",
			wantErr: "contains another opening marker at line 3",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseConflictBlocks(tc.content)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("blocks =\n%#v\nwant\n%#v", got, tc.want)
			}
		})
	}
}

// Applying a choice must keep every byte outside the block, including CRLF
// line endings and a missing final newline.
func TestApplyConflictBlockPreservesSurroundingBytes(t *testing.T) {
	content := "a\r\n<<<<<<< HEAD\r\nO\r\n=======\r\nT\r\n>>>>>>> topic"
	blocks, err := parseConflictBlocks(content)
	if err != nil {
		t.Fatal(err)
	}
	repl, err := conflictBlockLines(blocks[0], "both")
	if err != nil {
		t.Fatal(err)
	}
	got := applyConflictBlock(content, blocks[0], repl)
	if want := "a\r\nO\r\nT\r"; got != want {
		t.Fatalf("applied = %q, want %q", got, want)
	}
}

// --- real repository ---

// callChunksGet invokes HandleGitConflictChunks for the repo at dir.
func callChunksGet(t *testing.T, dir, rawQuery string) (*httptest.ResponseRecorder, GitConflictChunks) {
	t.Helper()
	h := NewHandler()
	h.SetWorkDir(dir)
	r := httptest.NewRequest(http.MethodGet, "/api/git/conflict/chunks?"+rawQuery, nil)
	w := httptest.NewRecorder()
	h.HandleGitConflictChunks(w, r)

	var out GitConflictChunks
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatalf("decode chunks: %v\nbody=%s", err, w.Body.String())
		}
	}
	return w, out
}

// callChunkResolve invokes HandleGitResolveConflictChunk for the repo at dir.
func callChunkResolve(t *testing.T, dir string, body GitConflictChunkRequest) (*httptest.ResponseRecorder, GitWorkspace) {
	t.Helper()
	h := NewHandler()
	h.SetWorkDir(dir)
	r, w := call("/api/git/conflict/chunks", body)
	h.HandleGitResolveConflictChunk(w, r)

	var ws GitWorkspace
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&ws); err != nil {
			t.Fatalf("decode workspace: %v\nbody=%s", err, w.Body.String())
		}
	}
	return w, ws
}

func pathQuery(path string) string {
	return "path=" + url.QueryEscape(path)
}

func readRepoFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestConflictChunksListsEveryBlockInFileOrder(t *testing.T) {
	dir := gitThreeChunkMerge(t)

	w, got := callChunksGet(t, dir, pathQuery("f.txt"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if got.Fingerprint == "" {
		t.Fatal("fingerprint is empty for a file with markers")
	}
	if len(got.Chunks) != 3 {
		t.Fatalf("chunks = %d, want 3: %+v", len(got.Chunks), got.Chunks)
	}
	wantOurs := []string{"OURS-2", "OURS-15", "OURS-28"}
	wantTheirs := []string{"THEIRS-2", "THEIRS-15", "THEIRS-28"}
	for i, c := range got.Chunks {
		if c.Ours != wantOurs[i] || c.Theirs != wantTheirs[i] {
			t.Errorf("chunk %d = ours %q theirs %q, want %q / %q", i, c.Ours, c.Theirs, wantOurs[i], wantTheirs[i])
		}
		if c.OursLabel != "HEAD" || c.TheirsLabel != "other" {
			t.Errorf("chunk %d labels = %q / %q, want HEAD / other", i, c.OursLabel, c.TheirsLabel)
		}
	}
}

// A project rooted in a subdirectory must still read the file by its
// repo-relative path. Conflict paths are repo-relative, so resolving against
// the subdirectory would read a file that does not exist there.
func TestConflictChunksReadFromSubdirectoryWorkDir(t *testing.T) {
	dir := gitThreeChunkMergeAt(t, "sub/f.txt")
	h := NewHandler()
	h.SetWorkDir(filepath.Join(dir, "sub"))

	r := httptest.NewRequest(http.MethodGet, "/api/git/conflict/chunks?"+pathQuery("sub/f.txt"), nil)
	w := httptest.NewRecorder()
	h.HandleGitConflictChunks(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var got GitConflictChunks
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Chunks) != 3 {
		t.Fatalf("chunks = %d, want 3: a silent zero here means the wrong file was read", len(got.Chunks))
	}
}

func TestConflictChunkResolveAppliesOneBlockAtATime(t *testing.T) {
	dir := gitThreeChunkMerge(t)

	steps := []struct {
		index  int
		choice string
	}{
		{0, "theirs"}, // block 1 -> THEIRS-2
		{0, "both"},   // block 2 -> OURS-15 then THEIRS-15
		{0, "ours"},   // block 3 -> OURS-28
	}
	for i, step := range steps {
		_, chunks := callChunksGet(t, dir, pathQuery("f.txt"))
		w, ws := callChunkResolve(t, dir, GitConflictChunkRequest{
			Path: "f.txt", Fingerprint: chunks.Fingerprint, Index: step.index, Choice: step.choice,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("step %d: status = %d, body=%s", i, w.Code, w.Body.String())
		}
		conflicted, listed := conflictState(ws, "f.txt")
		last := i == len(steps)-1
		if conflicted == last {
			t.Fatalf("step %d: conflicted = %v, want %v", i, conflicted, !last)
		}
		// The file is staged only once its last block is resolved.
		if listed != last {
			t.Fatalf("step %d: listed = %v, want %v", i, listed, last)
		}
	}

	want := numberedLines(1, 1) + "THEIRS-2\n" + numberedLines(3, 14) +
		"OURS-15\nTHEIRS-15\n" + numberedLines(16, 27) + "OURS-28\n" + numberedLines(29, 30)
	if got := readRepoFile(t, dir, "f.txt"); got != want {
		t.Fatalf("resolved file =\n%s\nwant\n%s", got, want)
	}
}

func TestConflictChunkResolveRefusesStaleFingerprint(t *testing.T) {
	dir := gitThreeChunkMerge(t)
	_, chunks := callChunksGet(t, dir, pathQuery("f.txt"))

	// The user edits the file in another editor after the chunks were read.
	edited := readRepoFile(t, dir, "f.txt") + "extra\n"
	writeFile(t, filepath.Join(dir, "f.txt"), edited)

	w, _ := callChunkResolve(t, dir, GitConflictChunkRequest{
		Path: "f.txt", Fingerprint: chunks.Fingerprint, Index: 0, Choice: "ours",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	if got := readRepoFile(t, dir, "f.txt"); got != edited {
		t.Fatalf("refused request still changed the file:\n%s", got)
	}
}

func TestConflictChunkResolveRejectsBadIndexAndChoice(t *testing.T) {
	dir := gitThreeChunkMerge(t)
	_, chunks := callChunksGet(t, dir, pathQuery("f.txt"))
	before := readRepoFile(t, dir, "f.txt")

	for _, tc := range []struct {
		name string
		req  GitConflictChunkRequest
	}{
		{"index past the last block", GitConflictChunkRequest{Path: "f.txt", Fingerprint: chunks.Fingerprint, Index: 3, Choice: "ours"}},
		{"negative index", GitConflictChunkRequest{Path: "f.txt", Fingerprint: chunks.Fingerprint, Index: -1, Choice: "ours"}},
		{"unknown choice", GitConflictChunkRequest{Path: "f.txt", Fingerprint: chunks.Fingerprint, Index: 0, Choice: "delete"}},
		{"missing fingerprint", GitConflictChunkRequest{Path: "f.txt", Index: 0, Choice: "ours"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := callChunkResolve(t, dir, tc.req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
			}
		})
	}
	if got := readRepoFile(t, dir, "f.txt"); got != before {
		t.Fatalf("rejected requests changed the file:\n%s", got)
	}
}

func TestConflictChunkRefusesPathThatIsNotConflicted(t *testing.T) {
	dir := gitThreeChunkMerge(t)
	writeFile(t, filepath.Join(dir, "clean.txt"), "x\n")

	w, _ := callChunksGet(t, dir, pathQuery("clean.txt"))
	if w.Code != http.StatusConflict {
		t.Fatalf("GET status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	w, _ = callChunkResolve(t, dir, GitConflictChunkRequest{
		Path: "clean.txt", Fingerprint: "x", Index: 0, Choice: "ours",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("POST status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
}

func TestConflictChunksRefusesMalformedFile(t *testing.T) {
	dir := gitThreeChunkMerge(t)
	// Drop the closing marker of the LAST block so it is unclosed at end of file.
	// (Dropping an earlier one would instead trip the nested-opener check.)
	content := readRepoFile(t, dir, "f.txt")
	const closer = ">>>>>>> other\n"
	idx := strings.LastIndex(content, closer)
	broken := content[:idx] + content[idx+len(closer):]
	writeFile(t, filepath.Join(dir, "f.txt"), broken)

	w, _ := callChunksGet(t, dir, pathQuery("f.txt"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "never closed") {
		t.Fatalf("error should say the block is unclosed, body=%s", w.Body.String())
	}
}

// A remote project must be refused before any local path is touched, so the
// same-named local file here is left exactly as it was.
func TestConflictChunksRefuseRemoteProject(t *testing.T) {
	dir := gitThreeChunkMerge(t)
	before := readRepoFile(t, dir, "f.txt")

	h := NewHandler()
	h.SetWorkDir(dir)
	get := httptest.NewRequest(http.MethodGet, "/api/git/conflict/chunks?"+pathQuery("f.txt")+"&host=ssh-box", nil)
	getW := httptest.NewRecorder()
	h.HandleGitConflictChunks(getW, get)
	if getW.Code != http.StatusNotImplemented {
		t.Fatalf("GET status = %d, want 501; body=%s", getW.Code, getW.Body.String())
	}

	r := httptest.NewRequest(http.MethodPost, "/api/git/conflict/chunks?host=ssh-box", strings.NewReader(
		`{"path":"f.txt","fingerprint":"x","index":0,"choice":"ours"}`))
	w := httptest.NewRecorder()
	h.HandleGitResolveConflictChunk(w, r)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("POST status = %d, want 501; body=%s", w.Code, w.Body.String())
	}
	if got := readRepoFile(t, dir, "f.txt"); got != before {
		t.Fatal("remote request changed the local file")
	}
}
