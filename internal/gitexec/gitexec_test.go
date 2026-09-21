package gitexec

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestEnvSetsOptionalLocksOff pins the env contract: every ocode git child must
// run with GIT_OPTIONAL_LOCKS=0 (git's documented setting for background
// processes that must not cause lock contention), the inherited value must not
// leak through, and the rest of the environment must survive.
func TestEnvSetsOptionalLocksOff(t *testing.T) {
	t.Setenv("GIT_OPTIONAL_LOCKS", "1")
	t.Setenv("OCODE_GITEXEC_TEST", "kept")

	var lockEntries []string
	sawKept := false
	for _, kv := range Env() {
		if strings.HasPrefix(kv, "GIT_OPTIONAL_LOCKS=") {
			lockEntries = append(lockEntries, kv)
		}
		if kv == "OCODE_GITEXEC_TEST=kept" {
			sawKept = true
		}
	}

	if len(lockEntries) != 1 {
		t.Fatalf("GIT_OPTIONAL_LOCKS entries = %v, want exactly one (inherited value must be replaced)", lockEntries)
	}
	if lockEntries[0] != optionalLocksOff {
		t.Fatalf("GIT_OPTIONAL_LOCKS entry = %q, want %q", lockEntries[0], optionalLocksOff)
	}
	if !sawKept {
		t.Fatal("Env dropped unrelated environment entries")
	}
}

// TestLockHeldDetection pins what counts as index-lock contention. The
// "user report" case is the exact text from the field bug; the false cases
// guard the match from widening into every error that merely mentions a lock.
func TestLockHeldDetection(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{
			"user report",
			errors.New("exit status 128: fatal: Unable to create '/Users/james/www/proposal/.git/index.lock': File exists.\n\nAnother git process seems to be running in this repository, or the lock file may be stale"),
			true,
		},
		{
			"no advice line",
			errors.New("exit status 128: fatal: Unable to create '/repo/.git/index.lock': File exists."),
			true,
		},
		{
			"advice line only",
			errors.New("fatal: Another git process seems to be running in this repository"),
			true,
		},
		{
			"not a repository",
			errors.New("exit status 128: fatal: not a git repository (or any of the parent directories): .git"),
			false,
		},
		{
			"pathspec mentions a lock file",
			errors.New("exit status 1: error: pathspec 'index.lock.bak' did not match any file(s) known to git"),
			false,
		},
		{
			"index refresh failure is not lock contention",
			errors.New("exit status 128: fatal: index file smaller than expected"),
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LockHeld(tc.err); got != tc.want {
				t.Fatalf("LockHeld(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// withFastRetry compresses the retry schedule so the retry tests do not sleep.
func withFastRetry(t *testing.T, delays ...time.Duration) {
	t.Helper()
	oldDelays, oldSleep := lockRetryDelays, sleep
	lockRetryDelays, sleep = delays, func(time.Duration) {}
	t.Cleanup(func() { lockRetryDelays, sleep = oldDelays, oldSleep })
}

func TestWithLockRetrySucceedsOnFirstTry(t *testing.T) {
	withFastRetry(t, time.Millisecond)

	attempts := 0
	if err := WithLockRetry(func() error { attempts++; return nil }); err != nil {
		t.Fatalf("WithLockRetry returned %v, want nil", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

// TestWithLockRetryRidesOutTransientLock is the fix in miniature: the first
// attempts lose the .git/index.lock race, a later one wins.
func TestWithLockRetryRidesOutTransientLock(t *testing.T) {
	withFastRetry(t, time.Millisecond, time.Millisecond, time.Millisecond)

	lockErr := errors.New("fatal: Unable to create '/repo/.git/index.lock': File exists.")
	attempts := 0
	err := WithLockRetry(func() error {
		attempts++
		if attempts < 3 {
			return lockErr
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithLockRetry returned %v, want nil once the holder released the lock", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3 (two lock failures then success)", attempts)
	}
}

// TestWithLockRetryExhaustsAndAnnotatesError pins the terminal error: the
// original git stderr survives (it carries the lock path and git's own advice)
// and the message says ocode already waited, so a user knows a lock still
// present is stale.
func TestWithLockRetryExhaustsAndAnnotatesError(t *testing.T) {
	withFastRetry(t, time.Millisecond, time.Millisecond)

	lockErr := errors.New("exit status 128: fatal: Unable to create '/repo/.git/index.lock': File exists.")
	attempts := 0
	err := WithLockRetry(func() error { attempts++; return lockErr })
	if err == nil {
		t.Fatal("WithLockRetry returned nil, want the lock error")
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 1 + 2 retries = 3", attempts)
	}
	if !errors.Is(err, lockErr) {
		t.Fatalf("error %v does not wrap the original lock error", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "index.lock") {
		t.Fatalf("error %q dropped git's own stderr (the lock path)", msg)
	}
	if !strings.Contains(msg, "attempted this git command 3 times") {
		t.Fatalf("error %q does not report the retry, so a stale lock is indistinguishable from no attempt", msg)
	}
}

// TestWithLockRetryDoesNotRetryOtherErrors keeps the retry scoped to lock
// contention: a real git failure must surface immediately, once.
func TestWithLockRetryDoesNotRetryOtherErrors(t *testing.T) {
	withFastRetry(t, time.Millisecond, time.Millisecond)

	want := errors.New("exit status 1: error: pathspec 'nope' did not match any file(s) known to git")
	attempts := 0
	err := WithLockRetry(func() error { attempts++; return want })
	if !errors.Is(err, want) {
		t.Fatalf("WithLockRetry returned %v, want the original error", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (non-lock errors are not retried)", attempts)
	}
}
