package notebus

import "errors"

// ErrUnknownKind is returned by Append when the entry's Kind is
// KindUnknown (the zero value) or otherwise not a recognized kind.
// This is a programming error — the bus never produces it for a
// well-formed caller.
var ErrUnknownKind = errors.New("notebus: unknown entry kind")

// ErrBusClosed is returned by Append/Snapshot/Delta when the owner
// goroutine has exited. Callers should treat the bus as gone and
// surface a graceful shutdown to the user (the log is still on disk
// via the persist sink if one was configured).
var ErrBusClosed = errors.New("notebus: bus closed")

// ErrBusNotStarted is returned by Append when the bus has never been
// Start()'d. Without an owner goroutine the request channel has no
// reader, so the call would otherwise block forever — failing fast
// turns a silent deadlock into an immediate, debuggable error.
var ErrBusNotStarted = errors.New("notebus: bus not started")
