package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Per-block conflict resolution: GET /api/git/conflict/chunks lists the
// <<<<<<< ... >>>>>>> regions of one conflicted working-tree file, and
// POST /api/git/conflict/chunks keeps ours, theirs or both for one of them.
//
// Local projects only. A remote project answers 501 before any path is
// touched, so the request can never reach the local machine's copy of a
// same-named file.

// GitConflictChunk is one conflict region as the UI shows it. Ours and Theirs
// are the lines between the markers, joined with "\n". The diff3 base section
// is dropped during parsing so its lines cannot be mistaken for ours.
type GitConflictChunk struct {
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	OursLabel   string `json:"ours_label"`
	TheirsLabel string `json:"theirs_label"`
	Ours        string `json:"ours"`
	Theirs      string `json:"theirs"`
}

// GitConflictChunks is the body of GET /api/git/conflict/chunks. Chunks are not
// paginated on purpose: they are in file order, bounded by conflictMarkerScanLimit,
// and a chunk index is only meaningful against the whole list it was read from.
//
// Fingerprint is the SHA-256 of the file bytes the chunks were parsed from. A
// resolve request must echo it back, so chunk indexes can never be applied to a
// file that changed underneath the client. A file that is absent (a
// modify/delete conflict) reports an empty fingerprint and no chunks.
type GitConflictChunks struct {
	Path        string             `json:"path"`
	Fingerprint string             `json:"fingerprint"`
	Chunks      []GitConflictChunk `json:"chunks"`
}

// GitConflictChunkRequest is the body of POST /api/git/conflict/chunks.
type GitConflictChunkRequest struct {
	Path        string `json:"path"`
	Fingerprint string `json:"fingerprint"`
	// Index is the chunk's position in the list the client last read.
	Index int `json:"index"`
	// Choice is one of "ours", "theirs" or "both" ("both" is ours then theirs).
	Choice string `json:"choice"`
}

// conflictBlock is one parsed region. Line indexes are 0-based and inclusive,
// so the region is lines[start:end+1] of the file split on "\n".
type conflictBlock struct {
	start, end  int
	oursLabel   string
	theirsLabel string
	ours        []string
	theirs      []string
}

// Marker words. Git writes each marker as the word, then a space and a label
// (or nothing). A bare "=======" separator is matched exactly.
const (
	conflictOpenWord  = "<<<<<<<"
	conflictBaseWord  = "|||||||"
	conflictSepLine   = "======="
	conflictCloseWord = ">>>>>>>"
)

// conflictSection tracks which part of a block the parser is inside.
type conflictSection int

const (
	sectionOutside conflictSection = iota
	sectionOurs
	sectionBase
	sectionTheirs
)

// isConflictMarker reports whether line is the given marker word, alone or
// followed by a space and a label. A word glued to other text is content.
func isConflictMarker(line, word string) bool {
	return line == word || strings.HasPrefix(line, word+" ")
}

// conflictMarkerLabel returns the text after a marker word, e.g. "HEAD" for
// "<<<<<<< HEAD". A bare marker has an empty label.
func conflictMarkerLabel(line, word string) string {
	return strings.TrimSpace(strings.TrimPrefix(line, word))
}

