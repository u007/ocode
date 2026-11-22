package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/filelock"
	"github.com/u007/ocode/internal/hook"
	"github.com/u007/ocode/internal/paths"
)

// CredentialKind distinguishes how a credential was obtained.
type CredentialKind string

const (
	KindAPIKey CredentialKind = "api" // matches opencode's "type": "api" convention
	KindOAuth  CredentialKind = "oauth"
)

// Credential is a single saved provider credential.
//
// JSON tags match the opencode auth.json format so both tools can share
// the same file at ~/.local/share/opencode/auth.json.
type Credential struct {
	Kind         CredentialKind `json:"type"`                   // "api_key" or "oauth" — serialised as "type" for opencode compat
	Key          string         `json:"key,omitempty"`          // API key
	AccessToken  string         `json:"access_token,omitempty"` // OAuth
	RefreshToken string         `json:"refresh_token,omitempty"`
	ExpiresAt    int64          `json:"expires_at,omitempty"` // unix seconds
	Account      string         `json:"account,omitempty"`
	BaseURL      string         `json:"baseURL,omitempty"`    // endpoint override
	AccountID    string         `json:"account_id,omitempty"` // Cloudflare
	// CookieAuthToken and CookieCt0 hold the x.com session cookies used by the
	// Grok subscription flow. ocode-only extension; opencode ignores unknown fields.
	CookieAuthToken string `json:"cookie_auth_token,omitempty"`
	CookieCt0       string `json:"cookie_ct0,omitempty"`
}

// credentialJSON handles both ocode (access_token, refresh_token, expires_at,
// account_id) and opencode (access, refresh, expires, accountId) field naming.
type credentialJSON struct {
	Type            string `json:"type"`
	Kind            string `json:"kind"`
	Key             string `json:"key,omitempty"`
	AccessToken     string `json:"access_token,omitempty"`
	RefreshToken    string `json:"refresh_token,omitempty"`
	ExpiresAt       int64  `json:"expires_at,omitempty"`
	Account         string `json:"account,omitempty"`
	BaseURL         string `json:"baseURL,omitempty"`
	BaseURLOld      string `json:"base_url,omitempty"`
	AccountID       string `json:"account_id,omitempty"`
	CookieAuthToken string `json:"cookie_auth_token,omitempty"`
	CookieCt0       string `json:"cookie_ct0,omitempty"`
	// opencode aliases (no underscores, expires in milliseconds)
	AccessOc   string `json:"access,omitempty"`
	RefreshOc  string `json:"refresh,omitempty"`
	ExpiresOc  int64  `json:"expires,omitempty"`
	AccountIDO string `json:"accountId,omitempty"`
}

func normalizeCredentialKind(kind string) CredentialKind {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", string(KindAPIKey), "api_key":
		return KindAPIKey
	case string(KindOAuth):
		return KindOAuth
	default:
		return CredentialKind(kind)
	}
}

func (c *Credential) UnmarshalJSON(data []byte) error {
	var raw credentialJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	kind := raw.Type
	if kind == "" {
		kind = raw.Kind
	}

	// Merge ocode and opencode field names — prefer ocode (underscored) names,
	// fall back to opencode (no-underscore) aliases when the primary is empty.
	accessToken := raw.AccessToken
	if accessToken == "" {
		accessToken = raw.AccessOc
	}
	refreshToken := raw.RefreshToken
	if refreshToken == "" {
		refreshToken = raw.RefreshOc
	}
	expiresAt := raw.ExpiresAt
	if expiresAt == 0 && raw.ExpiresOc != 0 {
		// opencode stores expires in milliseconds; ocode uses seconds.
		expiresAt = raw.ExpiresOc / 1000
	}
	accountID := raw.AccountID
	if accountID == "" {
		accountID = raw.AccountIDO
	}

	*c = Credential{
		Kind:            normalizeCredentialKind(kind),
		Key:             raw.Key,
		AccessToken:     accessToken,
		RefreshToken:    refreshToken,
		ExpiresAt:       expiresAt,
		Account:         raw.Account,
		BaseURL:         raw.BaseURL,
		AccountID:       accountID,
		CookieAuthToken: raw.CookieAuthToken,
		CookieCt0:       raw.CookieCt0,
	}
	if c.BaseURL == "" {
		c.BaseURL = raw.BaseURLOld
	}
	return nil
}

