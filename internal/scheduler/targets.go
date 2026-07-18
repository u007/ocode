package scheduler

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Targets is a tiny JSON-backed registry mapping "workdir → chatID" so the
// outbox drainer can forward cron results to the user's Telegram chat
// without forcing them to repeat the wiring. It is intentionally minimal:
// one file (`cron-targets.json`) under the scheduler store dir, no
// external dependencies, no schema migrations. Hosts own a Targets per
// project (one per DefaultStorePath).
//
// JSON shape:
//
//	{ "schema": 1, "entries": { "<abs workdir>": <chatID> } }
type Targets struct {
	path string
	mu   sync.Mutex
	data targetData
}

type targetData struct {
	Schema  int              `json:"schema"`
	Entries map[string]int64 `json:"entries"`
}

// NewTargets loads (or creates) a Targets registry rooted next to the
// given storePath (so targets live in the same project dir as
// jobs.json and deliveries.jsonl).
func NewTargets(storePath string) *Targets {
	return &Targets{
		path: filepath.Join(filepath.Dir(storePath), "cron-targets.json"),
		data: targetData{Schema: 1, Entries: map[string]int64{}},
	}
}

// Path returns the absolute path of the JSON file the registry is stored in.
func (t *Targets) Path() string { return t.path }

// ErrNotFound is returned by Get when no chat is registered for the workdir.
var ErrNotFound = errors.New("cron: no target chat registered for workdir")

// Get returns the chat id registered for the given workdir, or
// ErrNotFound when unset.
func (t *Targets) Get(workdir string) (int64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.loadLocked(); err != nil {
		return 0, err
	}
	id, ok := t.data.Entries[workdir]
	if !ok {
		return 0, ErrNotFound
	}
	return id, nil
}

// Set registers the chat id for the given workdir and persists the
// change. Overwrites any prior mapping. Passing a zero chatID removes the
// entry.
func (t *Targets) Set(workdir string, chatID int64) error {
	if workdir == "" {
		return errors.New("cron: workdir is required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.loadLocked(); err != nil {
		return err
	}
	if chatID == 0 {
		delete(t.data.Entries, workdir)
	} else {
		t.data.Entries[workdir] = chatID
	}
	return t.saveLocked()
}

// All returns a copy of the current (workdir, chatID) pairs.
func (t *Targets) All() map[string]int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	_ = t.loadLocked()
	out := make(map[string]int64, len(t.data.Entries))
	for k, v := range t.data.Entries {
		out[k] = v
	}
	return out
}

// loadLocked is the on-disk loader. Callers must hold t.mu.
func (t *Targets) loadLocked() error {
	if t.path == "" {
		return nil
	}
	data, err := os.ReadFile(t.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var d targetData
	if err := json.Unmarshal(data, &d); err != nil {
		return err
	}
	if d.Entries == nil {
		d.Entries = map[string]int64{}
	}
	t.data = d
	return nil
}

// saveLocked writes the registry atomically. Callers must hold t.mu.
func (t *Targets) saveLocked() error {
	if t.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(t.path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(t.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := t.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, t.path)
}

// TargetsFor returns a Targets for the given project workdir, computed
// the same way as DefaultStorePath.
func TargetsFor(workDir string) (*Targets, error) {
	p, err := DefaultStorePath(workDir)
	if err != nil {
		return nil, err
	}
	return NewTargets(p), nil
}
