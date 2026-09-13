package computer

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/u007/ocode/internal/tool"
)

// helperScript manages one embedded helper (the darwin JXA or Windows
// PowerShell script) written to the temp dir. The file is written on first
// use and removed when a supervisor it was registered with shuts down; a
// later ensure after that shutdown writes it again, so a process that runs
// several supervisors over its lifetime (server sessions) never points a
// live driver at a deleted file.
type helperScript struct {
	name string // file name pattern, e.g. "ocode-cgevent-%d.js"
	data []byte

	mu         sync.Mutex
	path       string
	registered map[*tool.ProcessSupervisor]bool
}

func (h *helperScript) ensure(sup *tool.ProcessSupervisor) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.path == "" {
		path := filepath.Join(os.TempDir(), fmt.Sprintf(h.name, os.Getpid()))
		if err := os.WriteFile(path, h.data, 0o600); err != nil {
			return "", fmt.Errorf("computer: write helper %s: %w", path, err)
		}
		h.path = path
	}
	if h.registered == nil {
		h.registered = map[*tool.ProcessSupervisor]bool{}
	}
	if !h.registered[sup] {
		if err := sup.RegisterShutdownCallback(h.remove); err != nil {
			return "", fmt.Errorf("computer: register helper cleanup: %w", err)
		}
		h.registered[sup] = true
	}
	return h.path, nil
}

func (h *helperScript) remove() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.path == "" {
		return
	}
	if err := os.Remove(h.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("[COMPUTER] remove helper %s: %v", h.path, err)
	}
	h.path = ""
	h.registered = nil
}
