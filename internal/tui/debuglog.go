package tui

import "sync"

type DebugEntryKind string

const (
	DebugKindLLM     DebugEntryKind = "LLM"
	DebugKindTool    DebugEntryKind = "TOOL"
	DebugKindAgent   DebugEntryKind = "AGENT"
	DebugKindError   DebugEntryKind = "ERROR"
	DebugKindSession DebugEntryKind = "SESSION"
)

type DebugEntry struct {
	Kind    DebugEntryKind
	Message string
}

const debugLogCap = 500

var DebugLog = newDebugLog()

type debugLog struct {
	mu      sync.Mutex
	entries []DebugEntry
	notify  chan struct{}
}

func newDebugLog() *debugLog {
	return &debugLog{
		entries: make([]DebugEntry, 0, debugLogCap),
		notify:  make(chan struct{}, 1),
	}
}

func (d *debugLog) Append(e DebugEntry) {
	d.mu.Lock()
	if len(d.entries) >= debugLogCap {
		copy(d.entries, d.entries[1:])
		d.entries = d.entries[:debugLogCap-1]
	}
	d.entries = append(d.entries, e)
	d.mu.Unlock()
	select {
	case d.notify <- struct{}{}:
	default:
	}
}

func (d *debugLog) Snapshot() []DebugEntry {
	d.mu.Lock()
	out := make([]DebugEntry, len(d.entries))
	copy(out, d.entries)
	d.mu.Unlock()
	return out
}

func (d *debugLog) Clear() {
	d.mu.Lock()
	d.entries = d.entries[:0]
	d.mu.Unlock()
}

func (d *debugLog) Notify() chan struct{} {
	return d.notify
}