func (c Credential) MarshalJSON() ([]byte, error) {
	return json.Marshal(credentialJSON{
		Type:            string(normalizeCredentialKind(string(c.Kind))),
		Key:             c.Key,
		AccessToken:     c.AccessToken,
		RefreshToken:    c.RefreshToken,
		ExpiresAt:       c.ExpiresAt,
		Account:         c.Account,
		BaseURL:         c.BaseURL,
		AccountID:       c.AccountID,
		CookieAuthToken: c.CookieAuthToken,
		CookieCt0:       c.CookieCt0,
	})
}

var (
	storeMu     sync.Mutex
	cache       map[string]Credential // provider → Credential, as this process last read or wrote it
	cacheLoaded bool                  // true after a successful load
	// cacheInfo is the stat of auth.json that cache was read from (nil when the
	// file was absent). ensureLoadedLocked compares it with the current stat to
	// spot a change made by another process.
	cacheInfo os.FileInfo
	// legacyAuthPath is opencodeLegacyAuthPath behind a variable, so a test can
	// point it at a file that differs from AuthPath on every platform.
	legacyAuthPath = opencodeLegacyAuthPath
	storeLoadErr   errLogOnce
)

// AuthPath returns the opencode-compatible credentials path:
//
//	<GlobalDataDir>/auth.json
//
// See internal/paths.GlobalDataDir for the platform-specific base directory.
func AuthPath() (string, error) {
	base, err := paths.GlobalDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "auth.json"), nil
}

// readLegacyOcodeFormat attempts to parse the legacy ocode auth.json which
// wraps credentials under {"credentials": {…}}. It is the wrapper when
// "credentials" is the SOLE top-level key and its value is a JSON object whose
// values are all JSON objects (an empty object is an empty store). A flat file
// that holds a real provider literally named "credentials" has scalar values
// (fields such as "type" and "key") and is parsed flat, so it returns false with
// no error. A wrapper whose values MIX objects with non-objects is malformed, not
// flat: it returns an error and the caller refuses the file, so a write can never
// drop the object providers. On success it fills dst and returns true. The error
// never quotes file content.
func readLegacyOcodeFormat(data []byte, dst map[string]Credential) (bool, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil || len(top) != 1 {
		return false, nil
	}
	raw, ok := top["credentials"]
	if !ok || !isJSONObject(raw) {
		return false, nil
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(raw, &inner); err != nil {
		return false, nil
	}
	objects, others := 0, 0
	for _, v := range inner {
		if isJSONObject(v) {
			objects++
		} else {
			others++
		}
	}
	if others > 0 && objects > 0 {
		return false, errors.New("legacy credentials wrapper mixes provider objects with non-object values")
	}
	if others > 0 {
		return false, nil
	}
	var legacy map[string]Credential
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return false, nil
	}
	for k, v := range legacy {
		dst[k] = v
	}
	return true, nil
}

// isJSONObject reports whether raw, ignoring surrounding whitespace, starts with
// '{'. Callers have already decoded raw as valid JSON, so that is enough to tell
// an object from null, a string, a number or an array.
func isJSONObject(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// errLogOnce logs an error once per distinct message. A corrupt auth.json seen
// by every Get would otherwise write the same line on every call.
type errLogOnce struct {
	mu   sync.Mutex
	last string
}

func (l *errLogOnce) log(what string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if msg := err.Error(); msg != l.last {
		l.last = msg
		log.Printf("auth: %s: %v", what, err)
	}
}

// LoadStore reads auth.json into memory. Missing file is not an error; an
// absent file is seeded. A reload that changes the credentials bumps
// CredentialVersion, so cached clients rebuild.
func LoadStore() error {
	storeMu.Lock()
	defer storeMu.Unlock()

	return loadFailed(refreshAuthLocked(true))
}

// opencodeLegacyAuthPath returns the path to opencode's own auth.json at
// the XDG data directory (for merging legacy credentials from opencode).
func opencodeLegacyAuthPath() string {
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "opencode", "auth.json")
		}
		return ""
	}
	home, _ := os.UserHomeDir()
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode", "auth.json")
	}
	return filepath.Join(home, ".local", "share", "opencode", "auth.json")
}

