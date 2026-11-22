package boxsession

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	gopsprocess "github.com/shirou/gopsutil/v4/process"
	"github.com/u007/ocode/internal/filelock"
)

// KindEphemeral is a box that Upstash deletes at its ttl. ocode may delete it
// early, and the orphan sweep removes it when its owner is gone.
const KindEphemeral = "ephemeral"

// KindRetained is a box that outlives its session. The orphan sweep never
// deletes it.
const KindRetained = "retained"

// ErrRetainedBusy means a retained box cannot be resumed now: another ocode
// process holds it running, or a delete of it is in progress. The caller wraps it
// with the login state name.
var ErrRetainedBusy = errors.New("the retained box is in use by another ocode process, or is being deleted")

// Lease identifies the ocode process that owns a box and says when that process
// last proved it was alive. ProcessStart distinguishes a process from a later
// process that reuses the same PID.
type Lease struct {
	PID          int       `json:"pid"`
	ProcessStart time.Time `json:"process_start"`
	Heartbeat    time.Time `json:"heartbeat"`
}

// Entry is one journaled box. The journal holds no secret: no API key and no
// CDP URL.
//
// For an ephemeral box ExpiresAt is the box's ttl. For a retained box it is the
// time the box is deleted: LastUsed plus the account's retain period.
type Entry struct {
	BoxID     string    `json:"box_id"`
	AccountID string    `json:"account_id"`
	SessionID string    `json:"session_id"`
	Kind      string    `json:"kind"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Lease     Lease     `json:"lease"`
	// LoginState names a retained box's login state. Empty for an ephemeral box.
	LoginState string `json:"login_state,omitempty"`
	// Paused is true while a retained box is paused. A paused box belongs to no
	// process, so its lease is not consulted.
	Paused bool `json:"paused,omitempty"`
	// Deleting is true while a process has claimed a retained box for deletion.
	// The claim's lease is the claimer's. A claimer that dies mid-delete leaves a
	// row another process can finish.
	Deleting bool `json:"deleting,omitempty"`
	// LastUsed is when a retained box was last claimed or paused. Its retain period
	// is ExpiresAt minus LastUsed.
	LastUsed time.Time `json:"last_used,omitzero"`
}

// journalFile is the on-disk form of upstash-boxes.json.
type journalFile struct {
	Version int     `json:"version"`
	Boxes   []Entry `json:"boxes"`
}

// journalVersion is the only file version this code reads or writes.
const journalVersion = 1

// Journal records every box this installation created, so a crashed process's
// boxes can be found and deleted later. Several ocode processes share one file.
// Every mutation is a load-modify-write under a cross-process lock, and the
// file is replaced atomically. A corrupt file is an error, never a reset.
type Journal struct {
	path     string
	lockPath string
	// alive reports whether the process that wrote a lease is still running. Resume
	// uses it to refuse a row that a live peer holds. Manager.New binds
	// Deps.LeaseAlive here. Until then it is leaseAlive.
	alive func(l Lease) bool
}

// NewJournal returns a journal stored at path. The file is created on the first
// mutation, and its directory is created then too.
func NewJournal(path string) *Journal {
	return &Journal{path: path, lockPath: path + ".lock", alive: leaseAlive}
}

// bindAlive sets the liveness check Resume uses. Manager.New calls it, so the
// journal and the manager agree on which leases are live.
func (j *Journal) bindAlive(alive func(l Lease) bool) {
	if alive != nil {
		j.alive = alive
	}
}

// Add journals a new box. It fails when BoxID is already present.
func (j *Journal) Add(e Entry) error {
	if e.BoxID == "" {
		return errors.New("boxsession: journal entry has no box id")
	}
	if e.Kind != KindEphemeral && e.Kind != KindRetained {
		return fmt.Errorf("boxsession: journal entry %s has unknown kind %q", e.BoxID, e.Kind)
	}
	if e.Kind == KindRetained && e.LoginState == "" {
		return fmt.Errorf("boxsession: retained journal entry %s has no login state", e.BoxID)
	}
	return j.mutate(func(entries []Entry) ([]Entry, bool, error) {
		if slices.ContainsFunc(entries, func(x Entry) bool { return x.BoxID == e.BoxID }) {
			return nil, false, fmt.Errorf("boxsession: box %s is already journaled", e.BoxID)
		}
		// At most one retained box per account and login state: a second would
		// make Start's lookup ambiguous.
		if e.Kind == KindRetained && slices.ContainsFunc(entries, func(x Entry) bool {
			return sameRetainedKey(x, e.AccountID, e.LoginState)
		}) {
			return nil, false, fmt.Errorf("boxsession: login state %q already has a retained box", e.LoginState)
		}
		return append(entries, e), true, nil
	})
}

// sameRetainedKey reports whether x is the retained row of accountID and loginState.
func sameRetainedKey(x Entry, accountID, loginState string) bool {
	return x.Kind == KindRetained && x.AccountID == accountID && x.LoginState == loginState
}

// Retained returns the retained row of accountID's login state loginState. At
// most one exists. A missing row is ok=false with a nil error.
func (j *Journal) Retained(accountID, loginState string) (Entry, bool, error) {
	entries, err := j.load()
	if err != nil {
		return Entry{}, false, err
	}
	for _, e := range entries {
		if sameRetainedKey(e, accountID, loginState) {
			return e, true, nil
		}
	}
	return Entry{}, false, nil
}

// SetPaused marks the retained row of boxID paused. A zero lastUsed or expiresAt
// keeps the row's current value, so a caller that did not read the row never
// writes a stale time over it. The lease and session are left as they are: a
// paused box belongs to no process, and the session that last used it keeps the row.
func (j *Journal) SetPaused(boxID string, lastUsed, expiresAt time.Time) error {
	return j.updateRetained(boxID, func(e *Entry) {
		e.Paused = true
		if !lastUsed.IsZero() {
			e.LastUsed = lastUsed
		}
		if !expiresAt.IsZero() {
			e.ExpiresAt = expiresAt
		}
	})
}

// Resume marks the retained row of boxID as running: unpaused, owned by owner,
// and held by sessionID, with its last-use time and delete-at. It is a
// compare-and-set. It fails with ErrRetainedBusy when the row is being deleted, or
// when it is running under another process that is still alive. A session that
// resumes a paused box, or a box this process already owns, moves the row to
// itself. owner.Heartbeat is the caller's clock, which the liveness rule reads.
func (j *Journal) Resume(boxID, sessionID string, owner Lease, lastUsed, expiresAt time.Time) error {
	if sessionID == "" {
		return fmt.Errorf("boxsession: resume of box %s needs a session id", boxID)
	}
	now := owner.Heartbeat
	return j.mutate(func(entries []Entry) ([]Entry, bool, error) {
		out := slices.Clone(entries)
		for i := range out {
			if out[i].BoxID != boxID {
				continue
			}
			if out[i].Kind != KindRetained {
				return nil, false, fmt.Errorf("boxsession: journal row of box %s is not a retained box", boxID)
			}
			busy := out[i].Deleting ||
				(!out[i].Paused && !sameOwner(out[i].Lease, owner) && !leaseDead(out[i].Lease, now, leaseTTL, j.alive))
			if busy {
				return nil, false, ErrRetainedBusy
			}
			out[i].Paused = false
			out[i].SessionID = sessionID
			out[i].Lease = owner
			out[i].LastUsed = lastUsed
			out[i].ExpiresAt = expiresAt
			return out, true, nil
		}
		return nil, false, fmt.Errorf("boxsession: no journal row for box %s", boxID)
	})
}

// updateRetained applies set to the retained row of boxID under the lock. A
// missing row, or one that is not retained, is an error.
func (j *Journal) updateRetained(boxID string, set func(e *Entry)) error {
	return j.mutate(func(entries []Entry) ([]Entry, bool, error) {
		out := slices.Clone(entries)
		for i := range out {
			if out[i].BoxID != boxID {
				continue
			}
			if out[i].Kind != KindRetained {
				return nil, false, fmt.Errorf("boxsession: journal row of box %s is not a retained box", boxID)
			}
			set(&out[i])
			return out, true, nil
		}
		return nil, false, fmt.Errorf("boxsession: no journal row for box %s", boxID)
	})
}

// dueKind is what a retained row needs from a reaper pass.
type dueKind int

const (
	dueNone dueKind = iota
	// dueDelete: the box must be deleted. Its delete-at has passed, or its claimer died mid-delete.
	dueDelete
	// dueStray: the box runs under a dead process and is inside its period, so it must be paused.
	dueStray
)

// retainedDue is the one rule for what a retained row needs now. RetainedDue
// lists rows by it and ClaimDue re-applies it inside the claim, so a snapshot and
// its claim cannot disagree.
func retainedDue(e Entry, owner Lease, now time.Time, leaseTTL time.Duration, alive func(l Lease) bool) dueKind {
	if e.Kind != KindRetained {
		return dueNone
	}
	if e.Deleting {
		// A delete whose claimer is gone is finished by whoever finds it. A row this
		// process claimed is due too: when releasing that claim failed after a failed
		// delete, nothing else would retry it. A delete in flight in this process is
		// excluded by the sweeping reservation, not by the lease.
		if sameOwner(e.Lease, owner) || leaseDead(e.Lease, now, leaseTTL, alive) {
			return dueDelete
		}
		return dueNone
	}
	if e.Paused {
		if !now.Before(e.ExpiresAt) {
			return dueDelete
		}
		return dueNone
	}
	if sameOwner(e.Lease, owner) || !leaseDead(e.Lease, now, leaseTTL, alive) {
		return dueNone
	}
	if !now.Before(e.ExpiresAt) {
		return dueDelete
	}
	return dueStray
}

// RetainedDue sorts the retained rows that need an action now.
//
// expired are the rows past their delete-at that the sweep must delete: every
// paused row past its delete-at, and every unpaused row past it whose owner is
// dead. strays are the unpaused rows still inside their period whose owner is
// dead, so the box is running with no process to pause it. A row of owner, or of
// a live process, is in neither list. RetainedDue is a snapshot: a caller acts on
// a row only after ClaimDue succeeds for it.
func (j *Journal) RetainedDue(now time.Time, owner Lease, leaseTTL time.Duration, alive func(l Lease) bool) (expired, strays []Entry, err error) {
	if alive == nil {
		return nil, nil, errors.New("boxsession: RetainedDue needs a PID liveness check")
	}
	entries, err := j.load()
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		switch retainedDue(e, owner, now, leaseTTL, alive) {
		case dueDelete:
			expired = append(expired, e)
		case dueStray:
			strays = append(strays, e)
		}
	}
	sortEntries(expired)
	sortEntries(strays)
	return expired, strays, nil
}

// ClaimDue claims the retained row of boxID for the action its state still calls
// for. It re-applies the RetainedDue rule inside the journal lock, so a row that a
// Resume, or another claimer, changed since the snapshot is refused with ok false.
//
// action is "delete" or "pause". A delete sets Deleting. A claim of either kind
// stamps owner as the row's lease, so the row is this process's to act on. A
// claimer that dies mid-delete leaves a row another process can finish. ok is
// false, with a nil error, when nothing was claimed.
func (j *Journal) ClaimDue(boxID string, now time.Time, owner Lease, leaseTTL time.Duration, alive func(l Lease) bool) (action string, ok bool, err error) {
	if alive == nil {
		return "", false, errors.New("boxsession: ClaimDue needs a PID liveness check")
	}
	err = j.mutate(func(entries []Entry) ([]Entry, bool, error) {
		out := slices.Clone(entries)
		for i := range out {
			if out[i].BoxID != boxID {
				continue
			}
			switch retainedDue(out[i], owner, now, leaseTTL, alive) {
			case dueDelete:
				action = "delete"
				out[i].Deleting = true
			case dueStray:
				action = "pause"
			default:
				return nil, false, nil
			}
			out[i].Lease = owner
			ok = true
			return out, true, nil
		}
		return nil, false, nil
	})
	if err != nil {
		return "", false, err
	}
	return action, ok, nil
}

// releaseClaim gives back a claim ClaimDue made for owner, after the action it was
// taken for failed. It clears the Deleting flag and gives the row a lease no
// process holds: PID 0 is never alive, and a zero heartbeat is stale. The row is
// then a dead owner's again, so the next sweep finds it and retries. It acts only
// while owner still holds the row, so it never undoes another process's claim.
func (j *Journal) releaseClaim(boxID string, owner Lease) error {
	return j.mutate(func(entries []Entry) ([]Entry, bool, error) {
		out := slices.Clone(entries)
		for i := range out {
			if out[i].BoxID == boxID && sameOwner(out[i].Lease, owner) {
				out[i].Deleting = false
				out[i].Lease = Lease{}
				return out, true, nil
			}
		}
		return nil, false, nil
	})
}

// leaseDead reports whether the process that wrote lease is gone: its heartbeat
// is older than leaseTTL and alive reports it not running. Orphans, RetainedDue,
// ClaimDue and the retained-box Resume check all use this one rule.
func leaseDead(l Lease, now time.Time, leaseTTL time.Duration, alive func(l Lease) bool) bool {
	return now.Sub(l.Heartbeat) > leaseTTL && !alive(l)
}

// leaseAlive reports whether the process that wrote l is still running. It is the
// production liveness check for every journal row.
func leaseAlive(l Lease) bool {
	return leaseAliveWith(l, probeProcess)
}

// processProbe reports whether pid names a running process, and when that process
// was created. created is zero when the creation time is unknown.
type processProbe func(pid int) (exists bool, created time.Time, err error)

// probeProcess is the production processProbe: gopsutil's PidExists and CreateTime.
func probeProcess(pid int) (bool, time.Time, error) {
	exists, err := gopsprocess.PidExists(int32(pid))
	if err != nil || !exists {
		return exists, time.Time{}, err
	}
	p, err := gopsprocess.NewProcess(int32(pid))
	if err != nil {
		return true, time.Time{}, err
	}
	ms, err := p.CreateTime()
	if err != nil {
		return true, time.Time{}, err
	}
	return true, time.UnixMilli(ms), nil
}

// leaseAliveWith is leaseAlive over an injected probe. A PID that does not exist is
// dead. A probe error counts as alive, as pidAlive does, so a sweep never deletes a
// box it cannot prove orphaned. A PID is reused after a reboot: a process created
// after the lease's last heartbeat cannot be the process that wrote the lease, so
// that lease is dead.
func leaseAliveWith(l Lease, probe processProbe) bool {
	if l.PID <= 0 {
		return false
	}
	exists, created, err := probe(l.PID)
	if err != nil {
		return true
	}
	if !exists {
		return false
	}
	return created.IsZero() || !created.After(l.Heartbeat)
}

// Remove drops the entry for boxID. A missing id is not an error.
func (j *Journal) Remove(boxID string) error {
	return j.mutate(func(entries []Entry) ([]Entry, bool, error) {
		kept := slices.DeleteFunc(slices.Clone(entries), func(x Entry) bool { return x.BoxID == boxID })
		return kept, len(kept) != len(entries), nil
	})
}

// Rekey moves every entry of oldSession to newSession. It is a no-op when no
// entry matches.
func (j *Journal) Rekey(oldSession, newSession string) error {
	_, err := j.rekeyMoved(oldSession, newSession)
	return err
}

// rekeyMoved is Rekey. It also returns the box ids whose rows it moved, so a caller
// can undo exactly that move with restoreSession.
func (j *Journal) rekeyMoved(oldSession, newSession string) ([]string, error) {
	if newSession == "" {
		return nil, errors.New("boxsession: rekey needs a new session id")
	}
	if oldSession == newSession {
		return nil, nil
	}
	var moved []string
	err := j.mutate(func(entries []Entry) ([]Entry, bool, error) {
		moved = nil
		out := slices.Clone(entries)
		for i := range out {
			if out[i].SessionID == oldSession {
				out[i].SessionID = newSession
				moved = append(moved, out[i].BoxID)
			}
		}
		return out, len(moved) > 0, nil
	})
	return moved, err
}

// restoreSession sets the session of the rows of boxIDs to session. It undoes a
// rekeyMoved and touches no other row.
func (j *Journal) restoreSession(boxIDs []string, session string) error {
	if len(boxIDs) == 0 {
		return nil
	}
	return j.mutate(func(entries []Entry) ([]Entry, bool, error) {
		out := slices.Clone(entries)
		changed := false
		for i := range out {
			if slices.Contains(boxIDs, out[i].BoxID) {
				out[i].SessionID = session
				changed = true
			}
		}
		return out, changed, nil
	})
}

// Heartbeat sets the heartbeat to now on every entry that owner's process
// created. An entry matches when both PID and ProcessStart are equal.
func (j *Journal) Heartbeat(owner Lease, now time.Time) error {
	return j.mutate(func(entries []Entry) ([]Entry, bool, error) {
		changed := false
		out := slices.Clone(entries)
		for i := range out {
			if sameOwner(out[i].Lease, owner) {
				out[i].Lease.Heartbeat = now
				changed = true
			}
		}
		return out, changed, nil
	})
}

// List returns every entry, sorted by CreatedAt and then BoxID.
func (j *Journal) List() ([]Entry, error) {
	entries, err := j.load()
	if err != nil {
		return nil, err
	}
	sortEntries(entries)
	return entries, nil
}

// Orphans returns the ephemeral entries that owner did not create, whose
// heartbeat is older than leaseTTL, and whose lease alive reports as dead.
// Retained entries, entries with a fresh heartbeat and entries of a live lease are
// never returned. A PID reused by a later process is not a live lease: alive
// compares the process creation time with the heartbeat (see leaseAliveWith).
func (j *Journal) Orphans(owner Lease, now time.Time, leaseTTL time.Duration, alive func(l Lease) bool) ([]Entry, error) {
	if alive == nil {
		return nil, errors.New("boxsession: Orphans needs a PID liveness check")
	}
	entries, err := j.load()
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range entries {
		if e.Kind != KindEphemeral || sameOwner(e.Lease, owner) {
			continue
		}
		if !leaseDead(e.Lease, now, leaseTTL, alive) {
			continue
		}
		out = append(out, e)
	}
	sortEntries(out)
	return out, nil
}

// sameOwner reports whether a and b name the same process incarnation.
func sameOwner(a, b Lease) bool {
	return a.PID == b.PID && a.ProcessStart.Equal(b.ProcessStart)
}

func sortEntries(entries []Entry) {
	slices.SortFunc(entries, func(a, b Entry) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		switch {
		case a.BoxID < b.BoxID:
			return -1
		case a.BoxID > b.BoxID:
			return 1
		}
		return 0
	})
}

// load reads the journal. A missing file, or an empty one, is an empty journal.
// Any other content that does not parse is an error.
func (j *Journal) load() ([]Entry, error) {
	data, err := os.ReadFile(j.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("boxsession: read journal %s: %w", j.path, err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var f journalFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("boxsession: journal %s is corrupt, not resetting it: %w", j.path, err)
	}
	if f.Version != journalVersion {
		return nil, fmt.Errorf("boxsession: journal %s has version %d, want %d", j.path, f.Version, journalVersion)
	}
	return f.Boxes, nil
}

// mutate runs one load-modify-write under the cross-process lock. fn returns
// the new entries and whether they differ from the old ones. The file is
// written only when they differ.
func (j *Journal) mutate(fn func(entries []Entry) ([]Entry, bool, error)) error {
	if err := os.MkdirAll(filepath.Dir(j.path), 0o755); err != nil {
		return fmt.Errorf("boxsession: create journal directory: %w", err)
	}
	return filelock.WithFileLock(j.lockPath, func() error {
		entries, err := j.load()
		if err != nil {
			return err
		}
		next, changed, err := fn(entries)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		return j.save(next)
	})
}

// save replaces the journal file atomically: write a temp file in the same
// directory, sync it, then rename it over the journal.
func (j *Journal) save(entries []Entry) error {
	data, err := json.MarshalIndent(journalFile{Version: journalVersion, Boxes: entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("boxsession: encode journal: %w", err)
	}
	dir := filepath.Dir(j.path)
	tmp, err := os.CreateTemp(dir, ".upstash-boxes-*.tmp")
	if err != nil {
		return fmt.Errorf("boxsession: create temp journal: %w", err)
	}
	tmpName := tmp.Name()
	// After a successful rename this is a no-op; on any failure it removes the temp file.
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("boxsession: write temp journal: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("boxsession: chmod temp journal: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("boxsession: sync temp journal: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("boxsession: close temp journal: %w", err)
	}
	if err := os.Rename(tmpName, j.path); err != nil {
		return fmt.Errorf("boxsession: replace journal: %w", err)
	}
	return nil
}
