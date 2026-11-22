package boxsession

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/filelock"
	"github.com/u007/ocode/internal/paths"
)

// A login state is a named set of cookies and site storage saved from a box
// browser's shared context, kept on this machine. It is restored into the next
// box started with the same name. Files are owner-only. The data never enters a
// provider payload, a Jev request, or sync.

// loginStateDirName is the directory under the global data directory that holds
// the login states.
const loginStateDirName = "login-states"

// LoginState is one saved login state. Cookies are the browser's cookie objects
// as the DevTools protocol returned them. LocalStorage maps an origin to its
// items. The values are secrets: never log them.
type LoginState struct {
	Name         string                       `json:"name"`
	SavedAt      time.Time                    `json:"saved_at"`
	Cookies      []json.RawMessage            `json:"cookies"`
	LocalStorage map[string]map[string]string `json:"local_storage"` // origin → items
}

// LoginStateInfo describes one saved login state without its contents. A file
// that cannot be read or parsed is listed with Corrupt set and zero counts, so
// the user can still delete it.
type LoginStateInfo struct {
	Name    string
	SavedAt time.Time
	Cookies int
	Origins int
	Bytes   int64
	Corrupt bool
}

// loginStateTempMaxAge is how old a crash leftover temp file must be before the
// store removes it. A live write finishes in seconds, so an hour is never a
// file still being written.
const loginStateTempMaxAge = time.Hour

// loginProcessStart is when this process loaded the package. It is the process
// start a holder records, so a holder written by an earlier process that reused
// this process's pid can be told apart. It is the same notion as the journal
// lease's ProcessStart: the moment the owning process came up.
var loginProcessStart = time.Now().Round(0)

// ValidLoginStateName checks a login-state name against the one rule defined in
// internal/config (config must not import this package). Every path this store
// builds comes from a name that passed this check.
func ValidLoginStateName(name string) error {
	return config.ValidLoginStateName(name)
}

// ErrLoginStateInUse reports that a live session holds the login state.
type ErrLoginStateInUse struct {
	Name      string
	SessionID string
}

func (e ErrLoginStateInUse) Error() string {
	return fmt.Sprintf("login state %q is in use by session %s", e.Name, e.SessionID)
}

// loginHolder is the holder file <name>.lock.json: which session has the login
// state, and the process that holds it.
//
// ProcessStart is the start of the process that wrote the holder. A pid can be
// reused after a crash, most often after a container restart, so a holder whose
// pid is ours but whose ProcessStart differs is from an earlier process. A holder
// with no ProcessStart (written before the field existed) is judged by pid alone.
type loginHolder struct {
	SessionID    string    `json:"session_id"`
	PID          int       `json:"pid"`
	AcquiredAt   time.Time `json:"acquired_at"`
	ProcessStart time.Time `json:"process_start"`
}

// LoginStateStore keeps login states in one directory. Several ocode processes
// may share it. Each state has one writer at a time, enforced by Acquire.
type LoginStateStore struct {
	dir string
	// alive reports whether a holder's pid is still running. It is pidAlive, the
	// same check the box manager uses. Tests replace it.
	alive func(pid int) bool
	// processStart is the start recorded in holders this store writes, and the
	// value a holder with our pid must match to be live. Tests may replace it.
	processStart time.Time
}

// NewLoginStateStore returns a store rooted at dir. The directory is created on
// the first write.
func NewLoginStateStore(dir string) *LoginStateStore {
	return &LoginStateStore{dir: dir, alive: pidAlive, processStart: loginProcessStart}
}

// DefaultLoginStateDir is the production directory:
// <paths.GlobalDataDir()>/browse/login-states.
func DefaultLoginStateDir() (string, error) {
	base, err := paths.GlobalDataDir()
	if err != nil {
		return "", fmt.Errorf("login states: resolve the data directory: %w", err)
	}
	return filepath.Join(base, "browse", loginStateDirName), nil
}

func (s *LoginStateStore) statePath(name string) string {
	return filepath.Join(s.dir, name+".json")
}

func (s *LoginStateStore) holderPath(name string) string {
	return filepath.Join(s.dir, name+".lock.json")
}

func (s *LoginStateStore) lockPath(name string) string {
	return filepath.Join(s.dir, name+".lock")
}

