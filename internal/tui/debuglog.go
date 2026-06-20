package tui

import (
	"github.com/u007/ocode/internal/debuglog"
)

// DebugEntryKind is an alias for backward compatibility.
type DebugEntryKind = debuglog.EntryKind

const (
	DebugKindLLM       = debuglog.KindLLM
	DebugKindTool      = debuglog.KindTool
	DebugKindAgent     = debuglog.KindAgent
	DebugKindError     = debuglog.KindError
	DebugKindSession   = debuglog.KindSession
	DebugKindGit       = debuglog.KindGit
	DebugKindLSP       = debuglog.KindLSP
	DebugKindWarn      = debuglog.KindWarn
	DebugKindDiscovery = debuglog.KindDiscovery
)

// DebugEntry is an alias for backward compatibility.
type DebugEntry = debuglog.Entry

// DebugLog is the global debug log instance.
var DebugLog = debuglog.Log

// debugLogWriter adapts the standard library log package to the in-TUI debug
// panel.
type debugLogWriter = debuglog.LogWriter
