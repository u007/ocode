package tts

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// Per-engine installation state machine as defined in the design spec.
type InstallState string

const (
	InstallNotAccepted InstallState = "not-accepted"
	InstallAccepted    InstallState = "license-accepted"
	InstallPinned      InstallState = "pinned"
	InstallDownloading InstallState = "downloading"
	InstallInstalled   InstallState = "installed"
	InstallFailed      InstallState = "failed"
	InstallEnabled     InstallState = "enabled"
)

type EngineInstall struct {
	EngineID    string       `json:"engine_id"`
	LicenseHash string       `json:"license_hash"`
	LicenseName string       `json:"license_name"`
	State       InstallState `json:"state"`
	Progress    int          `json:"progress"`
	Step        string       `json:"step,omitempty"`
	Error       string       `json:"error,omitempty"`
	ManifestVer string       `json:"manifest_version,omitempty"`
	Pinned      bool         `json:"pinned"`
}

// Installer tracks per-engine install states independently of supervisor
// playback and persists them under the cache root so license acceptance and
// installed artifacts survive restarts. An empty root keeps state in memory
// only (tests). Downloads must use WithDownloadLock; the supervisor never
// holds its lock during I/O.
type Installer struct {
	mu     sync.RWMutex
	path   string
	states map[string]EngineInstall // key: engine_id
}

func NewInstaller(root string) *Installer {
	i := &Installer{states: make(map[string]EngineInstall)}
	if root == "" {
		return i
	}
	i.path = filepath.Join(root, "models", "tts", "install-state.json")
	data, err := os.ReadFile(i.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("tts: read install state %s: %v", i.path, err)
		}
		return i
	}
	if err := json.Unmarshal(data, &i.states); err != nil {
		log.Printf("tts: parse install state %s: %v (starting from empty state)", i.path, err)
		i.states = make(map[string]EngineInstall)
	}
	return i
}

func (i *Installer) State(id string) EngineInstall {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.states[id]
}

func (i *Installer) All() []EngineInstall {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]EngineInstall, 0, len(i.states))
	for _, s := range i.states {
		out = append(out, s)
	}
	return out
}

func (i *Installer) SetState(id string, s EngineInstall) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.states[id] = s
	if err := i.persistLocked(); err != nil {
		log.Printf("tts: persist install state for %s: %v", id, err)
	}
}

// Update applies fn to the current state of id under the lock. It keeps
// progress/error updates from a background job from clobbering fields set
// concurrently by the supervisor.
func (i *Installer) Update(id string, fn func(*EngineInstall)) EngineInstall {
	i.mu.Lock()
	defer i.mu.Unlock()
	s := i.states[id]
	if s.EngineID == "" {
		s.EngineID = id
	}
	fn(&s)
	i.states[id] = s
	if err := i.persistLocked(); err != nil {
		log.Printf("tts: persist install state for %s: %v", id, err)
	}
	return s
}

func (i *Installer) persistLocked() error {
	if i.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(i.path), 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	data, err := json.MarshalIndent(i.states, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	tmp := i.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if err := os.Rename(tmp, i.path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}