// Load returns the named login state. A missing state is ok=false with a nil
// error. A file that does not parse, or whose name differs from the file name,
// is an error: it is never treated as missing.
func (s *LoginStateStore) Load(name string) (LoginState, bool, error) {
	if err := ValidLoginStateName(name); err != nil {
		return LoginState{}, false, err
	}
	data, err := os.ReadFile(s.statePath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return LoginState{}, false, nil
	}
	if err != nil {
		return LoginState{}, false, fmt.Errorf("login states: read %q: %w", name, err)
	}
	st, err := decodeLoginState(name, data)
	if err != nil {
		return LoginState{}, false, err
	}
	return st, true, nil
}

// decodeLoginState parses one state file. The error deliberately omits the
// json error text: a syntax error quotes the offending byte, and that byte can
// be part of a cookie or storage value.
func decodeLoginState(name string, data []byte) (LoginState, error) {
	var st LoginState
	if err := json.Unmarshal(data, &st); err != nil {
		return LoginState{}, fmt.Errorf("login states: %q is corrupt (not a login state file), not resetting it", name)
	}
	if st.Name != name {
		return LoginState{}, fmt.Errorf("login states: %q names itself %q, not resetting it", name, st.Name)
	}
	return st, nil
}

// Save writes the login state atomically: a temporary file in the same
// directory, fsynced, then renamed over the state file. A nil cookie list or
// origin map is written as an empty one.
func (s *LoginStateStore) Save(st LoginState) error {
	if err := ValidLoginStateName(st.Name); err != nil {
		return err
	}
	if st.Cookies == nil {
		st.Cookies = []json.RawMessage{}
	}
	if st.LocalStorage == nil {
		st.LocalStorage = map[string]map[string]string{}
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		// Only a cookie (a json.RawMessage) can fail to encode. Its error text
		// would quote the cookie's bytes, so it is not included.
		return fmt.Errorf("login states: encode %q: a cookie is not valid JSON", st.Name)
	}
	return s.writeAtomic(s.statePath(st.Name), data)
}

// Delete removes the named login state and any stale holder. A missing state is
// not an error. A state held by a live session is refused with ErrLoginStateInUse.
func (s *LoginStateStore) Delete(name string) error {
	if err := ValidLoginStateName(name); err != nil {
		return err
	}
	return s.withNameLock(name, func() error {
		holder, held, err := s.liveHolder(name)
		if err != nil {
			return err
		}
		if held {
			return ErrLoginStateInUse{Name: name, SessionID: holder.SessionID}
		}
		if err := removeIfExists(s.statePath(name)); err != nil {
			return fmt.Errorf("login states: delete %q: %w", name, err)
		}
		if err := removeIfExists(s.holderPath(name)); err != nil {
			return fmt.Errorf("login states: remove stale holder of %q: %w", name, err)
		}
		if err := removeIfExists(s.lockPath(name)); err != nil {
			return fmt.Errorf("login states: remove lock file of %q: %w", name, err)
		}
		return nil
	})
}

// List returns every saved login state sorted by name. A file that cannot be read
// or parsed is still listed, with Corrupt set and zero counts, so it can be
// deleted; one bad file does not fail the list. Holder files, lock files and
// temporary files are skipped. A missing directory is an empty list. Crash
// leftover temp files are removed first.
func (s *LoginStateStore) List() ([]LoginStateInfo, error) {
	infos := []LoginStateInfo{}
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return infos, nil
	}
	if err != nil {
		return nil, fmt.Errorf("login states: list %s: %w", s.dir, err)
	}
	s.sweepStaleTemps(func(fname string) bool { return strings.HasPrefix(fname, ".") })
	for _, e := range entries {
		base, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() {
			continue
		}
		// A holder file is "<name>.lock.json"; a dot is never valid in a name, so
		// this check skips it along with anything else that is not a state.
		if ValidLoginStateName(base) != nil {
			continue
		}
		infos = append(infos, s.infoOf(base, e))
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}