// parseAuthData decodes auth.json. A zero-byte or whitespace-only file is an
// empty store. The legacy {"credentials": …} wrapper is recognised first, by
// readLegacyOcodeFormat's strict shape test; anything else is the flat opencode
// format. The error never quotes file content.
func parseAuthData(path string, data []byte) (map[string]Credential, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]Credential{}, nil
	}
	creds := map[string]Credential{}
	wrapped, err := readLegacyOcodeFormat(data, creds)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if wrapped {
		return creds, nil
	}
	flat := map[string]Credential{}
	if err := json.Unmarshal(data, &flat); err != nil {
		return nil, fmt.Errorf("parse %s: unknown format", path)
	}
	if flat == nil {
		flat = map[string]Credential{}
	}
	return flat, nil
}

// readAuthFile reads auth.json as it is on disk now. The stat comes before the
// read, so a write that lands in between leaves the recorded stamp older than
// the data, and the next check reloads rather than missing the change. An
// absent file is (empty map, nil stat, nil error).
func readAuthFile(path string) (map[string]Credential, os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Credential{}, nil, nil
		}
		return nil, nil, fmt.Errorf("stat %s: %w", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Credential{}, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	creds, err := parseAuthData(path, data)
	if err != nil {
		return nil, nil, err
	}
	return creds, info, nil
}

// withAuthFileLock runs fn holding the cross-process lock for path. Every
// writer of auth.json or auth.profiles.json takes it, so a load-modify-write
// cannot interleave with another process's. The lock is not reentrant.
func withAuthFileLock(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create auth dir: %w", err)
	}
	return filelock.WithFileLock(path+".lock", fn)
}

// WithStoreFileLock runs fn holding the same cross-process lock that Set and
// Remove take for auth.json. internal/sync uses it so its read-compare-write of
// auth.json is exclusive against those writers. fn must not call into this
// package's credential store: the lock is not reentrant.
func WithStoreFileLock(fn func() error) error {
	path, err := AuthPath()
	if err != nil {
		return fmt.Errorf("resolve auth path: %w", err)
	}
	return withAuthFileLock(path, fn)
}

// renameAttempts and renameRetryDelay bound the retry around the final rename.
// On Windows a reader that holds auth.json open can make the rename fail with a
// sharing violation for a moment. The same retry runs on every OS, so there is
// one code path to test.
const (
	renameAttempts   = 5
	renameRetryDelay = 20 * time.Millisecond
)

// writeFileAtomic replaces path with data at mode 0600. The bytes go to
// path+".tmp", are fsynced, and are renamed over path. Callers hold the file
// lock, so the shared temp name cannot collide with another writer. The rename
// is retried briefly; the last error is returned if every attempt fails.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := writeSyncedFile(tmp, data); err != nil {
		removeTempFile(tmp)
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	var err error
	for attempt := 1; attempt <= renameAttempts; attempt++ {
		if err = os.Rename(tmp, path); err == nil {
			break
		}
		if attempt < renameAttempts {
			time.Sleep(renameRetryDelay)
		}
	}
	if err != nil {
		removeTempFile(tmp)
		return fmt.Errorf("rename %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func writeSyncedFile(name string, data []byte) (err error) {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	// A temp file left by a crash may carry wider permissions; O_TRUNC keeps them.
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func removeTempFile(tmp string) {
	if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
		log.Printf("auth: remove temp file %s: %v", tmp, err)
	}
}

// writeAuthLocked writes creds to path (caller holds the file lock) and returns
// the stat of the file it just wrote. That stat is the stamp the cache records.
// Taken under the lock, it cannot race another writer.
func writeAuthLocked(path string, creds map[string]Credential) (os.FileInfo, error) {
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal auth: %w", err)
	}
	if err := writeFileAtomic(path, data); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	return info, nil
}

