package remote

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/paths"
)

// SyncFileDir selects which remote directory a synced file is written
// under, mirroring the local resolver so the remote layout matches.
type SyncFileDir string

const (
	SyncDirOcodeData    SyncFileDir = "ocode-data"    // remote paths.OcodeGlobalDataDir()
	SyncDirConfig       SyncFileDir = "config"        // remote config.GlobalConfigDir()
	SyncDirOpencodeData SyncFileDir = "opencode-data" // remote paths.GlobalDataDir() (auth.json, mcp-auth.json)
	SyncDirHome         SyncFileDir = "home"          // remote $HOME; Name is an allowlisted relative path (sync_home.go)
)

// SyncFile is one file carried in a sync payload.
type SyncFile struct {
	Dir     SyncFileDir `json:"dir"`
	Name    string      `json:"name"`
	Content []byte      `json:"content"`
	// Mode carries the source permission bits for SyncDirHome entries only (the
	// owner-exec bit is preserved); other dirs always write 0600.
	Mode uint32 `json:"mode,omitempty"`
}

// SyncPayload is the credential/config bundle pushed to a remote on
// connect: auth profiles (the remote TUI is useless without keys) plus the
// core model config files. Secrets never leave this struct's json.Marshal
// output — never argv, never a log line.
type SyncPayload struct {
	Files []SyncFile `json:"files"`
	// KeepHome lists SyncDirHome paths that exist locally but were left out of
	// Files because they are credential files unchanged since the last push
	// (see gateCredentialFiles). The remote counts them as still managed so the
	// stale-file sweep does not delete them.
	KeepHome []string `json:"keep_home,omitempty"`
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

	if dataDir, err := paths.GlobalDataDir(); err == nil {
		for _, name := range []string{"auth.json", "mcp-auth.json"} {
			if f, ok, err := readSyncFile(SyncDirOpencodeData, filepath.Join(dataDir, name)); err != nil {
				return SyncPayload{}, err
			} else if ok {
				p.Files = append(p.Files, f)
			}
		}
	}

	if cfgDir, err := config.GlobalConfigDir(); err == nil {
		for _, name := range []string{"opencode.json", "ocodeconfig.json"} {
			if f, ok, err := readSyncFile(SyncDirConfig, filepath.Join(cfgDir, name)); err != nil {
				return SyncPayload{}, err
			} else if ok {
				f.Content = stripMachineLocalConfig(f.Name, f.Content)
				p.Files = append(p.Files, f)
			}
		}
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		files, err := collectHomeSyncFiles(home)
		if err != nil {
			return SyncPayload{}, err
		}
		p.Files = append(p.Files, files...)
	}

	return p, nil
}

// machineLocalConfigKeys maps a synced config file to the keys that must NOT
// cross machines because they describe the local host rather than the user.
//
// This is the second half of the remote-shell fix: `terminal_shell` is an
// absolute path to a shell binary on the *local* machine. Pushing it verbatim
// gives the remote a config value it cannot satisfy, and any remote process
// that honors the override (the interactive terminal, `!` commands) then fails
// with `fork/exec /bin/zsh: no such file or directory`. The remote resolves its
// own shell instead (remote.ShellProbeCommand / remote.LoginShellScript), so the key
// is dropped rather than translated.
//
// `extra_allowed_paths` is the same class of value: absolute paths that exist on
// ONE machine. Pushing them is not merely useless on the other host (macOS
// `/Users/...` entries on a Linux box match nothing) — because WritePayload
// replaces the whole file, it silently REVOKES every path grant the user made on
// that host, so a remote session asks forever no matter how often the path is
// re-granted there. WritePayload therefore re-attaches the destination's own
// value (preserveMachineLocalConfig) so the key is owned by the machine it
// describes.
var machineLocalConfigKeys = map[string][]string{
	"ocodeconfig.json": {"terminal_shell", "extra_allowed_paths"},
}