// infoOf describes one state file. Any failure to stat, read or parse it yields a
// Corrupt entry. The reason is logged by name only, never with the file's content.
func (s *LoginStateStore) infoOf(name string, e fs.DirEntry) LoginStateInfo {
	corrupt := LoginStateInfo{Name: name, Corrupt: true}
	fi, err := e.Info()
	if err != nil {
		log.Printf("boxsession: login state %q: stat: %v", name, err)
		return corrupt
	}
	corrupt.Bytes = fi.Size()
	data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
	if err != nil {
		log.Printf("boxsession: login state %q: read: %v", name, err)
		return corrupt
	}
	st, err := decodeLoginState(name, data)
	if err != nil {
		log.Printf("boxsession: %v", err)
		return corrupt
	}
	return LoginStateInfo{
		Name:    st.Name,
		SavedAt: st.SavedAt,
		Cookies: len(st.Cookies),
		Origins: len(st.LocalStorage),
		Bytes:   fi.Size(),
	}
}

// sweepStaleTemps removes the crash leftovers among the directory's temp files
// (".<file>.*.tmp", as writeAtomic names them) whose names match and that are older
// than loginStateTempMaxAge. A failure is logged, not returned: a leftover costs
// disk space and must not block the work that triggered the sweep.
func (s *LoginStateStore) sweepStaleTemps(match func(fname string) bool) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("boxsession: login states: sweep temp files: %v", err)
		}
		return
	}
	for _, e := range entries {
		fname := e.Name()
		if e.IsDir() || !match(fname) || !strings.HasSuffix(fname, ".tmp") {
			continue
		}
		fi, err := e.Info()
		if err != nil || time.Since(fi.ModTime()) < loginStateTempMaxAge {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, fname)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Printf("boxsession: login states: remove stale temp file %s: %v", fname, err)
		}
	}
}

// InUseBy reports the session holding the named login state, if a live process
// holds it. A dead holder is not in use. A corrupt holder is an error.
func (s *LoginStateStore) InUseBy(name string) (sessionID string, inUse bool, err error) {
	if err := ValidLoginStateName(name); err != nil {
		return "", false, err
	}
	holder, held, err := s.liveHolder(name)
	if err != nil || !held {
		return "", false, err
	}
	return holder.SessionID, true, nil
}

// Acquire makes sessionID the one writer of the named login state. It fails with
// ErrLoginStateInUse when a live process holds it under another session. A dead
// holder is replaced. The same session in the same process gets the existing
// holder back, and its release frees the name.
//
// The returned release removes the holder only if it is still this acquisition's.
// A second release, or a release after another session took over, is a no-op.
func (s *LoginStateStore) Acquire(name, sessionID string) (release func() error, err error) {
	if err := ValidLoginStateName(name); err != nil {
		return nil, err
	}
	if sessionID == "" {
		return nil, fmt.Errorf("login states: acquire %q: session id is required", name)
	}
	pid := os.Getpid()
	var mine loginHolder
	err = s.withNameLock(name, func() error {
		holder, held, err := s.liveHolder(name)
		if err != nil {
			return err
		}
		if held {
			if holder.SessionID != sessionID || holder.PID != pid {
				return ErrLoginStateInUse{Name: name, SessionID: holder.SessionID}
			}
			mine = holder
			return nil
		}
		mine = loginHolder{SessionID: sessionID, PID: pid, AcquiredAt: time.Now().Round(0), ProcessStart: s.processStart}
		data, err := json.MarshalIndent(mine, "", "  ")
		if err != nil {
			return fmt.Errorf("login states: encode holder of %q: %w", name, err)
		}
		return s.writeAtomic(s.holderPath(name), data)
	})
	if err != nil {
		return nil, err
	}
	return func() error {
		return s.release(name, mine)
	}, nil
}

