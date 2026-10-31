package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/crashguard"
	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/version"
)

// serveProbeTimeout caps one address probe. Probes run concurrently, so a scan
// costs at most this long in total, however many siblings are recorded.
const serveProbeTimeout = 250 * time.Millisecond

// serveInstancePrefix names one live `ocode serve` process inside
// paths.ServersDir(): serve-<pid>.json. The file is written after the listener
// binds, holds an advisory lock for the owner's lifetime, and is removed on a
// clean exit. A reader can therefore tell "another server is running" (the lock
// is held) from "a server that died and left a note" (the lock is free), which
// a pid check alone cannot do: pids are recycled.
const serveInstancePrefix = "serve-"

// serveInstance is the on-disk record of one running server. Version is the
// whole point: servers sharing a remote host retire each other's remote server
// when their versions differ, so a starting server must be able to name the
// stale sibling that keeps doing it.
type serveInstance struct {
	PID       int       `json:"pid"`
	Version   string    `json:"version"`
	Addr      string    `json:"addr"`
	WorkDir   string    `json:"workdir,omitempty"`
	StartedAt time.Time `json:"started_at"`

	// Verified is set by the scanner and never read from or written to disk.
	// True means a live process holds this record's advisory lock, which proves
	// the pid in it is still the server that wrote it. Unverified records (a
	// build that predates locking, or one killed before it locked) are still
	// reported, but without a destructive stop hint: the pid may have been
	// recycled by an unrelated process.
	Verified bool `json:"-"`
}

// registerServeInstance records this process and returns a cleanup func that
// removes the record. Every failure is non-fatal: the registry only feeds a
// startup warning, so it must never stop a server from starting.
//
// The record is advisory-locked for the process's lifetime. The lock is what
// makes a scan trustworthy: the OS releases it when the process dies, so a
// stale record is provably ownerless even if its pid has been recycled.
func registerServeInstance(addr, workDir string) func() {
	noop := func() {}
	path, err := serveInstancePath(os.Getpid())
	if err != nil {
		log.Printf("serve: cannot record this server instance: %v", err)
		return noop
	}
	inst := serveInstance{
		PID:       os.Getpid(),
		Version:   version.Version,
		Addr:      addr,
		WorkDir:   workDir,
		StartedAt: time.Now(),
	}
	if err := writeServeInstance(path, inst); err != nil {
		log.Printf("serve: cannot record this server instance: %v", err)
		return noop
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		// The record is still useful (a scanner falls back to pid+address), so
		// keep it and only lose the identity guarantee.
		log.Printf("serve: cannot open this server instance record for locking: %v", err)
		return func() { removeServeRecord(path) }
	}
	if locked, lerr := tryLockServeRecord(f); lerr != nil || !locked {
		log.Printf("serve: cannot lock this server instance record (locked=%v): %v", locked, lerr)
	}
	return func() {
		unlockServeRecord(f)
		_ = f.Close()
		removeServeRecord(path)
	}
}

// removeServeRecord deletes a record file. A record that is already gone is
// fine (cleanup can run twice).
func removeServeRecord(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("serve: cannot remove instance record %s: %v", path, err)
	}
}

// warnServeSiblings logs one warning block per live sibling running a
// DIFFERENT version. It runs before the listener binds.
//
// Why a warning and not a kill: stopping another process is destructive and
// may drop that server's terminals and running turns. The version-populated
// warning names the pid so the user can decide; auto-stop is a separate,
// opt-in decision.
func warnServeSiblings() {
	for _, line := range serveSiblingWarnings(scanServeInstances(), version.Version) {
		log.Print(line)
	}
}

// serveSiblingWarnings renders the warning lines for instances whose version
// differs from selfVersion. Same-version siblings are normal (the desktop shell
// plus a dev server) and produce nothing.
//
// Only a Verified record gets a literal stop command. An unverified record's
// pid may belong to an unrelated process, so telling the user to kill it could
// be destructive advice.
func serveSiblingWarnings(instances []serveInstance, selfVersion string) []string {
	var out []string
	for _, inst := range instances {
		if inst.Version == selfVersion {
			continue
		}
		where := ""
		if inst.WorkDir != "" {
			where = ", cwd " + inst.WorkDir
		}
		out = append(out,
			fmt.Sprintf("serve: another ocode server is running: v%s on %s (pid %d%s)",
				inst.Version, inst.Addr, inst.PID, where),
		)
		if inst.Verified {
			out = append(out, fmt.Sprintf("serve: versions differ (this server is v%s) — that one keeps retiring shared remote servers, so stop it: %s",
				selfVersion, serveStopHint(runtime.GOOS, inst.PID)))
			continue
		}
		out = append(out, fmt.Sprintf("serve: versions differ (this server is v%s) — that one keeps retiring shared remote servers. Its record predates identity locking, so the pid may be stale; stop that ocode process yourself.",
			selfVersion))
	}
	return out
}

// serveStopHint names the command that stops pid on goos. Windows has no
// kill(1); taskkill is its equivalent.
func serveStopHint(goos string, pid int) string {
	if goos == "windows" {
		return fmt.Sprintf("taskkill /PID %d", pid)
	}
	return fmt.Sprintf("kill %d", pid)
}

