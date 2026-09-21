// Package gitexec centralizes how ocode spawns the git binary so that ocode's
// own git traffic cannot turn into user-visible lock failures.
//
// Background git use is unavoidable in ocode: the server's git-status emitter
// probes every viewed project every 10s (internal/server/emitters.go), the web
// Git tab refreshes on a 10s poll plus git_status bus events, and the TUI
// re-badges the file tree from `git status` on its own ticker. All of those are
// *read* paths, but git's optional locks make them writers anyway — `git status`
// refreshes the index as a side effect, and so does `git diff`. Two consequences
// follow, and this package addresses both:
//
//   - ocode was a lock **contender**. A user's `git add` (in a terminal, an
//     editor, or another ocode tab) could lose the race and fail with
//     "Unable to create '<repo>/.git/index.lock': File exists. Another git
//     process seems to be running in this repository, or the lock file may be
//     stale". Env (below) makes ocode's git children skip optional
//     lock-taking, exactly what git documents GIT_OPTIONAL_LOCKS for:
//     "processes running in the background which do not want to cause lock
//     contention with other operations on the repository".
//
//   - A killed git child could **leave** a stale .git/index.lock behind. The
//     status probes run under a hard deadline (gitStatusTimeout) which
//     SIGKILLs git; a probe killed between creating index.lock and renaming it
//     into place strands the lock and breaks the next `git add`. Never taking
//     the optional lock removes that window entirely.
//
// When a mutation still loses the race, the holder is nearly always a
// short-lived git process (a user's git command, an editor's index refresh).
// WithLockRetry turns that transient failure into success instead of a red
// error in the UI.
package gitexec

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// optionalLocksOff is the environment entry that stops git performing optional
// lock-taking side effects. It is the environment-variable form of git's
// `--no-optional-locks` flag (see git(1)).
const optionalLocksOff = "GIT_OPTIONAL_LOCKS=0"

// Env returns the environment for an ocode-spawned git command: the current
// process environment plus GIT_OPTIONAL_LOCKS=0, with any inherited
// GIT_OPTIONAL_LOCKS dropped so ocode's children are deterministic.
//
// This is safe for mutating commands as well as read-only probes: only
// *optional* locks are skipped, so `git add`, `commit`, `stash` and
// `apply --cached` still take the locks they need and still fail loudly when
// the index is genuinely locked (verified against git 2.54: with
// GIT_OPTIONAL_LOCKS=0 and a pre-existing .git/index.lock, `git add` still
// exits 128).
func Env() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_OPTIONAL_LOCKS=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, optionalLocksOff)
}

// LockHeld reports whether err is git failing to acquire .git/index.lock.
//
// git prints "fatal: Unable to create '<path>/index.lock': File exists."
// followed by the advice line "Another git process seems to be running in this
// repository, or the lock file may be stale". Callers fold stderr into the
// returned error, so matching the message text is how the retry loop
// recognises the one failure mode worth retrying. The match is deliberately
// narrow — a bare ".lock" in a pathspec error must not qualify.
func LockHeld(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "another git process seems to be running") {
		return true
	}
	return strings.Contains(msg, "unable to create") && strings.Contains(msg, "index.lock")
}

// lockRetryDelays is how long WithLockRetry waits before each re-attempt after
// the first. The total window (~900ms) is long enough to outlast the short-lived
// holders that cause almost all contention, and short enough that a UI action
// never appears to hang. A package var so tests can compress the schedule.
var lockRetryDelays = []time.Duration{
	100 * time.Millisecond,
	150 * time.Millisecond,
	250 * time.Millisecond,
	400 * time.Millisecond,
}

// sleep is a seam so tests do not wait in real time.
var sleep = time.Sleep

// WithLockRetry runs fn and, if it fails because .git/index.lock is held,
// retries it a bounded number of times (see lockRetryDelays). fn must be
// idempotent in the sense that matters here: a failed lock acquisition means
// git never started the operation, so re-running is safe.
//
// The final error keeps the original git stderr (so the caller still shows the
// exact path and git's own advice) with a short suffix naming the retry, which
// is what tells a user staring at "Another git process seems to be running"
// that ocode already waited — and that a lock file still there is stale.
// Errors that are not lock contention are returned untouched, with no retry.
func WithLockRetry(fn func() error) error {
	err := fn()
	if !LockHeld(err) {
		return err
	}
	attempts := 1
	for _, delay := range lockRetryDelays {
		sleep(delay)
		attempts++
		if err = fn(); !LockHeld(err) {
			return err
		}
	}
	return fmt.Errorf("%w (ocode attempted this git command %d times over %s; another git process may be holding the repository lock — if none is running, the .git/index.lock file is stale and can be deleted)",
		err, attempts, retryWindow())
}

// retryWindow is the total wait WithLockRetry spends between attempts.
func retryWindow() time.Duration {
	var total time.Duration
	for _, d := range lockRetryDelays {
		total += d
	}
	return total
}
