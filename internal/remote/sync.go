package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/paths"
)

// SyncFileDir selects which remote directory a synced file is written
// under, mirroring the local resolver so the remote layout matches.
type SyncFileDir string

const (
	SyncDirOcodeData SyncFileDir = "ocode-data" // remote paths.OcodeGlobalDataDir()
	SyncDirConfig    SyncFileDir = "config"     // remote config.GlobalConfigDir()
)

// SyncFile is one file carried in a sync payload.
type SyncFile struct {
	Dir     SyncFileDir `json:"dir"`
	Name    string      `json:"name"`
	Content []byte      `json:"content"`
}

// SyncPayload is the credential/config bundle pushed to a remote on
// connect: auth profiles (the remote TUI is useless without keys) plus the
// core model config files. Secrets never leave this struct's json.Marshal
// output — never argv, never a log line.
type SyncPayload struct {
	Files []SyncFile `json:"files"`
}

// syncFrame is the wire format piped to `remote-receive-config` on stdin:
// framed JSON carrying a version tag and a checksum the remote verifies
// before trusting Payload at all.
type syncFrame struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Payload []byte `json:"payload"` // json.Marshal of SyncPayload
}

// maxSyncPayloadBytes bounds the frame the remote will accept, rejecting an
// oversized payload before it's ever unmarshaled.
const maxSyncPayloadBytes = 32 << 20 // 32 MiB

// BuildSyncPayload reads the local auth-profile store and the two global
// config files (opencode.json, ocodeconfig.json), skipping any that don't
// exist — a fresh machine with no saved profiles yet is not an error, the
// sync stage just carries fewer files.
func BuildSyncPayload() (SyncPayload, error) {
	var p SyncPayload

	if authPath, err := ocodeAuthProfilesPath(); err == nil {
		if f, ok, err := readSyncFile(SyncDirOcodeData, authPath); err != nil {
			return SyncPayload{}, err
		} else if ok {
			p.Files = append(p.Files, f)
		}
	}

	if cfgDir, err := config.GlobalConfigDir(); err == nil {
		for _, name := range []string{"opencode.json", "ocodeconfig.json"} {
			if f, ok, err := readSyncFile(SyncDirConfig, filepath.Join(cfgDir, name)); err != nil {
				return SyncPayload{}, err
			} else if ok {
				p.Files = append(p.Files, f)
			}
		}
	}

	return p, nil
}

func ocodeAuthProfilesPath() (string, error) {
	base, err := paths.OcodeGlobalDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "auth.profiles.json"), nil
}

func readSyncFile(dir SyncFileDir, path string) (SyncFile, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return SyncFile{}, false, nil
		}
		return SyncFile{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	return SyncFile{Dir: dir, Name: filepath.Base(path), Content: content}, true, nil
}

// EncodeFrame marshals a payload into the framed wire format, computing its
// checksum over the marshaled payload bytes.
func EncodeFrame(ver string, p SyncPayload) ([]byte, error) {
	payloadBytes, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal sync payload: %w", err)
	}
	sum := sha256.Sum256(payloadBytes)
	frame := syncFrame{Version: ver, SHA256: hex.EncodeToString(sum[:]), Payload: payloadBytes}
	out, err := json.Marshal(frame)
	if err != nil {
		return nil, fmt.Errorf("marshal sync frame: %w", err)
	}
	return out, nil
}

