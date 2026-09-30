package reminders

import (
	"errors"
	"fmt"
)

// ErrTransition wraps every rejection from Transition, so a caller can map an
// illegal status change onto an HTTP 409 with errors.Is instead of matching on
// message text — which would silently rot the first time a message is reworded.
var ErrTransition = errors.New("illegal status transition")

// allowedTransitions is the whole status state machine, as a table. Every
// legality question — the HTTP PATCH handler, the auto-complete path after a
// successful agent turn, and the tests — resolves through CanTransition, so
// the three can never disagree about what is a legal move.
//
// The shape of the machine:
//
//	pending ─────► in_progress ─────► completed
//	   ▲  │             │  │              │
//	   │  └─────────────┼──┘              │
//	   │                ▼                   │
//	   └──────────── cancelled ◄────────────┘
//
// Rules, in words:
//   - Every non-terminal state can go back to pending. That is the "unmark" the
//     user asked for, and it is deliberately always available — you must be
//     able to reopen a mistake from any state.
//   - pending may also be completed or cancelled directly (the common cases:
//     done without ever being started; abandoned outright).
//   - in_progress may be cancelled.
//   - completed and cancelled are terminal WITH RESPECT TO EACH OTHER: you
//     cannot go completed -> cancelled or cancelled -> completed in one step.
//     Route through pending instead. This keeps "reopen" a single, auditable
//     move rather than a set of overlapping ones.
//   - A no-op (from == to) is NOT in this table. CanTransition reports it
//     illegal so the validator is strict; Service.SetStatus handles the
//     idempotent case BEFORE calling it, so a retried PATCH is still a 200.
var allowedTransitions = map[Status][]Status{
	StatusPending: {
		StatusInProgress,
		StatusCompleted,
		StatusCancelled,
	},
	StatusInProgress: {
		StatusCompleted,
		StatusCancelled,
		StatusPending,
	},
	StatusCompleted: {
		StatusPending,
	},
	StatusCancelled: {
		StatusPending,
	},
}

// CanTransition reports whether from -> to is a legal status change.
//
// It returns false — rather than erroring — for a same-state move, and false
// for any status outside the accepted set, so a caller can never smuggle an
// unknown value through this function.
func CanTransition(from, to Status) bool {
	if !ValidStatus(from) || !ValidStatus(to) || from == to {
		return false
	}
	for _, next := range allowedTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// Transition validates from -> to and returns an error wrapping ErrTransition
// when the move is illegal. It is the single choke point for "is this a legal
// status change", so the error text the user sees has exactly one wording.
func Transition(from, to Status) error {
	if !ValidStatus(from) {
		return fmt.Errorf("%w: unknown current status %q", ErrTransition, from)
	}
	if !ValidStatus(to) {
		return fmt.Errorf("%w: unknown status %q: want one of %s", ErrTransition, to, statusList())
	}
	if from == to {
		return fmt.Errorf("%w: item is already %s", ErrTransition, to)
	}
	if !CanTransition(from, to) {
		return fmt.Errorf("%w: cannot go from %s to %s", ErrTransition, from, to)
	}
	return nil
}

// NextStatuses returns the statuses reachable from from, in lifecycle order.
// The web UI uses it to decide which buttons to offer, so the client cannot
// drift from the server's table.
func NextStatuses(from Status) []Status {
	next := allowedTransitions[from]
	out := make([]Status, 0, len(next))
	out = append(out, next...)
	return out
}
