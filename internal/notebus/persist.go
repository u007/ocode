package notebus

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Sidecar implements PersistSink. It writes one JSON object per line
// to <dir>/<group>.notes.jsonl. The file is append-only — existing
// lines are never rewritten. On Close, the buffered writer is flushed
// and the file is fsynced.
//
// The sidecar lives beside the session blob in the global data dir
// (the orchestrator passes the path in), never inside the session JSON
// itself. The session writer owns that file and concurrent rewrites
// there are a known hazard.
type Sidecar struct {
	dir      string
	groupID  string
	filePath string
	mu       sync.Mutex
	w        *bufio.Writer
	f        *os.File
}

// NewSidecar opens (or creates) the sidecar for the given group. The
// file is opened in append mode and pre-existing content is NOT
// truncated — Load can replay it. The returned sink is ready to
// accept Write calls; the caller should call Close on shutdown.
func NewSidecar(dir, groupID string) (*Sidecar, error) {
	if dir == "" {
		return nil, fmt.Errorf("notebus: sidecar dir is empty")
	}
	if groupID == "" {
		return nil, fmt.Errorf("notebus: sidecar groupID is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("notebus: mkdir sidecar dir: %w", err)
	}
	fp := filepath.Join(dir, groupID+".notes.jsonl")
	f, err := os.OpenFile(fp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("notebus: open sidecar: %w", err)
	}
	return &Sidecar{
		dir:      dir,
		groupID:  groupID,
		filePath: fp,
		f:        f,
		w:        bufio.NewWriter(f),
	}, nil
}

// Path returns the on-disk file path. Useful for the orchestrator to
// surface to the user (audit / debugging).
func (s *Sidecar) Path() string { return s.filePath }

// Write serializes the entry to one JSON object and appends it to the
// sidecar. Errors are returned to the caller; the bus logs and
// continues so a transient disk issue does not stop the in-memory
// log.
func (s *Sidecar) Write(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := json.NewEncoder(s.w).Encode(e); err != nil {
		return err
	}
	return nil
}

// Close flushes the buffered writer, fsyncs the file, and closes it.
// Idempotent.
func (s *Sidecar) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	var firstErr error
	if err := s.w.Flush(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.f.Sync(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.f.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	s.f = nil
	return firstErr
}

// LoadSnapshot reads the sidecar and returns the reconstructed log in
// seq order, the recovered seq (max(seq)+1, 0 if empty), the
// per-agent lastSeen map (recovered from the last seen watermark for
// each agent — a special trailing line if present), and the set of
// resolved seqs. A truncated/garbled trailing line is tolerated: the
// reader keeps every well-formed prefix and logs the bad line via the
// standard log package. The returned log is the canonical "what the
// bus would have on disk at the moment of the crash" view.
//
// The function does not write to the sidecar. It is safe to call
// LoadSnapshot on a non-existent file (returns an empty log, seq=0).
func LoadSnapshot(dir, groupID string) (log2 []Entry, nextSeq int64, wm map[string]int64, resolved map[int64]bool, err error) {
	fp := filepath.Join(dir, groupID+".notes.jsonl")
	wm = make(map[string]int64)
	resolved = make(map[int64]bool)
	f, err := os.Open(fp)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, wm, resolved, nil
		}
		return nil, 0, wm, resolved, fmt.Errorf("notebus: open sidecar: %w", err)
	}
	defer f.Close()

	// Per-agent state: the running max seq we have seen for that
	// agent's authored notes. The "lastSeen" is what we would
	// tell the agent on reload — i.e. the seq up to which the
	// agent has already been informed (we approximate by
	// remembering the highest seq known to exist when the agent
	// last had a delta computed). Without per-agent bookkeeping in
	// the sidecar (the design spec does not require it), we set
	// lastSeen[agent] to the highest seq that was already
	// reflected in the in-memory log, which is nextSeq-1 after
	// load — i.e. the agent has effectively seen everything that
	// was ever on disk. This is the safest fallback: on reload
	// mid-group, the agent does not re-receive old notes.
	//
	// In practice the bus only needs to recover entries (so
	// reconcile can audit the group) and the next seq to assign
	// (so appends do not duplicate seqs). The watermark reset
	// is conservative and correct: never inject an old note as
	// new on reload.
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var e Entry
		if jerr := json.Unmarshal(line, &e); jerr != nil {
			log.Printf("notebus: sidecar %s: line %d malformed (%v); truncating at last good line", fp, lineNo, jerr)
			break
		}
		if e.Seq <= 0 {
			log.Printf("notebus: sidecar %s: line %d missing seq; truncating", fp, lineNo)
			break
		}
		log2 = append(log2, e)
		if e.Seq > nextSeq {
			nextSeq = e.Seq
		}
		if e.Kind == KindResolve && e.Ref > 0 {
			resolved[e.Ref] = true
		}
		// Track per-agent max authored seq as a lower bound for
		// lastSeen. After load we will set lastSeen[agent] to
		// the head (nextSeq), which dominates.
		if e.By != "" {
			if s := e.Seq; s > wm[e.By] {
				wm[e.By] = s
			}
		}
	}
	if serr := scanner.Err(); serr != nil {
		// Already truncated at the last good line; surface the
		// scanner error for visibility but do not fail the load.
		log.Printf("notebus: sidecar %s: scan err: %v", fp, serr)
	}

	// Sort defensively — the file is append-only so this should
	// already be the case, but a partial write (fsync gap) could
	// in principle land a later seq before an earlier one. We
	// only get here from a normal, well-ordered append stream.
	sort.SliceStable(log2, func(i, j int) bool { return log2[i].Seq < log2[j].Seq })

	// On reload, set every agent's watermark to nextSeq so the
	// log is treated as already-seen. This matches the
	// invariant: "lastSeen[agent] == highest seq already reflected
	// in that agent's restored transcript." The transcript on
	// disk is complete up to the sidecar, so the agent has seen
	// everything that exists.
	//
	// Return nextSeq as the highest seq seen (e.g. 7 after 7
	// entries). Hydrate installs this as the owner's seq so the
	// next Append (b.seq++) assigns nextSeq+1 — i.e. 8.
	for by := range wm {
		wm[by] = nextSeq
	}
	return log2, nextSeq, wm, resolved, nil
}