// DecodeAndVerifyFrame parses and checksum-verifies a wire-format frame.
// Any framing error (malformed JSON, oversized input) or checksum mismatch
// is rejected outright — this is the mandatory security boundary on the
// remote-receive-config side.
func DecodeAndVerifyFrame(data []byte) (ver string, payload SyncPayload, err error) {
	if len(data) == 0 {
		return "", SyncPayload{}, fmt.Errorf("empty sync payload")
	}
	if len(data) > maxSyncPayloadBytes {
		return "", SyncPayload{}, fmt.Errorf("sync payload too large: %d bytes (max %d)", len(data), maxSyncPayloadBytes)
	}
	var frame syncFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return "", SyncPayload{}, fmt.Errorf("malformed sync frame: %w", err)
	}
	if frame.SHA256 == "" || frame.Version == "" {
		return "", SyncPayload{}, fmt.Errorf("malformed sync frame: missing version or sha256")
	}
	sum := sha256.Sum256(frame.Payload)
	got := hex.EncodeToString(sum[:])
	if got != frame.SHA256 {
		return "", SyncPayload{}, fmt.Errorf("sync payload checksum mismatch: got %s, frame claims %s", got, frame.SHA256)
	}
	var p SyncPayload
	if err := json.Unmarshal(frame.Payload, &p); err != nil {
		return "", SyncPayload{}, fmt.Errorf("malformed sync payload: %w", err)
	}
	return frame.Version, p, nil
}

// WritePayload writes every file in p to its resolved local destination
// (this runs on the remote, inside `remote-receive-config`, but is
// transport-agnostic so it's testable without ssh) with 0600 permissions
// via temp-file + rename — the same protection profile as
// internal/auth/profile_store.go's writeProfileStoreLocked.
func WritePayload(p SyncPayload) error {
	for _, f := range p.Files {
		destDir, err := resolveSyncDir(f.Dir)
		if err != nil {
			return fmt.Errorf("resolve dest dir for %s: %w", f.Name, err)
		}
		if err := os.MkdirAll(destDir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", destDir, err)
		}
		destPath := filepath.Join(destDir, f.Name)
		if err := writeFile0600(destPath, f.Content); err != nil {
			return fmt.Errorf("write %s: %w", destPath, err)
		}
	}
	return nil
}

// resolveSyncDirFn is a package var (not a plain function) so tests can
// redirect WritePayload at a temp directory without touching the real
// per-user global config/data dirs.
var resolveSyncDirFn = defaultResolveSyncDir

func resolveSyncDir(d SyncFileDir) (string, error) {
	return resolveSyncDirFn(d)
}

func defaultResolveSyncDir(d SyncFileDir) (string, error) {
	switch d {
	case SyncDirOcodeData:
		return paths.OcodeGlobalDataDir()
	case SyncDirConfig:
		return config.GlobalConfigDir()
	default:
		return "", fmt.Errorf("unknown sync file dir %q", d)
	}
}

// writeFile0600 writes data to path via temp-file + fsync + atomic rename,
// then enforces 0600 — a partial write (crash mid-write, disk full) leaves
// either the old file or nothing, never a half-written destination.
func writeFile0600(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() { os.Remove(tmpPath) }

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// --- per-host hash cache (skip-if-unchanged) ---

// syncCacheFile lives at <OcodeGlobalDataDir>/remote-sync.json, mapping a
// Target's canonical string (see Target.String) to the sha256 of the last
// payload successfully pushed to it.
const syncCacheFile = "remote-sync.json"

func syncCachePath() (string, error) {
	base, err := paths.OcodeGlobalDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, syncCacheFile), nil
}

func loadSyncCache() (map[string]string, error) {
	path, err := syncCachePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		// A corrupt cache is not fatal to a connect — treat as empty so the
		// next successful sync rewrites it.
		return map[string]string{}, nil
	}
	if m == nil {
		m = map[string]string{}
	}
	return m, nil
}

func saveSyncCache(m map[string]string) error {
	path, err := syncCachePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFile0600(path, data)
}

// CachedHash returns the last-pushed payload hash for host, if any.
func CachedHash(host string) (string, bool) {
	m, err := loadSyncCache()
	if err != nil {
		return "", false
	}
	h, ok := m[host]
	return h, ok
}

// SetCachedHash records host's last-pushed payload hash.
func SetCachedHash(host, hash string) error {
	m, err := loadSyncCache()
	if err != nil {
		m = map[string]string{}
	}
	m[host] = hash
	return saveSyncCache(m)
}

// PayloadHash returns the sha256 hex digest of p's canonical (marshaled)
// form, used both as the sync-frame checksum and the skip-if-unchanged
// cache key.
func PayloadHash(p SyncPayload) (string, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
