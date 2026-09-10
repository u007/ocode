package tts

import "sync"

// Per-engine installation state machine as defined in the design spec.
type InstallState string

const (
	InstallNotAccepted  InstallState = "not-accepted"
	InstallAccepted     InstallState = "license-accepted"
	InstallPinned       InstallState = "pinned"
	InstallDownloading  InstallState = "downloading"
	InstallInstalled    InstallState = "installed"
	InstallFailed       InstallState = "failed"
	InstallEnabled      InstallState = "enabled"
)

type EngineInstall struct {
	EngineID     string     `json:"engine_id"`
	LicenseHash  string     `json:"license_hash"`
	LicenseName  string     `json:"license_name"`
	State        InstallState `json:"state"`
	Progress     int        `json:"progress"`
	Error        string     `json:"error,omitempty"`
	ManifestVer  string     `json:"manifest_version,omitempty"`
	Pinned       bool       `json:"pinned"`
}

// Installer tracks per-engine install states independently of supervisor playback.
// Downloads must use WithDownloadLock; supervisor should never hold its lock during I/O.
type Installer struct {
	mu     sync.RWMutex
	states map[string]EngineInstall // key: engine_id
}

func NewInstaller() *Installer {
	return &Installer{states: make(map[string]EngineInstall)}
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
}
