// Package rc implements discovery of running ocode "remote control" (/rc)
// instances so that external clients (e.g. the Telegram bot) can find and
// drive every ocode instance on this machine.
//
// Each /rc instance writes a small JSON file named instance-<id>.json into a
// shared directory under the global data dir. A heartbeat keeps the entry
// fresh; /rc off (or a crash) lets the entry go stale and it is pruned by
// readers via List's TTL. Files are per-instance so concurrent writers never
// contend on a single lock file.
package rc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/u007/ocode/internal/paths"
)

const (
	// DirName is the sub-directory of GlobalDataDir that holds instance files.
	DirName = "rc"
	// FilePattern matches a single instance file (glob).
	FilePattern = "instance-*.json"
	// DefaultTTL is how long an entry may go without a heartbeat before List
	// treats it as dead and prunes it.
	DefaultTTL = 60 * time.Second
	// HeartbeatInterval is how often a live instance should Touch its entry.
	HeartbeatInterval = 15 * time.Second
)

// Entry describes one running /rc instance.
type Entry struct {
	InstanceID string `json:"instance_id"` // unique per /rc start
	SessionID  string `json:"session_id"`  // the ocode session being shared
	Model      string `json:"model"`       // model in use
	CWD        string `json:"cwd"`         // project working directory
	Addr       string `json:"addr"`        // host:port the RC server listens on
	Token      string `json:"token"`       // RC auth token (local only)
	PID        int    `json:"pid"`         // owning process id
	StartedAt  int64  `json:"started_at"`  // unix seconds
	LastSeen   int64  `json:"last_seen"`   // unix seconds, refreshed by heartbeat
}

// baseDirOverride, when set, replaces the resolved global data dir. Used by
// tests to avoid touching the real data dir.
var baseDirOverride string

// SetBaseDirForTest overrides the registry directory (test only).
func SetBaseDirForTest(dir string) {
	baseDirOverride = dir
}

// WithDir runs f with baseDirOverride temporarily set to dir and restores the
// previous value afterwards. It lets callers scope a single lookup to a
// specific registry directory without permanently mutating the global (which
// would race with concurrent or test-driven lookups).
func WithDir(dir string, f func() error) error {
	prev := baseDirOverride
	baseDirOverride = dir
	defer func() { baseDirOverride = prev }()
	return f()
}

// FindIn resolves an instance id against a specific registry directory rather
// than the process-global default. Used by the Telegram bot when configured
// with a non-default OCODE_TG_RC_DIR so every lookup is consistent.
func FindIn(dir, id string) (Entry, bool) {
	var out Entry
	var ok bool
	_ = WithDir(dir, func() error {
		out, ok = Find(id)
		return nil
	})
	return out, ok
}

// ListIn lists live instances from a specific registry directory.
func ListIn(dir string, ttl time.Duration) ([]Entry, error) {
	var out []Entry
	var err error
	_ = WithDir(dir, func() error {
		out, err = List(ttl)
		return nil
	})
	return out, err
}

// Dir returns (creating if needed) the directory holding instance files.
func Dir() (string, error) {
	if baseDirOverride != "" {
		if err := os.MkdirAll(baseDirOverride, 0o700); err != nil {
			return "", err
		}
		return baseDirOverride, nil
	}
	base, err := paths.GlobalDataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func fileName(id string) string { return "instance-" + id + ".json" }

// Register writes (or refreshes) an instance entry atomically.
func Register(e Entry) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	e.LastSeen = time.Now().Unix()
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".tmp-"+e.InstanceID)
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, fileName(e.InstanceID)))
}

// Touch refreshes LastSeen for a live instance.
func Touch(instanceID string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	p := filepath.Join(dir, fileName(instanceID))
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	var e Entry
	if err := json.Unmarshal(b, &e); err != nil {
		return err
	}
	e.LastSeen = time.Now().Unix()
	nb, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, nb, 0o600)
}

// Unregister removes an instance file (best-effort).
func Unregister(instanceID string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(dir, fileName(instanceID)))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// List returns live instance entries, pruning any that have not been heartbeated
// within ttl. A ttl <= 0 disables pruning.
func List(ttl time.Duration) ([]Entry, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	matches, _ := filepath.Glob(filepath.Join(dir, FilePattern))
	now := time.Now().Unix()
	out := make([]Entry, 0, len(matches))
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var e Entry
		if err := json.Unmarshal(b, &e); err != nil {
			continue
		}
		if ttl > 0 && now-e.LastSeen > int64(ttl.Seconds()) {
			_ = os.Remove(m) // stale: prune
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt < out[j].StartedAt })
	return out, nil
}

// Find returns the entry whose InstanceID or SessionID matches the given id
// (exact, prefix, or substring). Useful for /session <id> selection where the
// user may paste a fragment of the session or instance id.
func Find(id string) (Entry, bool) {
	entries, err := List(DefaultTTL)
	if err != nil {
		return Entry{}, false
	}
	for _, e := range entries {
		if e.InstanceID == id || e.SessionID == id {
			return e, true
		}
		if len(id) >= 3 && (strings.Contains(e.InstanceID, id) || strings.Contains(e.SessionID, id)) {
			return e, true
		}
	}
	return Entry{}, false
}
