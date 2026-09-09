// Package crashguard is the shared spawn path for goroutines that ocode owns
// outside bubbletea. Bubbletea only recovers panics raised in Update/View and
// in the goroutines it starts itself; a panic in a raw `go func()` kills the
// process before bubbletea can restore the terminal, leaving mouse tracking
// and the alt-screen enabled in the user's shell (every mouse move then
// prints "[<35;20;10M"-style garbage). Go routes every such goroutine through
// a recover that runs the registered OnPanic hook (the TUI installs a
// terminal reset) before re-panicking so the crash is still fatal and the
// original stack is still printed.
package crashguard

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"sync/atomic"
)

var onPanic atomic.Pointer[func()]

// SetOnPanic registers the hook run before a guarded goroutine re-panics.
// Only one hook is kept; the latest registration wins.
func SetOnPanic(fn func()) {
	onPanic.Store(&fn)
}

// Go starts fn on a new goroutine guarded by Recover.
func Go(fn func()) {
	go func() {
		defer Recover()
		fn()
	}()
}

// Recover must be deferred at the top of a goroutine. On panic it runs the
// OnPanic hook, prints the original stack, and re-panics.
func Recover() {
	r := recover()
	if r == nil {
		return
	}
	if fn := onPanic.Load(); fn != nil {
		(*fn)()
	}
	// The terminal may still be in raw mode: use "\r\n" so the trace stays
	// readable, mirroring bubbletea's own panic output.
	rec := strings.ReplaceAll(fmt.Sprintf("%v", r), "\n", "\r\n")
	stack := strings.ReplaceAll(string(debug.Stack()), "\n", "\r\n")
	fmt.Fprintf(os.Stderr, "ocode: goroutine panic: %s\r\n\r\n%s\r\n", rec, stack)
	panic(r)
}
