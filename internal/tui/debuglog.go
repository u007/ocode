package tui

import (
	"strings"
	"sync"
)

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

// debugLogWriter adapts the standard library log package to the in-TUI debug
// panel. The TUI runs in bubbletea's alt-screen, so anything written to the
// default log output (os.Stderr) paints directly over the rendered frame and
// corrupts it (the "hairwire" overlap at the bottom of the chat). Installing
// this as log.SetOutput keeps every log.Printf — from this package and any
// package the agent loop drives — inside the debug panel instead of bleeding
// onto the screen.
type debugLogWriter struct{}

func (debugLogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	if msg != "" {
		DebugLog.Append(DebugEntry{Kind: DebugKindError, Message: msg})
	}
	return len(p), nil
}