// parseConflictBlocks finds every conflict region in content. It is strict:
// anything it cannot place with certainty is an error, never a guess, because
// a wrong guess would make "keep ours" silently drop the user's lines.
//
// Rules:
//   - "<<<<<<< " opens a block; "||||||| " (diff3 base) and "=======" move it
//     along; ">>>>>>> " closes it. "\r" is trimmed before matching so CRLF
//     files parse. Content lines keep their "\r".
//   - A bare "=======" outside a block is content, not a marker, so a
//     Markdown setext underline does not split a file.
//   - A second separator inside a block, a nested opener, a stray closer, or an
//     unclosed block is an error. The second separator case is the one that
//     hides a real problem: a setext underline inside "ours" makes the first
//     separator look like the real one.
func parseConflictBlocks(content string) ([]conflictBlock, error) {
	lines := strings.Split(content, "\n")
	var blocks []conflictBlock
	var cur conflictBlock
	section := sectionOutside

	for i, raw := range lines {
		line := strings.TrimSuffix(raw, "\r")
		lineNo := i + 1
		switch {
		case isConflictMarker(line, conflictOpenWord):
			if section != sectionOutside {
				return nil, fmt.Errorf("conflict block opened at line %d contains another opening marker at line %d", cur.start+1, lineNo)
			}
			cur = conflictBlock{start: i, oursLabel: conflictMarkerLabel(line, conflictOpenWord)}
			section = sectionOurs

		case isConflictMarker(line, conflictBaseWord):
			if section != sectionOurs {
				return nil, fmt.Errorf("stray base marker at line %d", lineNo)
			}
			section = sectionBase

		case line == conflictSepLine && section != sectionOutside:
			if section == sectionTheirs {
				return nil, fmt.Errorf("conflict block opened at line %d has a second separator at line %d; the block is ambiguous, so resolve this file by hand or with ours/theirs", cur.start+1, lineNo)
			}
			section = sectionTheirs

		case isConflictMarker(line, conflictCloseWord):
			if section != sectionTheirs {
				return nil, fmt.Errorf("stray closing marker at line %d", lineNo)
			}
			cur.end = i
			cur.theirsLabel = conflictMarkerLabel(line, conflictCloseWord)
			blocks = append(blocks, cur)
			section = sectionOutside

		case section == sectionOurs:
			cur.ours = append(cur.ours, raw)

		case section == sectionTheirs:
			cur.theirs = append(cur.theirs, raw)
		}
	}
	if section != sectionOutside {
		return nil, fmt.Errorf("conflict block opened at line %d is never closed", cur.start+1)
	}
	return blocks, nil
}

// conflictBlockLines returns the replacement lines for one choice. An unknown
// choice is an error, so a caller can never delete a block by accident.
func conflictBlockLines(b conflictBlock, choice string) ([]string, error) {
	switch choice {
	case "ours":
		return b.ours, nil
	case "theirs":
		return b.theirs, nil
	case "both":
		out := make([]string, 0, len(b.ours)+len(b.theirs))
		out = append(out, b.ours...)
		return append(out, b.theirs...), nil
	}
	return nil, fmt.Errorf("choice must be \"ours\", \"theirs\" or \"both\", got %q", choice)
}

// applyConflictBlock replaces one block's markers with the chosen lines. Every
// byte outside the block is kept, including a missing final newline.
func applyConflictBlock(content string, b conflictBlock, repl []string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines)-(b.end-b.start+1)+len(repl))
	out = append(out, lines[:b.start]...)
	out = append(out, repl...)
	out = append(out, lines[b.end+1:]...)
	return strings.Join(out, "\n")
}

