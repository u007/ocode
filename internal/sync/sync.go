// Package sync provides encrypted config sync between ocode machines via
// the kakiit server. It implements device-code login, debounced background
// push, startup pull-merge, and a 3-way JSON merge with per-blob-type
// conflict policy.
package sync

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/paths"
)

// BaseURLSource describes how a sync client's base URL was chosen. It is
// resolved once at the same time as the URL so the diagnostic never re-reads
// the environment and never reports a stale source.
type BaseURLSource int

const (
	// BaseURLSourceDefault means no explicit override was set: neither
	// config.SyncURL nor OCODE_SYNC_URL. The resolved URL is the
	// DefaultBaseURL production default (https://hub.mercstudio.com as of the
	// backend/sync split — previously http://localhost:3201). This is the case
	// the one-time warning is meant to surface, because an unconfigured local
	// kakiit dev machine now reaches production.
	BaseURLSourceDefault BaseURLSource = iota
	// BaseURLSourceEnv means OCODE_SYNC_URL was set and config.SyncURL was not.
	BaseURLSourceEnv
	// BaseURLSourceConfig means an explicit per-machine config.SyncURL override
	// was set (via Settings > Backend > Sync server or ocodeconfig.json).
	BaseURLSourceConfig
)

// baseURLNoticeOnce guards the one-time diagnostic. It is reset between tests
// via resetBaseURLNoticeTestOnly so test cases are order-independent.
var baseURLNoticeOnce sync.Once

// resetBaseURLNoticeTestOnly resets the once-guard so tests can exercise each
// source branch independently. It is ONLY for tests; production code never
// calls it.
func resetBaseURLNoticeTestOnly() { baseURLNoticeOnce = sync.Once{} }

// LogBaseURLNotice emits a one-time diagnostic describing how the sync
// client's base URL was resolved. Call it once per process at client
// construction (NOT from read-only status/config inspection endpoints). The
// warning branch fires only for BaseURLSourceDefault, so an unconfigured
// local kakiit dev machine that silently started reaching production after
// the DefaultBaseURL flip gets a visible signal pointing at the two override
// knobs (config.SyncURL, OCODE_SYNC_URL).
func LogBaseURLNotice(resolved string, src BaseURLSource) {
	baseURLNoticeOnce.Do(func() {
		switch src {
		case BaseURLSourceConfig:
			log.Printf("sync: using explicitly configured sync_url %q (config.SyncURL)", resolved)
		case BaseURLSourceEnv:
			log.Printf("sync: using sync server %q from OCODE_SYNC_URL env (no config.SyncURL set)", resolved)
		case BaseURLSourceDefault:
			// No explicit override at all — resolved is the post-flip
			// production default. Surface prominently: an unconfigured
			// local kakiit dev now reaches production. See AGENTS.md
			// "Backend / Sync URL Split" and CHANGES.md [Unreleased].
			log.Printf("sync: WARNING no sync_url configured and OCODE_SYNC_URL unset — using production default %q. "+
				"If you run a local kakiit dev server, set sync_url (Settings > Backend > Sync server, "+
				"or PUT /api/config/ocode/sync-url) or OCODE_SYNC_URL to point at it. "+
				"See AGENTS.md \"Backend / Sync URL Split\" / CHANGES.md [Unreleased].", resolved)
		}
	})
}

// DefaultBaseURL returns the kakiit sync server URL: OCODE_SYNC_URL if set,
// otherwise the production hub. Shared by every caller of NewClient (TUI,
// web/desktop server) so they all resolve the same override.
func DefaultBaseURL() string {
	if v := os.Getenv("OCODE_SYNC_URL"); v != "" {
		return v
	}
	return "https://hub.mercstudio.com"
}

// ResolveBaseURL layers the sync server URL: an explicit per-machine
// override (config.OcodeConfig.SyncURL, settable from the web UI or
// ocodeconfig.json) wins, then DefaultBaseURL's OCODE_SYNC_URL/production
// fallback. configured should already be normalized (config.NormalizeSyncURL).
func ResolveBaseURL(configured string) string {
	if configured != "" {
		return configured
	}
	return DefaultBaseURL()
}

// ResolveBaseURLWithSource is like ResolveBaseURL but also returns the
// baseURLSource describing how the URL was chosen. Resolve the source here,
// at the same time as the URL, so the diagnostic never re-reads the
// environment and never reports a stale source. configured should already be
// normalized (config.NormalizeSyncURL).
func ResolveBaseURLWithSource(configured string) (string, BaseURLSource) {
	if configured != "" {
		return configured, BaseURLSourceConfig
	}
	if v := os.Getenv("OCODE_SYNC_URL"); v != "" {
		return v, BaseURLSourceEnv
	}
	return "https://hub.mercstudio.com", BaseURLSourceDefault
}

