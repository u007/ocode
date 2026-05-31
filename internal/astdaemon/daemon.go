// Package astdaemon manages a shared per-project daemon for AST indexing via
// ast-grep (sg). The daemon uses a PID-file lock so that multiple ocode
// instances on the same project share a single file watcher. The first
// instance to call EnsureRunning acquires the lock, starts an fsnotify
// watcher, and runs incremental sg scan --update on file changes. All
// other instances (or the daemon instance itself) can query the index
// directly via Search / Symbols / Kinds — those calls only read the
// persisted SQLite index under .sgindex/.
package astdaemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	lockFileRel  = ".sgindex/daemon.lock"
	indexDirRel  = ".sgindex"
	scanTimeout  = 120 * time.Second
	debounceWait = 500 * time.Millisecond
)

// ErrNotInstalled is returned when the sg binary cannot be found.
var ErrNotInstalled = fmt.Errorf("ast-grep (sg) not found in PATH; install with: npm install -g @ast-grep/cli  or  brew install ast-grep")

// Instance holds the daemon lock and lifecycle for one project.
type Instance struct {
	projectRoot string
	lockPath    string
	indexDir    string

	mu          sync.Mutex
	watcher     *fsnotify.Watcher
	stopWatcher chan struct{}
	cleanedUp   bool
	daemonOwner bool // true if we acquired the lock and run the watcher

	// Debounce timer for fsnotify events.
	debounceTimer *time.Timer
}

// EnsureRunning finds or starts the daemon for projectRoot. It returns the
// Instance so callers can later call Stop. The first process to acquire the
// lock becomes the daemon and watches for file changes; other processes
// simply detect the live lock and skip watcher setup.
//
// projectRoot is typically os.Getwd() — the root of the project being indexed.
func EnsureRunning(projectRoot string) (*Instance, error) {
	inst := &Instance{
		projectRoot: projectRoot,
		lockPath:    filepath.Join(projectRoot, lockFileRel),
		indexDir:    filepath.Join(projectRoot, indexDirRel),
	}

	// Make sure .sgindex/ exists.
	if err := os.MkdirAll(inst.indexDir, 0755); err != nil {
		return nil, fmt.Errorf("create .sgindex: %w", err)
	}

	// Try to acquire the lock (O_EXCL). If we get it, we're the daemon.
	acquired, err := inst.tryAcquireLock()
	if err != nil {
		return nil, fmt.Errorf("acquire daemon lock: %w", err)
	}

	if acquired {
		inst.daemonOwner = true
		// We are the daemon — start file watcher and run initial index.
		if err := inst.startWatching(); err != nil {
			inst.releaseLock()
			return nil, fmt.Errorf("start watcher: %w", err)
		}
		// Initial index build in background — don't block tool startup.
		go func() {
			if err := inst.initialScan(); err != nil {
				log.Printf("ast-daemon: initial scan: %v\n", err)
			}
		}()
	} else {
		// Another instance holds the lock. Verify it's alive; if stale,
		// take over.
		tookOver, err := inst.checkStaleAndTakeover()
		if err != nil {
			return nil, err
		}
		if tookOver {
			inst.daemonOwner = true
			if err := inst.startWatching(); err != nil {
				inst.releaseLock()
				return nil, fmt.Errorf("start watcher after takeover: %w", err)
			}
			go func() {
				if err := inst.initialScan(); err != nil {
					log.Printf("ast-daemon: initial scan: %v\n", err)
				}
			}()
		}
	}

	return inst, nil
}

// Stop releases the daemon lock and stops the file watcher (if we own it).
// Safe to call multiple times.
func (inst *Instance) Stop() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.stopLocked()
}

func (inst *Instance) stopLocked() {
	if inst.cleanedUp {
		return
	}
	inst.cleanedUp = true

	if inst.stopWatcher != nil {
		close(inst.stopWatcher)
	}
	if inst.watcher != nil {
		inst.watcher.Close()
	}
	if inst.debounceTimer != nil {
		inst.debounceTimer.Stop()
	}
	if inst.daemonOwner {
		inst.releaseLock()
	}
}

// IndexDir returns the path to the .sgindex directory.
func (inst *Instance) IndexDir() string { return inst.indexDir }

// ProjectRoot returns the project root passed at creation.
func (inst *Instance) ProjectRoot() string { return inst.projectRoot }

// IsDaemon returns true if this instance holds the daemon lock.
func (inst *Instance) IsDaemon() bool {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.daemonOwner && !inst.cleanedUp
}

// --- lock ---