// conflictFingerprint is the SHA-256 of the file bytes, hex encoded.
func conflictFingerprint(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// chunksFromBlocks converts parsed regions into the wire shape.
func chunksFromBlocks(blocks []conflictBlock) []GitConflictChunk {
	chunks := make([]GitConflictChunk, 0, len(blocks))
	for _, b := range blocks {
		chunks = append(chunks, GitConflictChunk{
			StartLine:   b.start + 1,
			EndLine:     b.end + 1,
			OursLabel:   b.oursLabel,
			TheirsLabel: b.theirsLabel,
			Ours:        strings.Join(b.ours, "\n"),
			Theirs:      strings.Join(b.theirs, "\n"),
		})
	}
	return chunks
}

// refuseRemoteConflictChunks answers 501 for a remote project. It runs before
// any path or project lookup, so the request cannot touch the local copy.
func refuseRemoteConflictChunks(w http.ResponseWriter, r *http.Request) bool {
	if hostParam(r) == "" {
		return false
	}
	writeError(w, http.StatusNotImplemented,
		"per-block conflict resolution is not available for remote projects yet; resolve the whole file with ours, theirs or mark resolved")
	return true
}

// readConflictFile reads a conflicted working-tree file. It returns exists=false
// for an absent file, which is a legitimate modify/delete conflict and not an
// error. It writes the response and returns ok=false when the file cannot be
// used: not a regular file, too large to scan, or unreadable.
func readConflictFile(w http.ResponseWriter, dir, spec string) (content []byte, exists bool, ok bool) {
	path := filepath.Join(dir, filepath.FromSlash(spec))
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, true
		}
		slog.Error("git conflict chunks: stat failed", "path", path, "err", err)
		writeError(w, http.StatusInternalServerError, "cannot read "+spec+": "+err.Error())
		return nil, false, false
	}
	if !info.Mode().IsRegular() {
		writeError(w, http.StatusBadRequest, spec+" is not a regular file; resolve it with ours or theirs instead")
		return nil, false, false
	}
	if info.Size() > conflictMarkerScanLimit {
		writeError(w, http.StatusBadRequest,
			spec+" is too large to resolve block by block; resolve it with ours or theirs instead")
		return nil, false, false
	}
	content, err = os.ReadFile(path)
	if err != nil {
		slog.Error("git conflict chunks: read failed", "path", path, "err", err)
		writeError(w, http.StatusInternalServerError, "cannot read "+spec+": "+err.Error())
		return nil, false, false
	}
	return content, true, true
}

// gitPathUnmerged reports whether spec still has unmerged index entries. It is
// a single path-limited `git ls-files -u`, so it does not scan the whole work
// tree the way a full status does on every block click.
func gitPathUnmerged(w http.ResponseWriter, dir, spec string) bool {
	out, err := gitRunInDirLiteral(dir, "ls-files", "-u", "--", spec)
	if err != nil {
		writeLocalGitError(w, dir, err)
		return false
	}
	if out == "" {
		writeError(w, http.StatusConflict, "path is not a conflicted file: "+spec)
		return false
	}
	return true
}