// BlobType identifies which config file is being synced.
type BlobType string

const (
	BlobTypeConfig BlobType = "ocodeconfig"
	BlobTypeAuth   BlobType = "authsecrets"
)

// syncDir returns the ocode sync data directory (~/.local/share/opencode/sync).
func syncDir() (string, error) {
	base, err := paths.GlobalDataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "sync")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// SnapshotPath returns the path to the local snapshot file for the given blob
// type. The snapshot tracks the last-known server version to detect conflicts.
func SnapshotPath(blob BlobType) (string, error) {
	dir, err := syncDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, string(blob)+".snapshot.json"), nil
}

// TokenPath returns the path to the stored sync bearer token file.
func TokenPath() (string, error) {
	dir, err := syncDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "token"), nil
}

// writeLocalConfigFileAtomic writes a merged blob to a local config file
// (ocodeconfig.json or auth.json) via write-tmp-then-rename, matching
// internal/auth.persistLocked's own atomicity. auth.json in particular can
// be written concurrently by a separate opencode CLI installation sharing
// the same data directory (see internal/auth's opencodeLegacyAuthPath) — a
// plain os.WriteFile could leave that file (or ours) reading a torn write.
func writeLocalConfigFileAtomic(path string, data []byte) error {
	tmp := path + ".sync-tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// blobLocks serializes sync's own merge-write sequences per blob type: Push,
// Pull, and the watcher all funnel through blobLock, so two sync operations
// can never interleave their re-read/compare/replace steps. It does not (and
// cannot — config/auth writes go through their own packages) lock out
// concurrent user edits via config.Save*/auth.Set: those are handled by
// compare-before-write in writeMergedLocal, which re-reads the file under
// this lock and aborts instead of overwriting a newer local edit.
var blobLocks = map[BlobType]*sync.Mutex{
	BlobTypeConfig: {},
	BlobTypeAuth:   {},
}

func blobLock(blob BlobType) *sync.Mutex {
	if mu, ok := blobLocks[blob]; ok {
		return mu
	}
	// Unknown blob type: fall back to the config stripe rather than
	// introducing an unsynchronized path.
	return blobLocks[BlobTypeConfig]
}

// normalizeLocalJSON treats a missing/empty local file as `{}` so the
// compare-before-write below sees a stable canonical form.
func normalizeLocalJSON(raw []byte) string {
	if len(raw) == 0 {
		return `{}`
	}
	return string(raw)
}

// writeMergedLocal replaces the local config file with a merge result, but
// only when the file still holds exactly the content the merge was computed
// from. A local config/auth edit that landed while the network round-trip
// was in flight (or while another sync op ran) aborts this write instead of
// overwriting it: the caller re-merges against the fresh content on its next
// cycle. The atomic rename prevents torn files; this check prevents lost
// updates. Reports whether the write happened.
func writeMergedLocal(blob BlobType, localPath string, mergeBase json.RawMessage, merged []byte) (wrote bool, err error) {
	mu := blobLock(blob)
	mu.Lock()
	defer mu.Unlock()
	return writeMergedLocalLocked(localPath, mergeBase, merged)
}

// writeMergedLocalLocked is writeMergedLocal for callers that already hold
// blobLock(blob).
func writeMergedLocalLocked(localPath string, mergeBase json.RawMessage, merged []byte) (bool, error) {
	current, err := readLocalConfigFile(localPath)
	if err != nil {
		return false, err
	}
	if normalizeLocalJSON(current) != normalizeLocalJSON(mergeBase) {
		// The file moved under us — do not overwrite the newer edit.
		return false, nil
	}
	if err := writeLocalConfigFileAtomic(localPath, merged); err != nil {
		return false, err
	}
	return true, nil
}

// localConfigPathFor returns the path to the actual local config file that
// corresponds to the given blob type.
func localConfigPathFor(blob BlobType) (string, error) {
	switch blob {
	case BlobTypeConfig:
		return config.ActiveOcodeConfigPath()
	case BlobTypeAuth:
		return auth.AuthPath()
	default:
		return "", fmt.Errorf("unknown blob type: %s", blob)
	}
}

// SaveToken persists the sync bearer token at 0600 permissions.
func SaveToken(token string) error {
	path, err := TokenPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token), 0600)
}

// LoadToken reads the stored sync bearer token. A missing file is not an
// error — it means the user hasn't logged in.
func LoadToken() (string, bool, error) {
	path, err := TokenPath()
	if err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

// ClearToken deletes the locally stored sync bearer token. A missing file
// is not an error — logout is idempotent.
func ClearToken() error {
	path, err := TokenPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
