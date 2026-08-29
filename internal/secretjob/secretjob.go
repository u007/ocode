// Package secretjob provides the shared, cancellable, progress-reporting
// core for encrypting/decrypting many files under a directory. It is used
// by the CLI (synchronously), the TUI, and the server (both asynchronously
// via Manager) so none of them duplicate the walk/transform/cancel logic.
package secretjob

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/u007/ocode/internal/secretfile"
)

// ErrCancelled is returned by a job's error when it was stopped via
// Manager.Cancel before processing every file.
var ErrCancelled = errors.New("secretjob: cancelled")

// Walk lists the regular files under root eligible for the given
// operation: forEncrypt=true selects plaintext files (skipping ones already
// age-encrypted), forEncrypt=false selects age-encrypted files (skipping
// plaintext ones). It never descends into .git, and never includes keyPath
// itself (the project's own wrapped key file).
func Walk(root, keyPath string, forEncrypt bool) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if keyPath != "" && path == keyPath {
			return nil
		}

		encrypted, err := secretfile.PeekIsEncrypted(path)
		if err != nil {
			return fmt.Errorf("peek %s: %w", path, err)
		}
		if encrypted != forEncrypt {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// TransformFile encrypts or decrypts absFile in place with key, preserving
// its file mode. It fails if the file is already in the requested state.
func TransformFile(key *secretfile.ProjectKey, absFile string, encrypt bool) error {
	info, err := os.Stat(absFile)
	if err != nil {
		return fmt.Errorf("stat %s: %w", absFile, err)
	}

	data, err := os.ReadFile(absFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", absFile, err)
	}

	var out []byte
	if encrypt {
		if secretfile.IsEncrypted(data) {
			return fmt.Errorf("%s is already encrypted", absFile)
		}
		if out, err = key.Encrypt(data); err != nil {
			return err
		}
	} else {
		if !secretfile.IsEncrypted(data) {
			return fmt.Errorf("%s is not encrypted", absFile)
		}
		if out, err = key.Decrypt(data); err != nil {
			return err
		}
	}

	return secretfile.WriteFileAtomic(absFile, out, info.Mode().Perm())
}

// ProgressFunc reports that current (index done of total) just finished.
type ProgressFunc func(done, total int, current string)

// runFiles processes files in order, calling onProgress after each one and
// checking ctx for cancellation between files. It returns ErrCancelled if
// stopped early, or the first transform error encountered.
func runFiles(ctx context.Context, key *secretfile.ProjectKey, files []string, encrypt bool, onProgress ProgressFunc) error {
	total := len(files)
	for i, f := range files {
		if err := ctx.Err(); err != nil {
			return ErrCancelled
		}
		if err := TransformFile(key, f, encrypt); err != nil {
			return err
		}
		if onProgress != nil {
			onProgress(i+1, total, f)
		}
	}
	return nil
}

type job struct {
	root   string
	cancel context.CancelFunc
}

// Manager runs cancellable directory-wide encrypt/decrypt jobs in the
// background, one active job per project root at a time.
type Manager struct {
	mu       sync.Mutex
	jobs     map[string]*job
	rootJobs map[string]string // root -> active jobID
}

// NewManager returns an empty Manager.
func NewManager() *Manager {
	return &Manager{
		jobs:     make(map[string]*job),
		rootJobs: make(map[string]string),
	}
}

func newJobID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Start launches a job encrypting/decrypting files with key in the
// background. onProgress and onDone (called exactly once, with nil on
// success, ErrCancelled if Cancel was called, or the first transform error)
// receive the job's ID as their first argument -- not the string Start
// itself returns, since a caller can only capture that return value after
// Start returns, by which point the background goroutine may already have
// invoked a callback. Start fails if root already has an active job.
func (m *Manager) Start(root string, key *secretfile.ProjectKey, files []string, encrypt bool, onProgress func(jobID string, done, total int, current string), onDone func(jobID string, err error)) (string, error) {
	m.mu.Lock()
	if _, active := m.rootJobs[root]; active {
		m.mu.Unlock()
		return "", fmt.Errorf("secretjob: a job is already running for %s", root)
	}
	ctx, cancel := context.WithCancel(context.Background())
	id := newJobID()
	m.jobs[id] = &job{root: root, cancel: cancel}
	m.rootJobs[root] = id
	m.mu.Unlock()

	go func() {
		var progress ProgressFunc
		if onProgress != nil {
			progress = func(done, total int, current string) {
				onProgress(id, done, total, current)
			}
		}
		err := runFiles(ctx, key, files, encrypt, progress)

		m.mu.Lock()
		delete(m.jobs, id)
		if m.rootJobs[root] == id {
			delete(m.rootJobs, root)
		}
		m.mu.Unlock()

		if onDone != nil {
			onDone(id, err)
		}
	}()

	return id, nil
}

// Cancel stops the job with the given ID before it processes its next file.
// It returns false if no such active job exists.
func (m *Manager) Cancel(jobID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return false
	}
	j.cancel()
	return true
}
