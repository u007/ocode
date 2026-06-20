package discovery

import "github.com/u007/ocode/internal/debuglog"

// emitDiscoveryDebug forwards discovery-package log lines to the global debug log
// so they appear on the Log tab. kind is a debuglog EntryKind string
// ("DISCOVERY" or "WARN").
var emitDiscoveryDebug = func(kind, msg string) {
	debuglog.Log.Append(debuglog.Entry{Kind: debuglog.EntryKind(kind), Message: msg})
}
