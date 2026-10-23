package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/u007/ocode/internal/paths"
)

// Credential files are pushed only when their own local content changed since
// the last successful push to that host. OAuth refresh tokens rotate: if the
// remote refreshed one and the next re-push (triggered by an unrelated config
// edit) overwrote it with the older local copy, that login would break. Gating
// per file lets a remote-rotated token survive until the local file changes
// (e.g. a fresh local login) or the user forces a sync.

func isCredentialSyncFile(f SyncFile) bool {
	switch f.Dir {
	case SyncDirOcodeData:
		return f.Name == "auth.profiles.json"
	case SyncDirOpencodeData:
		return f.Name == "auth.json" || f.Name == "mcp-auth.json"
	case SyncDirHome:
		return f.Name == ".config/opencode/antigravity-accounts.json"
	}
	return false
}

func credentialKey(f SyncFile) string { return string(f.Dir) + "/" + f.Name }

func contentHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// gateCredentialFiles returns p without the credential files whose content hash
// equals cached (unless force), plus the hashes of the credential files it kept
// so the caller can record them after a successful push. Skipped home
// credential files are listed in KeepHome so the remote does not sweep them.
func gateCredentialFiles(p SyncPayload, cached map[string]string, force bool) (SyncPayload, map[string]string) {
	out := SyncPayload{KeepHome: append([]string(nil), p.KeepHome...)}
	sent := map[string]string{}
	for _, f := range p.Files {
		if !isCredentialSyncFile(f) {
			out.Files = append(out.Files, f)
			continue
		}
		h := contentHash(f.Content)
		if !force && cached[credentialKey(f)] == h {
			if f.Dir == SyncDirHome {
				out.KeepHome = append(out.KeepHome, f.Name)
			}
			continue
		}
		out.Files = append(out.Files, f)
		sent[credentialKey(f)] = h
	}
	return out, sent
}

const syncCredCacheFile = "remote-sync-creds.json"

func syncCredCachePath() (string, error) {
	base, err := paths.OcodeGlobalDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, syncCredCacheFile), nil
}

func loadCredCache() (map[string]map[string]string, error) {
	path, err := syncCredCachePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]map[string]string{}, nil
		}
		return nil, err
	}
	var m map[string]map[string]string
	if err := json.Unmarshal(data, &m); err != nil || m == nil {
		// Corrupt cache: treat as empty so every credential is re-pushed once.
		return map[string]map[string]string{}, nil
	}
	return m, nil
}

// CachedCredentialHashes returns the per-file hashes last pushed to host.
func CachedCredentialHashes(host string) map[string]string {
	m, err := loadCredCache()
	if err != nil {
		return nil
	}
	return m[host]
}

// RecordCredentialHashes merges sent into host's recorded per-file hashes.
func RecordCredentialHashes(host string, sent map[string]string) error {
	if len(sent) == 0 {
		return nil
	}
	m, err := loadCredCache()
	if err != nil {
		m = map[string]map[string]string{}
	}
	if m[host] == nil {
		m[host] = map[string]string{}
	}
	for k, v := range sent {
		m[host][k] = v
	}
	path, err := syncCredCachePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFile0600(path, data)
}