// Transfer hands the hold of name from fromSession to toSession, both in this
// process. It runs under the name lock, so no third session can take the name in
// between. It fails when the name has no holder, when another session holds it
// (ErrLoginStateInUse, naming that session), or when the holder belongs to another
// process. The returned release frees the name for toSession; the release for
// fromSession becomes a no-op.
func (s *LoginStateStore) Transfer(name, fromSession, toSession string) (release func() error, err error) {
	if err := ValidLoginStateName(name); err != nil {
		return nil, err
	}
	if fromSession == "" || toSession == "" {
		return nil, fmt.Errorf("login states: transfer %q: both session ids are required", name)
	}
	pid := os.Getpid()
	var mine loginHolder
	err = s.withNameLock(name, func() error {
		cur, found, err := s.readHolder(name)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("login states: transfer %q: no session holds it", name)
		}
		if cur.SessionID != fromSession {
			return ErrLoginStateInUse{Name: name, SessionID: cur.SessionID}
		}
		if cur.PID != pid {
			return fmt.Errorf("login states: transfer %q: the holder belongs to another process", name)
		}
		mine = loginHolder{SessionID: toSession, PID: pid, AcquiredAt: time.Now().Round(0), ProcessStart: s.processStart}
		data, err := json.MarshalIndent(mine, "", "  ")
		if err != nil {
			return fmt.Errorf("login states: encode holder of %q: %w", name, err)
		}
		return s.writeAtomic(s.holderPath(name), data)
	})
	if err != nil {
		return nil, err
	}
	return func() error {
		return s.release(name, mine)
	}, nil
}

// release removes the holder of name if it is still the one mine describes.
func (s *LoginStateStore) release(name string, mine loginHolder) error {
	return s.withNameLock(name, func() error {
		cur, found, err := s.readHolder(name)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		if cur.SessionID != mine.SessionID || cur.PID != mine.PID || !cur.AcquiredAt.Equal(mine.AcquiredAt) {
			return nil
		}
		if err := removeIfExists(s.holderPath(name)); err != nil {
			return fmt.Errorf("login states: release %q: %w", name, err)
		}
		return nil
	})
}

// liveHolder returns the holder of name and whether it is still live. A dead
// holder is returned with held=false, so the caller may replace it.
func (s *LoginStateStore) liveHolder(name string) (loginHolder, bool, error) {
	holder, found, err := s.readHolder(name)
	if err != nil || !found {
		return loginHolder{}, false, err
	}
	return holder, s.holderLive(holder), nil
}

// holderLive reports whether a holder's process still owns the name. Its pid must
// be alive. A holder with our own pid is live only when its ProcessStart is ours,
// or absent (a holder from before the field existed, judged by pid alone). A
// different start means our pid was reused: the holder is from an earlier process.
func (s *LoginStateStore) holderLive(h loginHolder) bool {
	if !s.alive(h.PID) {
		return false
	}
	if h.PID == os.Getpid() && !h.ProcessStart.IsZero() && !h.ProcessStart.Equal(s.processStart) {
		return false
	}
	return true
}

// readHolder reads the holder file without judging liveness. A holder with no
// session or no pid is corrupt.
func (s *LoginStateStore) readHolder(name string) (loginHolder, bool, error) {
	data, err := os.ReadFile(s.holderPath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return loginHolder{}, false, nil
	}
	if err != nil {
		return loginHolder{}, false, fmt.Errorf("login states: read holder of %q: %w", name, err)
	}
	var h loginHolder
	if err := json.Unmarshal(data, &h); err != nil {
		return loginHolder{}, false, fmt.Errorf("login states: holder of %q is corrupt, not resetting it: %w", name, err)
	}
	if h.SessionID == "" || h.PID <= 0 {
		return loginHolder{}, false, fmt.Errorf("login states: holder of %q is corrupt: no session or pid", name)
	}
	return h, true, nil
}

// withNameLock runs fn under the cross-process lock of one login state. It first
// removes this name's stale temp files.
func (s *LoginStateStore) withNameLock(name string, fn func() error) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("login states: create directory: %w", err)
	}
	s.sweepStaleTemps(func(fname string) bool { return strings.HasPrefix(fname, "."+name+".") })
	return filelock.WithFileLock(s.lockPath(name), fn)
}

// writeAtomic replaces path with data: a temp file in the same directory, fsync,
// chmod 0600, then rename. The temp file is removed on any failure.
func (s *LoginStateStore) writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("login states: create directory: %w", err)
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return fmt.Errorf("login states: restrict directory: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("login states: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	// After a successful rename this is a no-op; on any failure it removes the temp file.
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("login states: write temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("login states: chmod temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("login states: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("login states: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("login states: rename into place: %w", err)
	}
	syncParentDir(path)
	return nil
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
