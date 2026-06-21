package discovery

import "github.com/u007/ocode/internal/debuglog"

// emitDiscoveryDebug forwards discovery-package log lines to the global debug
// log so they appear on the Log tab. kind is a debuglog EntryKind string
// ("DISCOVERY" or "WARN"). userFacing is true for events the user is likely
// waiting on (artifact download start/progress/done); the TUI surfaces those
// in the chat transcript as a transient notice that is NOT sent to the LLM.
var emitDiscoveryDebug = func(kind, msg string) {
	emitDiscoveryDebugAt(kind, msg, false)
}

// emitUserDiscoveryDebug is the user-facing variant: also tags the log
// entry so the TUI can promote it to the chat transcript. Use sparingly —
// only for events the user is actually waiting on (downloads, spawns).
func emitUserDiscoveryDebug(kind, msg string) {
	emitDiscoveryDebugAt(kind, msg, true)
}

func emitDiscoveryDebugAt(kind, msg string, userFacing bool) {
	debuglog.Log.Append(debuglog.Entry{
		Kind:       debuglog.EntryKind(kind),
		Message:    msg,
		UserFacing: userFacing,
	})
}
