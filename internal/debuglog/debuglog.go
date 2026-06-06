package debuglog

import (
	"strings"
	"sync"
)

type EntryKind string

const (
	KindLLM     EntryKind = "LLM"
	KindTool    EntryKind = "TOOL"
	KindAgent   EntryKind = "AGENT"
	KindError   EntryKind = "ERROR"
	KindSession EntryKind = "SESSION"
	KindGit     EntryKind = "GIT"
	KindLSP     EntryKind = "LSP"
)

type Entry struct {
	Kind    EntryKind
	Message string
}

const cap = 500

var Log = newLog()

type log struct {
	mu      sync.Mutex
	entries []Entry
	notify  chan struct{}
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
	l.mu.Unlock()
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
	l.mu.Unlock()
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
