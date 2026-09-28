// Package logfile is an append-only log file that rotates itself once it
// grows past a size cap, keeping one previous generation (<path>.1). It is the
// durable sink for processes whose stderr goes nowhere (the desktop .app has
// fd 2 on /dev/null).
package logfile

import (
	"fmt"
	"os"
	"sync"
)

// Writer appends to a file, rotating it to <path>.1 when a write would push it
// past MaxBytes. Safe for concurrent use.
type Writer struct {
	path     string
	maxBytes int64

	mu   sync.Mutex
	f    *os.File
	size int64
}

// Open opens (creating if needed) path for append.
func Open(path string, maxBytes int64) (*Writer, error) {
	w := &Writer{path: path, maxBytes: maxBytes}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log %s: %w", w.path, err)
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("stat log %s: %w", w.path, err)
	}
	w.f, w.size = f, st.Size()
	return nil
}

// Write appends p, rotating first when the file would exceed MaxBytes.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *Writer) rotate() error {
	if err := w.f.Close(); err != nil {
		return fmt.Errorf("close log %s for rotation: %w", w.path, err)
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil {
		// Keep logging into the current file rather than losing lines.
		if openErr := w.open(); openErr != nil {
			return fmt.Errorf("rotate log %s: %v; reopen: %w", w.path, err, openErr)
		}
		return fmt.Errorf("rotate log %s: %w", w.path, err)
	}
	return w.open()
}

// Close closes the underlying file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}