// stripMachineLocalConfig removes machineLocalConfigKeys[name] from a synced
// config file. It preserves the rest of the document byte-for-byte for the
// common case of nothing to strip, and preserves all other keys when it does
// rewrite. A parse failure returns the original content unchanged: a config
// that does not round-trip through JSON is still a valid credential payload
// component (other code reads it leniently), so failing the sync over it would
// be a regression.
func stripMachineLocalConfig(name string, content []byte) []byte {
	keys := machineLocalConfigKeys[name]
	if len(keys) == 0 || len(bytes.TrimSpace(content)) == 0 {
		return content
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(content, &doc); err != nil {
		return content
	}
	stripped := false
	for _, k := range keys {
		if _, ok := doc[k]; ok {
			delete(doc, k)
			stripped = true
		}
	}
	if !stripped {
		return content
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return content
	}
	return append(out, '\n')
}

// preserveMachineLocalConfig re-attaches the machine-local keys that
// stripMachineLocalConfig removed on the sender, taking their values from the
// destination file that is about to be replaced. Without this, a stripped key
// would be ERASED on the receiver (WritePayload writes the frame's bytes as the
// whole file), which is worse than the original bug: the host would lose its own
// `extra_allowed_paths` grants and ask for paths the user already approved.
//
// The destination wins for these keys because it is the only machine that can
// evaluate them. A missing destination (fresh host) leaves the content untouched;
// an unparseable destination does too — failing the credential sync over a
// foreign config is not worth it, and the previous behaviour (write as-is) is
// the fallback.
func preserveMachineLocalConfig(name string, content []byte, destPath string) []byte {
	keys := machineLocalConfigKeys[name]
	if len(keys) == 0 || len(bytes.TrimSpace(content)) == 0 {
		return content
	}
	existing, err := os.ReadFile(destPath)
	if err != nil {
		// A missing destination is the normal fresh-host case: nothing to
		// preserve, so no log line. Any other read error is worth surfacing.
		if !os.IsNotExist(err) {
			log.Printf("sync: preserve machine-local keys: read %s: %v (writing frame as-is)", destPath, err)
		}
		return content
	}
	if len(bytes.TrimSpace(existing)) == 0 {
		return content
	}
	var incoming, current map[string]json.RawMessage
	if err := json.Unmarshal(content, &incoming); err != nil {
		log.Printf("sync: preserve machine-local keys: frame content is not a JSON object: %v (writing as-is)", err)
		return content
	}
	if err := json.Unmarshal(existing, &current); err != nil {
		log.Printf("sync: preserve machine-local keys: destination %s is not a JSON object: %v (writing frame as-is)", destPath, err)
		return content
	}
	added := false
	for _, k := range keys {
		if _, ok := incoming[k]; ok {
			continue // the frame carries a value; never overwrite it here
		}
		v, ok := current[k]
		if !ok {
			continue
		}
		incoming[k] = v
		added = true
	}
	if !added {
		return content
	}
	out, err := json.MarshalIndent(incoming, "", "  ")
	if err != nil {
		log.Printf("sync: preserve machine-local keys: re-marshal failed: %v (writing frame as-is)", err)
		return content
	}
	return append(out, '\n')
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
		if f.Dir == SyncDirHome && !allowedHomeSyncPath(f.Name) {
			return fmt.Errorf("refusing to write %q under $HOME: not in the sync allowlist", f.Name)
		}
		destDir, err := resolveSyncDir(f.Dir)
		if err != nil {
			return fmt.Errorf("resolve dest dir for %s: %w", f.Name, err)
		}
		destPath := filepath.Join(destDir, filepath.FromSlash(f.Name))
		if f.Dir == SyncDirHome {
			if err := removeLinkedParents(destDir, destPath); err != nil {
				return fmt.Errorf("replace symlink under %s: %w", destDir, err)
			}
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0o700); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(destPath), err)
		}
		mode := os.FileMode(0o600)
		if f.Dir == SyncDirHome {
			mode = homeFileMode(f.Mode)
		}
		content := f.Content
		if f.Dir == SyncDirConfig {
			content = preserveMachineLocalConfig(f.Name, f.Content, destPath)
		}
		if err := writeFileMode(destPath, content, mode); err != nil {
			return fmt.Errorf("write %s: %w", destPath, err)
		}
	}
	return applyHomeSyncManifest(p)
}

// removeLinkedParents removes any symlink among the directories between root
// and destPath. The sender follows links and ships the target's content under
// the link's path, so the real directory supersedes the link; a stale or
// dangling link (e.g. one pointing at the sender's /Users/... path) would
// otherwise make MkdirAll fail with "file exists". Only the link is removed,
// never its target.
func removeLinkedParents(root, destPath string) error {
	rel, err := filepath.Rel(root, filepath.Dir(destPath))
	if err != nil {
		return err
	}
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(cur); err != nil {
				return err
			}
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
	case SyncDirOpencodeData:
		return paths.GlobalDataDir()
	case SyncDirConfig:
		return config.GlobalConfigDir()
	case SyncDirHome:
		return os.UserHomeDir()
	default:
		return "", fmt.Errorf("unknown sync file dir %q", d)
	}
}

// writeFile0600 writes data to path via temp-file + fsync + atomic rename,
// then enforces 0600 — a partial write (crash mid-write, disk full) leaves
// either the old file or nothing, never a half-written destination.
func writeFile0600(path string, data []byte) error {
	return writeFileMode(path, data, 0o600)
}

// writeFileMode is writeFile0600 with an explicit final mode (0600 or 0700).
func writeFileMode(path string, data []byte, mode os.FileMode) error {
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
	if err := os.Chmod(tmpPath, mode); err != nil {
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
