package discovery

// Central pid-recorded start locks for the globally shared local servers (the
// embed server on its fixed port, and per-model chat instances).
//
// Scoping here is deliberate and differs from the LSP daemon path: LSP
// daemons are keyed by project root (broker.StartOnce flocks a per-root
// metadata lock, and the kernel releases that flock when the holder dies), so
// they need none of this bookkeeping. Embed and chat servers bind fixed ports
// shared by EVERY ocode process on the machine, so their spawn serialization
// is machine-global, never project-scoped.
//
// The lock file records its owner pid so contenders can tell three states
// apart:
//
//   - live owner mid-spawn    -> wait; adopt its server once healthy
//   - dead owner (crash/kill) -> reclaim IMMEDIATELY. This is why the pid is
//     recorded: after a crash there is nothing to wait for, and blocking on
//     an mtime timer made every post-crash start feel like a hang.
//   - garbage/pre-pid file    -> fall back to mtime staleness
//
// A live holder's lock is never taken before staleAfter elapses — breaking a
// live spawn's lock lets two processes race onto one port, and the loser's
// stray-reap path can then kill the winner's innocent server.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	gopsprocess "github.com/shirou/gopsutil/v4/process"
)

// acquirePidLock creates path exclusively and records os.Getpid as owner. If
// the file already exists it is reclaimed (removed + retried once) when either
// its recorded owner pid is no longer running or its mtime is older than
// staleAfter. Returns acquired=false when another live process holds it.
//
// The returned release func removes the lock ONLY if the file still records
// our own pid: if our stall got us declared dead and another process took
// over, an unconditional remove would delete THEIR lock and let a third
// process spawn a duplicate server onto the same port.
func acquirePidLock(path string, staleAfter time.Duration) (acquired bool, release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		f, openErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if openErr == nil {
			if _, writeErr := fmt.Fprintf(f, "%d", os.Getpid()); writeErr != nil {
				emitDiscoveryDebugAt("DISCOVERY", fmt.Sprintf("could not record owner pid in start lock %s: %v", path, writeErr), false)
			}
			f.Close()
			released := false
			return true, func() {
				if released {
					return
				}
				released = true
				pidLockRelease(path)
			}, nil
		}
		if !os.IsExist(openErr) {
			return false, nil, fmt.Errorf("create start lock %s: %w", path, openErr)
		}
		if attempt == 0 && pidLockReclaimable(path, staleAfter) {
			if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
				return false, nil, fmt.Errorf("reclaim start lock %s: %w", path, rmErr)
			}
			continue
		}
		break
	}
	return false, nil, nil
}

// pidLockOwner reads the recorded owner pid. ok=false covers missing, empty,
// pre-pid (older ocode), and torn files; callers then fall back to mtime
// staleness instead of guessing at ownership.
func pidLockOwner(path string) (pid int, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return 0, false
	}
	n, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if convErr != nil || n <= 1 {
		return 0, false // pid 0/1 are never an ocode holder
	}
	return n, true
}

// pidAlive reports whether pid is currently a running process. PID reuse can
// make a dead owner look alive; that errs toward waiting, which is the safe
// direction (the mtime budget remains as the eventual backstop).
func pidAlive(pid int) bool {
	exists, err := gopsprocess.PidExists(int32(pid))
	if err != nil {
		emitDiscoveryDebugAt("DISCOVERY", fmt.Sprintf("could not check whether start-lock owner pid %d is alive: %v", pid, err), false)
		return true // assume live rather than steal from a possible holder
	}
	return exists
}

// pidLockReclaimable decides whether an existing lock may be taken over:
// instantly when its recorded owner is dead, otherwise only after staleAfter
// (last-resort escape hatch for a wedged-but-alive holder, matching the
// pre-existing behavior of these locks).
func pidLockReclaimable(path string, staleAfter time.Duration) bool {
	if pid, ok := pidLockOwner(path); ok {
		if pidAlive(pid) {
			return false
		}
		emitDiscoveryDebugAt("DISCOVERY", fmt.Sprintf("start lock %s held by dead pid %d — reclaiming immediately", path, pid), false)
		return true
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		return false // vanished between create-attempt and stat: next O_EXCL wins
	}
	return time.Since(info.ModTime()) > staleAfter
}

// pidLockRelease removes path only if it still names us as owner. See the
// acquirePidLock comment for why this must be conditional.
func pidLockRelease(path string) {
	pid, ok := pidLockOwner(path)
	switch {
	case ok && pid == os.Getpid():
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			emitDiscoveryDebugAt("DISCOVERY", fmt.Sprintf("could not remove start lock %s: %v", path, err), false)
		}
	case ok:
		emitDiscoveryDebugAt("DISCOVERY", fmt.Sprintf("not removing start lock %s: ownership moved to pid %d", path, pid), false)
	default:
		// Missing/unreadable: nothing to clean up (or a torn file another
		// contender will reclaim via mtime).
	}
}