func (inst *Instance) tryAcquireLock() (bool, error) {
	// Use O_CREATE|O_EXCL for atomic creation.
	f, err := os.OpenFile(inst.lockPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			return false, nil // lock held by another process
		}
		return false, err
	}
	// Write our PID.
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		f.Close()
		os.Remove(inst.lockPath)
		return false, err
	}
	f.Close()
	return true, nil
}

func (inst *Instance) releaseLock() {
	os.Remove(inst.lockPath)
}

// checkStaleAndTakeover reads the lock file, verifies the PID inside is
// alive, and if stale, removes the lock and returns true to indicate the
// caller should become the daemon.
func (inst *Instance) checkStaleAndTakeover() (bool, error) {
	data, err := os.ReadFile(inst.lockPath)
	if err != nil {
		// Lock disappeared — caller should become daemon.
		return true, nil
	}
	pidStr := strings.TrimSpace(string(data))
	if pidStr == "" {
		os.Remove(inst.lockPath)
		return true, nil
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		os.Remove(inst.lockPath)
		return true, nil
	}
	if processExists(pid) {
		return false, nil // daemon is alive, all good
	}
	// Stale lock — remove it, caller becomes daemon.
	os.Remove(inst.lockPath)
	return true, nil
}

// processExists checks whether a process with the given PID is running.
func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, sending signal 0 tests whether the process exists.
	// On Windows, FindProcess always returns a Process, so we optimistically
	// assume it's alive.
	if err := p.Signal(os.Signal(nil)); err != nil {
		return false
	}
	return true
}

// --- watcher ---

func (inst *Instance) startWatching() error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify: %w", err)
	}
	inst.watcher = w
	inst.stopWatcher = make(chan struct{})

	if err := inst.addWatchTree(inst.projectRoot); err != nil {
		w.Close()
		return fmt.Errorf("watch %s: %w", inst.projectRoot, err)
	}

	go inst.watchLoop()
	return nil
}

// watchLoop processes fsnotify events, debounces them, and triggers
// incremental reindex via sg scan --update.
func (inst *Instance) watchLoop() {
	for {
		select {
		case <-inst.stopWatcher:
			return
		case event, ok := <-inst.watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := inst.addWatchTree(event.Name); err != nil {
						log.Printf("ast-daemon: add watch %s: %v\n", event.Name, err)
					}
				}
			}
			if isRelevantEvent(event) {
				inst.debounceReindex()
			}
		case err, ok := <-inst.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("ast-daemon: watch error: %v\n", err)
		}
	}
}

func (inst *Instance) addWatchTree(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		if !d.IsDir() {
			return nil
		}
		base := d.Name()
		if base == ".git" || base == ".sgindex" || base == "node_modules" {
			return filepath.SkipDir
		}
		if err := inst.watcher.Add(path); err != nil && !os.IsExist(err) {
			return err
		}
		return nil
	})
}

func isRelevantEvent(event fsnotify.Event) bool {
	if event.Has(fsnotify.Chmod) {
		return false
	}
	rel := event.Name
	// Skip .git and .sgindex changes — they don't affect the AST index.
	if strings.Contains(rel, "/.git/") || strings.Contains(rel, "\\.git\\") {
		return false
	}
	if strings.Contains(rel, "/.sgindex/") || strings.Contains(rel, "\\.sgindex\\") {
		return false
	}
	return true
}

// debounceReindex ensures we don't run sg scan --update more than once
// per debounceWait during a batch of file changes.
func (inst *Instance) debounceReindex() {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.debounceTimer != nil {
		inst.debounceTimer.Stop()
	}
	inst.debounceTimer = time.AfterFunc(debounceWait, func() {
		if err := runSGScan(inst.projectRoot); err != nil {
			log.Printf("ast-daemon: reindex: %v\n", err)
		}
	})
}

// initialScan runs on daemon startup to ensure the index is built.
func (inst *Instance) initialScan() error {
	return runSGScan(inst.projectRoot)
}

// --- sg wrappers ---

// FindSG returns the path to the sg binary, or ErrNotInstalled.
func FindSG() (string, error) {
	path, err := exec.LookPath("sg")
	if err != nil {
		// Also try ast-grep.
		path, err = exec.LookPath("ast-grep")
		if err != nil {
			return "", ErrNotInstalled
		}
	}
	return path, nil
}

// runSGScan runs sg scan --update in the project root to incrementally
// rebuild the AST index.
func runSGScan(projectRoot string) error {
	sg, err := FindSG()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, sg, "scan", "--update")
	cmd.Dir = projectRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sg scan --update failed: %w\nOutput: %s", err, string(out))
	}
	return nil
}

// SgVersion returns the installed sg version string.
func SgVersion() (string, error) {
	sg, err := FindSG()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(sg, "--version")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("sg --version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