// writeConflictFileAtomic replaces the file at path with content through a temp
// file in the same directory and a rename, so a crash never leaves a truncated
// file. wantFingerprint is re-checked against the bytes on disk right before
// the rename; a mismatch means someone saved in between and nothing is written.
// The original mode is kept.
func writeConflictFileAtomic(path string, content []byte, wantFingerprint string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ocode-conflict-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		if err := os.Remove(tmpName); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("git conflict chunk: remove temp file failed", "path", tmpName, "err", err)
		}
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmpName, info.Mode().Perm()); err != nil {
		cleanup()
		return err
	}
	current, err := os.ReadFile(path)
	if err != nil {
		cleanup()
		return err
	}
	if conflictFingerprint(current) != wantFingerprint {
		cleanup()
		return errConflictFileChanged
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

var errConflictFileChanged = errors.New("file changed while it was being resolved")

// HandleGitConflictChunks lists the conflict regions of one conflicted file.
func (h *Handler) HandleGitConflictChunks(w http.ResponseWriter, r *http.Request) {
	if refuseRemoteConflictChunks(w, r) {
		return
	}
	dir, ok := h.gitProjectDir(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown project")
		return
	}
	dir, ok = gitWorkTreeRoot(w, dir)
	if !ok {
		return
	}
	spec, ok := resolveConflictPath(w, dir, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	if !gitPathUnmerged(w, dir, spec) {
		return
	}
	content, exists, ok := readConflictFile(w, dir, spec)
	if !ok {
		return
	}
	resp := GitConflictChunks{Path: spec, Chunks: []GitConflictChunk{}}
	if exists {
		blocks, err := parseConflictBlocks(string(content))
		if err != nil {
			writeError(w, http.StatusBadRequest, spec+": "+err.Error())
			return
		}
		resp.Fingerprint = conflictFingerprint(content)
		resp.Chunks = chunksFromBlocks(blocks)
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleGitResolveConflictChunk keeps one side of one conflict region, then
// returns the refreshed workspace. Once no markers remain the file is staged
// through gitMarkResolved, which re-scans it.
func (h *Handler) HandleGitResolveConflictChunk(w http.ResponseWriter, r *http.Request) {
	if refuseRemoteConflictChunks(w, r) {
		return
	}
	var req GitConflictChunkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if req.Fingerprint == "" {
		writeError(w, http.StatusBadRequest, "fingerprint is required; reload the conflict blocks")
		return
	}
	if req.Index < 0 {
		writeError(w, http.StatusBadRequest, "index must not be negative")
		return
	}

	dir, ok := h.gitDirForMutation(w, r)
	if !ok {
		return
	}
	spec, ok := resolveConflictPath(w, dir, req.Path)
	if !ok {
		return
	}
	if !gitPathUnmerged(w, dir, spec) {
		return
	}
	content, exists, ok := readConflictFile(w, dir, spec)
	if !ok {
		return
	}
	if !exists {
		writeError(w, http.StatusConflict, spec+" has no working-tree file to resolve block by block")
		return
	}
	if conflictFingerprint(content) != req.Fingerprint {
		writeError(w, http.StatusConflict, spec+" changed since its conflict blocks were read; reload them")
		return
	}
	blocks, err := parseConflictBlocks(string(content))
	if err != nil {
		writeError(w, http.StatusBadRequest, spec+": "+err.Error())
		return
	}
	if req.Index >= len(blocks) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%s has no conflict block at index %d", spec, req.Index))
		return
	}
	repl, err := conflictBlockLines(blocks[req.Index], req.Choice)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out := applyConflictBlock(string(content), blocks[req.Index], repl)

	path := filepath.Join(dir, filepath.FromSlash(spec))
	if err := writeConflictFileAtomic(path, []byte(out), req.Fingerprint); err != nil {
		if errors.Is(err, errConflictFileChanged) {
			writeError(w, http.StatusConflict, spec+" changed since its conflict blocks were read; reload them")
			return
		}
		slog.Error("git conflict chunk: write failed", "project", dir, "path", spec, "index", req.Index, "err", err)
		writeError(w, http.StatusInternalServerError, "cannot write "+spec+": "+err.Error())
		return
	}

	// "Resolved" uses the parser's definition, not hasConflictMarkers: a bare
	// marker the parser still counts as a block must keep the file unstaged.
	remaining, err := parseConflictBlocks(out)
	if err != nil {
		slog.Error("git conflict chunk: result no longer parses", "project", dir, "path", spec, "err", err)
		h.rollbackConflictFile(w, path, content, out, spec, "result no longer parses: "+err.Error())
		return
	}
	if len(remaining) == 0 {
		if _, err := gitRunInDirLiteral(dir, "add", "--", spec); err != nil {
			slog.Error("git conflict chunk: stage failed", "project", dir, "path", spec, "err", err)
			h.rollbackConflictFile(w, path, content, out, spec, "git add failed: "+err.Error())
			return
		}
	}
	writeLocalGitWorkspace(w, dir)
}

// rollbackConflictFile restores the pre-resolve bytes after a step that follows
// the write failed, so the file and the index never disagree and a retry with
// the client's fingerprint still applies. It writes the error response.
func (h *Handler) rollbackConflictFile(w http.ResponseWriter, path string, original []byte, written, spec, cause string) {
	if err := writeConflictFileAtomic(path, original, conflictFingerprint([]byte(written))); err != nil {
		slog.Error("git conflict chunk: rollback failed", "path", path, "err", err)
		writeError(w, http.StatusInternalServerError,
			cause+"; and restoring "+spec+" failed ("+err.Error()+"), so the file now holds the resolved text")
		return
	}
	writeError(w, http.StatusInternalServerError, cause+"; "+spec+" was left unchanged")
}