func scanServeInstances() []serveInstance {
	return scanServeInstancesExcept(os.Getpid())
}

// serveCandidate is one record that survived the cheap checks, with whether its
// advisory lock was held (and so whether its pid is trustworthy).
type serveCandidate struct {
	inst  serveInstance
	owned bool
}

// scanServeInstancesExcept returns the live sibling records, dropping files
// whose owner is provably gone or that cannot be decoded. `self` is skipped so
// a process never reports itself.
//
// Ownership is decided by the record's advisory lock, not by the pid: the OS
// releases the lock when the owner dies, so a recycled pid can never make a
// stale record look alive. A record with no lock holder AND a dead pid is
// removed. An unlocked record whose pid is still alive is a pre-lock build (or
// a server killed between write and lock): it is reported as unverified and
// left on disk for its owner. Either way the address must be listening, so a
// record from a process that never bound is not announced.
func scanServeInstancesExcept(self int) []serveInstance {
	dir, err := paths.ServersDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	// Read and prune sequentially so only one goroutine ever touches the
	// directory; the address probes run afterwards.
	var candidates []serveCandidate
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, serveInstancePrefix) {
			continue
		}
		path := filepath.Join(dir, name)
		if strings.HasSuffix(name, ".tmp") {
			pruneServeTmp(path, name, self)
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		owned := serveRecordOwned(path)
		inst, ok := readServeInstance(path)
		if !ok {
			removeUndecodableServeRecord(path, name, self, owned)
			continue
		}
		if inst.PID == self {
			continue
		}
		if !owned && !servePIDAlive(inst.PID) {
			removeServeRecord(path)
			continue
		}
		candidates = append(candidates, serveCandidate{inst: inst, owned: owned})
	}

	// Probe every candidate concurrently: each probe is capped by
	// serveProbeTimeout, so the scan costs one timeout in total.
	listening := make([]bool, len(candidates))
	var wg sync.WaitGroup
	for i := range candidates {
		wg.Add(1)
		crashguard.Go(func() {
			defer wg.Done()
			listening[i] = serveAddrListening(candidates[i].inst.Addr)
		})
	}
	wg.Wait()

	var out []serveInstance
	for i, c := range candidates {
		if !listening[i] {
			continue
		}
		inst := c.inst
		inst.Verified = c.owned
		out = append(out, inst)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

// serveRecordOwned reports whether a live process still holds this record's
// advisory lock. It returns true on a lock error too: an unreadable record must
// never be deleted on a filesystem that does not support advisory locks.
func serveRecordOwned(path string) bool {
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()
	locked, err := tryLockServeRecord(f)
	if err != nil {
		return true
	}
	if !locked {
		return true
	}
	unlockServeRecord(f)
	return false
}

// removeUndecodableServeRecord handles a record that does not decode. Its
// owner may be a newer ocode whose format this build cannot read, so the file
// is removed only when its owner is confirmed gone. Otherwise it is kept and
// logged.
func removeUndecodableServeRecord(path, name string, self int, owned bool) {
	if owned {
		log.Printf("debug: serve registry: keeping undecodable record %s (a live owner holds its lock)", path)
		return
	}
	pid, ok := servePIDFromName(name)
	if !ok || pid == self || servePIDAlive(pid) {
		log.Printf("debug: serve registry: keeping undecodable record %s (owner not confirmed dead)", path)
		return
	}
	log.Printf("debug: serve registry: removing undecodable record %s (pid %d is gone)", path, pid)
	removeServeRecord(path)
}

// pruneServeTmp removes a half-written record left by a write that was killed
// before its rename. The scan filter ignores `.tmp` files, so nothing else
// would ever clean them; only a dead owner's file is removed.
func pruneServeTmp(path, name string, self int) {
	pid, ok := servePIDFromName(strings.TrimSuffix(name, ".tmp"))
	if !ok || pid == self || servePIDAlive(pid) {
		return
	}
	removeServeRecord(path)
}

// servePIDFromName parses the pid out of a serve-<pid>.json file name.
func servePIDFromName(name string) (int, bool) {
	raw, ok := strings.CutPrefix(name, serveInstancePrefix)
	if !ok {
		return 0, false
	}
	raw, ok = strings.CutSuffix(raw, ".json")
	if !ok {
		return 0, false
	}
	pid, err := strconv.Atoi(raw)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func serveInstancePath(pid int) (string, error) {
	dir, err := paths.ServersDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, serveInstancePrefix+strconv.Itoa(pid)+".json"), nil
}

func readServeInstance(path string) (serveInstance, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return serveInstance{}, false
	}
	var inst serveInstance
	if err := json.Unmarshal(data, &inst); err != nil || inst.PID <= 0 {
		return serveInstance{}, false
	}
	return inst, true
}

// writeServeInstance writes atomically so a concurrent scan never reads a
// half-written record.
func writeServeInstance(path string, inst serveInstance) error {
	data, err := json.Marshal(inst)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		// Leaving the tmp behind would accumulate; a rename failure means the
		// record was never published, so the tmp has no further use.
		removeServeRecord(tmp)
		return err
	}
	return nil
}

// serveAddrListening reports whether something accepts TCP connections on the
// recorded address. A wildcard bind is dialed through loopback.
func serveAddrListening(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" || port == "0" {
		return false
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), serveProbeTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