// fillAbsent returns a copy of base plus each entry of extra whose key base
// lacks. base wins, so ocode's own credentials always beat legacy ones.
func fillAbsent(base, extra map[string]Credential) map[string]Credential {
	out := make(map[string]Credential, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	return out
}

// sameAuthFile reports whether two stats describe one unchanged file. Every
// writer renames a new file into place, so SameFile catches a replacement even
// when the size and mtime happen to match.
func sameAuthFile(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// sameAuthPath reports whether two paths name the same file. When both exist
// the answer comes from the file identity, so symlinks and XDG overrides that
// resolve to one file count as the same.
func sameAuthPath(a, b string) bool {
	ai, aerr := os.Stat(a)
	bi, berr := os.Stat(b)
	if aerr == nil && berr == nil {
		return os.SameFile(ai, bi)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// legacySentinelPath is the marker that records opencode's auth.json has been
// folded into ocode's file. It sits beside auth.json.
func legacySentinelPath(path string) string {
	return path + ".legacy-imported"
}

// importLegacyOnce folds opencode's own auth.json into ocode's file, once. It
// runs on a process's first load and does nothing when the legacy path is
// auth.json itself, or when the sentinel already exists. Under the file lock it
// adds each legacy provider that auth.json lacks, writes auth.json atomically,
// and creates the sentinel last. A legacy file that is absent, unreadable or
// unparseable is logged and skipped without a sentinel, so a broken file never
// stops the store from loading. Once the sentinel exists the legacy file is
// never read again: a provider removed from ocode stays removed, and a newer
// credential in auth.json is never replaced by a legacy one. wrote reports
// whether auth.json was written.
// shortcut: a crash between the auth.json write and the sentinel re-imports on the next start, which can bring back a provider removed in that window; upgrade to a single atomic marker inside auth.json if that ever matters.
func importLegacyOnce(path string) (wrote bool, err error) {
	legacy := legacyAuthPath()
	if legacy == "" || sameAuthPath(legacy, path) {
		return false, nil
	}
	sentinel := legacySentinelPath(path)
	if _, serr := os.Stat(sentinel); serr == nil {
		return false, nil
	}
	if _, serr := os.Stat(legacy); serr != nil {
		if !os.IsNotExist(serr) {
			log.Printf("auth: stat legacy opencode credentials %s: %v", legacy, serr)
		}
		return false, nil
	}
	err = withAuthFileLock(path, func() error {
		// Re-check under the lock: another process may have imported meanwhile.
		if _, serr := os.Stat(sentinel); serr == nil {
			return nil
		}
		data, rerr := os.ReadFile(legacy)
		if rerr != nil {
			if !os.IsNotExist(rerr) {
				log.Printf("auth: read legacy opencode credentials %s: %v", legacy, rerr)
			}
			return nil
		}
		// parseAuthData's error never quotes file content, so it is safe to log.
		legacyCreds, perr := parseAuthData(legacy, data)
		if perr != nil {
			log.Printf("auth: legacy opencode credentials %s are not parseable; ignored", legacy)
			return nil
		}
		disk, info, derr := readAuthFile(path)
		if derr != nil {
			return derr
		}
		missing := map[string]Credential{}
		for id, cred := range legacyCreds {
			if _, ok := disk[id]; !ok {
				missing[id] = cred
			}
		}
		if info == nil || len(missing) > 0 {
			if _, werr := writeAuthLocked(path, fillAbsent(disk, missing)); werr != nil {
				return werr
			}
			wrote = true
		}
		if werr := os.WriteFile(sentinel, nil, 0o600); werr != nil {
			return fmt.Errorf("write %s: %w", sentinel, werr)
		}
		return nil
	})
	return wrote, err
}

// seedAuthFile creates an empty auth.json when it is absent. The existence check
// runs under the file lock, so a file another process created in the meantime
// is adopted, never overwritten. wrote reports whether this call created it.
func seedAuthFile(path string) (creds map[string]Credential, info os.FileInfo, wrote bool, err error) {
	err = withAuthFileLock(path, func() error {
		cur, curInfo, rerr := readAuthFile(path)
		if rerr != nil {
			return rerr
		}
		if curInfo != nil {
			creds, info = cur, curInfo
			return nil
		}
		seeded := map[string]Credential{}
		stat, werr := writeAuthLocked(path, seeded)
		if werr != nil {
			return werr
		}
		creds, info, wrote = seeded, stat, true
		return nil
	})
	return creds, info, wrote, err
}

// refreshAuthLocked (caller holds storeMu) reads auth.json into the cache. On the
// first load it runs the once-only legacy import first. With seed true an absent
// file is created; a reload with seed false never writes. A reload that changes
// the content bumps CredentialVersion; a first load never does. Any write made
// here fires OnCredentialsSaved and does not bump the version: see Set for why
// the seed path never bumps.
func refreshAuthLocked(seed bool) error {
	path, err := AuthPath()
	if err != nil {
		return fmt.Errorf("resolve auth path: %w", err)
	}
	wroteImport := false
	if !cacheLoaded {
		wroteImport, err = importLegacyOnce(path)
		if err != nil {
			return err
		}
	}
	disk, info, err := readAuthFile(path)
	if err != nil {
		return err
	}
	wroteSeed := false
	if info == nil && seed {
		disk, info, wroteSeed, err = seedAuthFile(path)
		if err != nil {
			return err
		}
	}
	if wroteImport || wroteSeed {
		OnCredentialsSaved.Fire()
	}
	changed := cacheLoaded && !maps.Equal(cache, disk)
	setCacheLocked(disk, info)
	if changed {
		credentialVersion.Add(1)
	}
	return nil
}

// setCacheLocked replaces the cache with state that was just read from, or
// written to, auth.json. Writers call it without a version bump: they bump
// after the write themselves.
func setCacheLocked(creds map[string]Credential, info os.FileInfo) {
	cache = creds
	cacheInfo = info
	cacheLoaded = true
}

// adoptAuthStateLocked adopts the state a locked load-modify-write returned and
// reports the change. It bumps the credential version when this call wrote, and
// also when the re-read differs from what this process had cached: a peer's write
// that landed between this process's last load and the locked re-read can make
// this call a no-op, and cached clients must still be invalidated. Only a write
// fires OnCredentialsSaved. Caller holds storeMu.
func adoptAuthStateLocked(creds map[string]Credential, info os.FileInfo, wrote bool) {
	changed := cacheLoaded && !maps.Equal(cache, creds)
	setCacheLocked(creds, info)
	if wrote {
		OnCredentialsSaved.Fire()
	}
	if wrote || changed {
		credentialVersion.Add(1)
	}
}

// loadFailed logs a failed load and returns err unchanged. It deliberately leaves
// the cache, its stamp and cacheLoaded alone: the last good state keeps serving
// readers, and because the stamp is unchanged the next call stats and retries.
// Writers still refuse, since they return err from ensureLoadedLocked.
func loadFailed(err error) error {
	if err != nil {
		storeLoadErr.log("load credentials", err)
	}
	return err
}

// readableLocked refreshes the cache and reports whether it holds a loaded state
// to read. A failed reload keeps the last good state, so readers keep seeing it.
// Only a process that has never loaded successfully reads nothing.
func readableLocked() bool {
	_ = ensureLoadedLocked()
	return cacheLoaded
}

// ensureLoadedLocked makes the cache current. The first call loads and seeds.
// Later calls stat auth.json, one call each, and reload only when another
// process changed it.
func ensureLoadedLocked() error {
	if !cacheLoaded {
		return loadFailed(refreshAuthLocked(true))
	}
	path, err := AuthPath()
	if err != nil {
		return loadFailed(fmt.Errorf("resolve auth path: %w", err))
	}
	info, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return loadFailed(fmt.Errorf("stat %s: %w", path, err))
	}
	if err != nil {
		info = nil
	}
	if sameAuthFile(info, cacheInfo) {
		return nil
	}
	return loadFailed(refreshAuthLocked(false))
}

// Get returns the stored credential for a provider, if any.
func Get(provider string) (Credential, bool) {
	storeMu.Lock()
	defer storeMu.Unlock()
	if !readableLocked() {
		return Credential{}, false
	}
	c, ok := cache[provider]
	return c, ok
}

// List returns a copy of all stored credentials keyed by provider.
func List() map[string]Credential {
	storeMu.Lock()
	defer storeMu.Unlock()
	if !readableLocked() {
		return map[string]Credential{}
	}
	out := make(map[string]Credential, len(cache))
	for k, v := range cache {
		out[k] = v
	}
	return out
}

// FindByBaseURL returns the first credential whose BaseURL matches the given
// OpenAI-compatible endpoint, normalized to the same /v1 suffix convention
// used by the OCR/OpenAI compatibility paths. Matching is deterministic: when
// multiple credentials share the same endpoint, provider names are sorted and
// the first match wins.
func FindByBaseURL(baseURL string) (Credential, bool) {
	storeMu.Lock()
	defer storeMu.Unlock()
	if !readableLocked() {
		return Credential{}, false
	}
	target := normalizeBaseURLForMatch(baseURL)
	if target == "" {
		return Credential{}, false
	}
	names := make([]string, 0, len(cache))
	for name := range cache {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cred := cache[name]
		if normalizeBaseURLForMatch(cred.BaseURL) == target {
			return cred, true
		}
	}
	return Credential{}, false
}

// ResolveOpenAICompatKey returns the Bearer token to use for an
// OpenAI-compatible endpoint (e.g. the OCR openai-compat backend), resolving it
// in priority order:
//
//  1. explicitKey, when the caller already has one configured;
//  2. a stored credential whose base URL matches baseURL;
//  3. when isLMStudio is true, the credential stored under the "lmstudio"
//     provider name.
//
// Step 3 is the important fallback: LM Studio credentials are commonly saved by
// provider name with no base_url field, so a base-URL match alone misses them
// and the request goes out unauthenticated (HTTP 401), which surfaces to the
// user as an empty OCR model list. Returns "" when nothing resolves.
func ResolveOpenAICompatKey(explicitKey, baseURL string, isLMStudio bool) string {
	if explicitKey != "" {
		return explicitKey
	}
	if cred, ok := FindByBaseURL(baseURL); ok && cred.Key != "" {
		return cred.Key
	}
	if isLMStudio {
		if cred, ok := Get("lmstudio"); ok && cred.Key != "" {
			return cred.Key
		}
	}
	return ""
}

func normalizeBaseURLForMatch(baseURL string) string {
	baseURL = strings.TrimSpace(strings.TrimRight(baseURL, "/"))
	if baseURL == "" {
		return ""
	}
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}
	return baseURL
}

// sameCredential reports whether two credentials are stored identically. The kind
// is compared after normalisation, as the file stores it.
func sameCredential(a, b Credential) bool {
	a.Kind = normalizeCredentialKind(string(a.Kind))
	b.Kind = normalizeCredentialKind(string(b.Kind))
	return a == b
}

// commitAuthLocked is one load-modify-write of auth.json under the file lock. It
// re-reads the disk, applies mutate to that copy, and writes only when mutate
// reports a change. It returns the state now on disk, the stat of that file, and
// whether it wrote. The caller owns the cache. Nothing is written when the read
// or the parse fails, or when nothing changed.
func commitAuthLocked(path string, mutate func(map[string]Credential) bool) (map[string]Credential, os.FileInfo, bool, error) {
	var creds map[string]Credential
	var info os.FileInfo
	wrote := false
	err := withAuthFileLock(path, func() error {
		disk, cur, rerr := readAuthFile(path)
		if rerr != nil {
			return rerr
		}
		if !mutate(disk) {
			creds, info = disk, cur
			return nil
		}
		stat, werr := writeAuthLocked(path, disk)
		if werr != nil {
			return werr
		}
		creds, info, wrote = disk, stat, true
		return nil
	})
	return creds, info, wrote, err
}

// Set writes a credential for a provider, persisting to disk at 0600. The write
// is a load-modify-write under the cross-process lock, so a key another process
// saved since this one loaded is kept. Storing a credential identical to the one
// on disk writes nothing, fires no hook and bumps no version.
func Set(provider string, cred Credential) error {
	storeMu.Lock()
	defer storeMu.Unlock()
	if err := ensureLoadedLocked(); err != nil {
		return err
	}
	path, err := AuthPath()
	if err != nil {
		return fmt.Errorf("resolve auth path: %w", err)
	}
	creds, info, wrote, err := commitAuthLocked(path, func(m map[string]Credential) bool {
		if prev, ok := m[provider]; ok && sameCredential(prev, cred) {
			return false
		}
		m[provider] = cred
		return true
	})
	if err != nil {
		return err
	}
	// Fire and bump only on a real mutation, after the new credential is durably
	// readable. The bump is not in the write path: that path also runs on the
	// seed-on-load write, which materialises an empty auth.json without changing
	// any credential, and bumping there would invalidate every agent for nothing.
	adoptAuthStateLocked(creds, info, wrote)
	return nil
}

// Remove deletes a credential. Like Set it is a locked load-modify-write. Removing
// a provider that is not stored writes nothing, fires no hook and bumps no
// version.
func Remove(provider string) error {
	storeMu.Lock()
	defer storeMu.Unlock()
	if err := ensureLoadedLocked(); err != nil {
		return err
	}
	path, err := AuthPath()
	if err != nil {
		return fmt.Errorf("resolve auth path: %w", err)
	}
	creds, info, wrote, err := commitAuthLocked(path, func(m map[string]Credential) bool {
		if _, ok := m[provider]; !ok {
			return false
		}
		delete(m, provider)
		return true
	})
	if err != nil {
		return err
	}
	// Removing a key must invalidate a cached client too, or the session keeps
	// authenticating with a credential the user deleted. See Set for why this
	// is not bumped inside the write path.
	adoptAuthStateLocked(creds, info, wrote)
	return nil
}

// OnCredentialsSaved is invoked after every successful auth.json
// write. Clients register callbacks with OnCredentialsSaved.Add;
// the zero value is ready to use. Used by internal/sync to debounce
// background push. Listeners must not block: they run under storeMu.
var OnCredentialsSaved hook.Hooks

// credentialVersion counts every credential mutation across BOTH stores (the
// base auth.json here and the per-profile overlays in profile_store.go). It is
// a single counter, deliberately not two summed: summing two monotonic
// counters can collide, and a collision is a missed invalidation.
//
// Callers that cache something built from credentials — an LLM client resolves
// its key at construction (agent.NewClientWithProfile) — snapshot this at build
// time and compare it later to notice an in-place edit that a name comparison
// would miss. internal/server.reconcileProfileAgent is that caller.
var credentialVersion atomic.Int64

// CredentialVersion returns the counter bumped on every credential mutation. It
// first runs the cheap stat-and-reload check on each store that has loaded, so a
// change another process made is counted even when no getter has run since. The
// two stores are checked one after the other under their own mutexes, never
// nested. A store that has never loaded is skipped: it has nothing cached to
// invalidate, and loading it here would seed or import files from a getter-less
// call. Callers must not hold storeMu or profileStoreMu, and no
// OnCredentialsSaved listener may call this: listeners run under those mutexes.
func CredentialVersion() int64 {
	storeMu.Lock()
	if cacheLoaded {
		_ = ensureLoadedLocked()
	}
	storeMu.Unlock()

	profileStoreMu.Lock()
	if profileLoaded {
		_ = ensureProfileStoreLocked()
	}
	profileStoreMu.Unlock()
	return credentialVersion.Load()
}

// SetCredentialVersionForTest pins the credential version. It exists only so a
// test that must mutate the process-global store (which bumps the version on
// every write) can restore it afterwards; a leaked bump makes every agent
// cached with an older snapshot look stale and rebuild unexpectedly.
func SetCredentialVersionForTest(v int64) {
	credentialVersion.Store(v)
}
