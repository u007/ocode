package desktop

import (
	"strings"
	"sync"
)

// Raw message protocol sent by the web UI through the minimal Wails bridge
// (window._wails.invoke), which the desktop shell receives in
// application.Options.RawMessageHandler. Messages that start with "wails:" go
// to Wails' own message dispatcher instead and never reach that handler.
const (
	quitGuardBlockedMessage = "ocode:quit-guard:blocked"
	quitGuardClearMessage   = "ocode:quit-guard:clear"
	quitGuardReasonPrefix   = "ocode:quit-guard:blocked:"
)

// QuitGuard records whether the web UI holds unsaved work it could not persist
// (currently: an editor draft that failed to write to localStorage). While
// blocked the desktop shell refuses to quit, so an edit is never silently
// lost:
//
//   - application.Options.ShouldQuit returns false, cancelling Cmd+Q, the
//     tray/menu Quit, and any programmatic app.Quit();
//   - a WindowClosing RegisterHook cancels the close before Wails' internal
//     listener can destroy the window (see cmd/ocode-desktop).
//
// The web UI is the only writer: it reports a failure with
// "ocode:quit-guard:blocked[:reason]" and a recovery with
// "ocode:quit-guard:clear". The native "Quit anyway" action also clears it.
type QuitGuard struct {
	mu      sync.Mutex
	blocked bool
	reason  string
}

// NewQuitGuard returns an unblocked guard.
func NewQuitGuard() *QuitGuard { return &QuitGuard{} }

// Block records that quitting must be refused until Clear is called. reason is
// surfaced to the user; an empty reason is allowed.
func (g *QuitGuard) Block(reason string) {
	g.mu.Lock()
	g.blocked = true
	g.reason = reason
	g.mu.Unlock()
}

// Clear removes the block, allowing quit.
func (g *QuitGuard) Clear() {
	g.mu.Lock()
	g.blocked = false
	g.reason = ""
	g.mu.Unlock()
}

// Blocked reports whether quit must be refused and, if so, the reason.
func (g *QuitGuard) Blocked() (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.blocked, g.reason
}

// HandleRawMessage interprets a non-"wails:" webview message. It reports
// whether the message was a quit-guard message so the caller can ignore the
// rest.
func (g *QuitGuard) HandleRawMessage(message string) bool {
	switch {
	case message == quitGuardBlockedMessage:
		g.Block("")
		return true
	case strings.HasPrefix(message, quitGuardReasonPrefix):
		g.Block(strings.TrimPrefix(message, quitGuardReasonPrefix))
		return true
	case message == quitGuardClearMessage:
		g.Clear()
		return true
	}
	return false
}
