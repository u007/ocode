package debuglog

import (
	"strings"
	"sync"
)

type EntryKind string

const (
	KindLLM       EntryKind = "LLM"
	KindTool      EntryKind = "TOOL"
	KindAgent     EntryKind = "AGENT"
	KindError     EntryKind = "ERROR"
	KindSession   EntryKind = "SESSION"
	KindGit       EntryKind = "GIT"
	KindLSP       EntryKind = "LSP"
	KindWarn      EntryKind = "WARN"
	KindDiscovery EntryKind = "DISCOVERY"
)

type Entry struct {
	Kind    EntryKind
	Message string
	// UserFacing tags entries that should also be surfaced in the chat
	// transcript as a transient notice (visible to the user, not sent to
	// the LLM). Used for download progress events (e.g. "downloading
	// llama-server …") so a long artifact download doesn't sit silent on
	// the Log tab while the user is on the chat tab waiting for a reply.
	UserFacing bool
}

const cap = 500

var Log = newLog()

type log struct {
	mu      sync.Mutex
	entries []Entry
	notify  chan struct{}
	// seq counts every entry ever appended; entry i in the ring has sequence
	// seq-len(entries)+i. Clear resets it to 0. Lets SnapshotSince consumers
	// diff correctly across ring-buffer drops (len stops growing at cap, so
	// a count-based diff would stall) and across clears (no re-emits).
	seq uint64
	// mirrorKind/mirrorPath route entries of one kind to an on-disk file (see
	// MirrorKindToFile); mirrorFailOnce keeps a broken mirror from spamming the
	// in-memory log with one error per append.
	mirrorKind     EntryKind
	mirrorPath     string
	mirrorFailOnce sync.Once
}

func newLog() *log {
	return &log{
		entries: make([]Entry, 0, cap),
		notify:  make(chan struct{}, 1),
	}
}

func (l *log) Append(e Entry) {
	l.mu.Lock()
	if len(l.entries) >= cap {
		copy(l.entries, l.entries[1:])
		l.entries = l.entries[:cap-1]
	}
	l.entries = append(l.entries, e)
	l.seq++
	mirrorPath := ""
	if l.mirrorPath != "" && e.Kind == l.mirrorKind && e.Kind != KindError {
		mirrorPath = l.mirrorPath
	}
	l.mu.Unlock()
	if mirrorPath != "" {
		l.mirror(mirrorPath, e)
	}
	select {
	case l.notify <- struct{}{}:
	default:
	}
}

func (l *log) Snapshot() []Entry {
	l.mu.Lock()
	out := make([]Entry, len(l.entries))
	copy(out, l.entries)
	l.mu.Unlock()
	return out
}

func (l *log) Clear() {
	l.mu.Lock()
	l.entries = l.entries[:0]
	l.seq = 0
	l.mu.Unlock()
}

// Cursor returns the current sequence cursor: pass it to SnapshotSince to
// receive only entries appended after this call.
func (l *log) Cursor() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seq
}

// SnapshotSince returns the entries appended at or after cursor since,
// plus the cursor to pass on the next call. A since ahead of the current
// sequence means the log was cleared (restart from 0); a since behind the
// ring's oldest entry means entries were dropped before being read (those
// are unavoidably skipped). Treat cursors as opaque: always pass back the
// previous return value.
func (l *log) SnapshotSince(since uint64) ([]Entry, uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	first := l.seq - uint64(len(l.entries))
	if since > l.seq { // cleared since the last call
		since = 0
	}
	if since < first { // ring dropped unread entries
		since = first
	}
	out := make([]Entry, l.seq-since)
	copy(out, l.entries[since-first:])
	return out, l.seq
}

func (l *log) Notify() chan struct{} {
	return l.notify
}

// LogWriter adapts the standard library log package to the debug log.
type LogWriter struct{}

func (LogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	if msg != "" {
		Log.Append(Entry{Kind: KindError, Message: msg})
	}
	return len(p), nil
}