// Hydrate restores a Bus's in-memory state from a sidecar snapshot.
// This is the resume path used by Part 02's group-construction code
// when a previous run of the same group id left a sidecar behind.
// Hydrate does NOT start the bus; the caller is still expected to
// call Start(ctx) and the in-memory state is installed before the
// goroutine sees its first request.
//
// Hydrate returns the set of agent ids seen in the loaded log so
// the caller can pre-register any per-agent state it has tracked
// externally (id ↔ child-session-id). Hydrate must be called before
// Start; the bus does not synchronize against a running owner.
func (b *Bus) Hydrate(dir, groupID string) ([]string, error) {
	log2, maxSeq, wm, resolved, err := LoadSnapshot(dir, groupID)
	if err != nil {
		return nil, err
	}
	// Install the recovered state directly. This is safe because
	// no goroutine is reading yet. b.seq is set to the highest
	// seq seen; the owner's pre-increment on Append then assigns
	// maxSeq+1, which is the canonical "next" value.
	b.log = append(b.log, log2...)
	b.seq = maxSeq
	for k, v := range wm {
		b.wm[k] = v
	}
	for k := range resolved {
		b.reslv[k] = true
	}
	// Collect unique authors for the caller's bookkeeping.
	seen := make(map[string]bool)
	for _, e := range b.log {
		if e.By != "" {
			seen[e.By] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// EncodeEntry returns the JSON wire form of an entry. Exposed for
// tests and for callers that want to write their own log (e.g.
// pre-seeded briefs) in the same shape as the sidecar.
func EncodeEntry(e Entry) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(e); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// String returns a short summary of an entry for logs: "seq=N by=a
// kind=note at=…". Avoid dumping bodies.
func (e Entry) String() string {
	switch e.Kind {
	case KindNote:
		return fmt.Sprintf("seq=%d by=%s note at=%q", e.Seq, e.By, e.At)
	case KindTouch:
		return fmt.Sprintf("seq=%d by=%s touch %s/%s", e.Seq, e.By, e.File, e.Act)
	case KindResolve:
		return fmt.Sprintf("seq=%d by=%s resolve ref=%d", e.Seq, e.By, e.Ref)
	default:
		return fmt.Sprintf("seq=%d by=%s kind=%s", e.Seq, e.By, e.Kind)
	}
}

// debugFields returns a comma-separated list of non-empty fields for
// the debug log line written on malformed sidecar lines.
func (e Entry) debugFields() string {
	parts := []string{}
	if e.At != "" {
		parts = append(parts, "at="+e.At)
	}
	if e.File != "" {
		parts = append(parts, "file="+e.File)
	}
	if e.Ref != 0 {
		parts = append(parts, fmt.Sprintf("ref=%d", e.Ref))
	}
	return strings.Join(parts, ",")
}
