package cdp

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// prepareProfileDir resolves the --user-data-dir for a Chrome launch.
//
// profileDir == "" keeps the historical behavior: a fresh temp dir that the
// returned cleanup removes, so nothing persists across launches.
//
// Otherwise profileDir is created (if needed) and reused across launches so
// cookies, localStorage and logins survive an ocode restart. cleanup is then
// a no-op for the directory itself (Chrome's files stay on disk).
//
// Chrome's posix ProcessSingleton leaves SingletonLock (a symlink whose
// target is "<host>-<pid>"), SingletonSocket and SingletonCookie in the
// profile. After a crash / SIGKILL they linger and a fresh Chrome would
// refuse the profile ("Opening in existing browser session") and exit.
// When the recorded pid is dead the stale files are removed. When it is
// alive (another ocode instance owns the profile) we fall back to a temp
// profile for this launch and log it, rather than failing the browser.
func prepareProfileDir(profileDir string, lg *log.Logger) (string, func(), error) {
	if profileDir == "" {
		return tempProfileDir()
	}
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return "", nil, fmt.Errorf("profile dir: %w", err)
	}
	owner, live := singletonOwner(profileDir)
	if live {
		if lg != nil {
			lg.Printf("browse: chrome profile %s is locked by live pid %d; using a temporary profile for this launch (cookies will not persist)", profileDir, owner)
		}
		return tempProfileDir()
	}
	for _, n := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		p := filepath.Join(profileDir, n)
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", nil, fmt.Errorf("profile dir: remove stale %s: %w", n, err)
		}
	}
	return profileDir, func() {}, nil
}

func tempProfileDir() (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "ocode-browse-*")
	if err != nil {
		return "", nil, fmt.Errorf("launch failed: %w", err)
	}
	return tmpDir, func() { _ = os.RemoveAll(tmpDir) }, nil
}

// singletonOwner reads Chrome's SingletonLock symlink and reports the pid it
// names and whether that process is still alive. Missing or unparsable lock
// means no live owner.
func singletonOwner(profileDir string) (int, bool) {
	target, err := os.Readlink(filepath.Join(profileDir, "SingletonLock"))
	if err != nil {
		return 0, false
	}
	i := strings.LastIndexByte(target, '-')
	if i < 0 {
		return 0, false
	}
	pid, err := strconv.Atoi(target[i+1:])
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, pidAlive(pid)
}
