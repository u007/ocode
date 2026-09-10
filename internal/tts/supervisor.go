package tts

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type Status struct {
	Config              Config   `json:"config"`
	Engine              Engine   `json:"engine"`
	Host                string   `json:"host"`
	State               string   `json:"state"`
	Hardware            string   `json:"hardware,omitempty"`
	Error               string   `json:"error,omitempty"`
	Playback            Playback `json:"playback"`
	SelectionGeneration uint64   `json:"selection_generation"`
}

type Supervisor struct {
	mu        sync.Mutex
	config    Config
	state     string
	err       string
	playback  PlaybackManager
	selection atomic.Uint64
	installer *Installer
}

func NewSupervisor(cfg Config) *Supervisor {
	if cfg.Engine == "" {
		cfg = DefaultConfig()
	}
	if cfg.Mode == "" {
		cfg.Mode = PlaybackManual
	}
	s := &Supervisor{config: cfg, state: "ready", installer: NewInstaller()}
	s.refreshLocked()
	return s
}

func (s *Supervisor) Config() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

func (s *Supervisor) Select(cfg Config) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg.Mode == "" {
		cfg.Mode = s.config.Mode
	}
	s.config = cfg
	selection := s.selection.Add(1)
	s.err = ""
	s.refreshLocked()
	// Selection changes invalidate playback from the previous engine. Keep this
	// under the supervisor lock so Select cannot race with Replace or Stop and
	// publish playback for a stale engine.
	s.playback.StopForSelection(selection)
	return s.statusLocked()
}

func (s *Supervisor) Prepare() Status {
	s.mu.Lock()
	s.refreshLocked()
	status := s.statusLocked()
	s.mu.Unlock()
	return status
}

func (s *Supervisor) Replace(text string) (Playback, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	engine := s.config.Engine
	s.refreshLocked()
	if s.state != "ready" {
		err := s.err
		if err == "" {
			err = "selected engine is not ready"
		}
		return Playback{}, fmt.Errorf("tts: %s", err)
	}
	return s.playback.ReplaceForSelection(engine, s.selection.Load(), text)
}

func (s *Supervisor) Stop() Playback {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.playback.StopForSelection(s.selection.Load())
}

func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

func (s *Supervisor) refreshLocked() {
	engine, ok := EngineForHost(s.config.Engine)
	if !ok {
		s.state = "error"
		s.err = "unknown TTS engine"
		return
	}
	if engine.Availability == AvailabilityUnavailable {
		s.state = "unavailable"
		s.err = engine.Reason
		return
	}
	s.state = "ready"
	s.err = ""
}

func (s *Supervisor) statusLocked() Status {
	engine, _ := EngineForHost(s.config.Engine)
	return Status{Config: s.config, Engine: engine, Host: Host(), State: s.state, Hardware: "browser", Error: s.err, Playback: s.playback.Status(), SelectionGeneration: s.selection.Load()}
}

// installStateError reports a rejected install-pipeline step so callers can
// surface the real failure instead of a later confusing "not installed".
func installStateError(engine, step string, prev EngineInstall, allowed ...InstallState) error {
	return fmt.Errorf("tts: cannot %s engine %s in state %q (requires one of %v)", step, engine, prev.State, allowed)
}

func (s *Supervisor) installStep(engine, step string, allowed ...InstallState) (EngineInstall, error) {
	if _, ok := EngineForHost(EngineID(engine)); !ok {
		return EngineInstall{}, fmt.Errorf("tts: unknown engine %q", engine)
	}
	prev := s.installer.State(engine)
	for _, a := range allowed {
		if prev.State == a {
			return prev, nil
		}
	}
	return prev, installStateError(engine, step, prev, allowed...)
}

// AcceptLicense records license acceptance. Requires not-accepted/failed state.
func (s *Supervisor) AcceptLicense(engine string, licenseHash string, licenseName string) error {
	prev, err := s.installStep(engine, "accept license for", "", InstallNotAccepted, InstallFailed)
	if err != nil {
		return err
	}
	s.installer.SetState(engine, EngineInstall{EngineID: engine, LicenseHash: licenseHash, LicenseName: licenseName, State: InstallAccepted, Pinned: prev.Pinned, ManifestVer: prev.ManifestVer, Progress: prev.Progress})
	return nil
}

// Pin requires accepted; preserves previous license info.
func (s *Supervisor) Pin(engine string, manifestVer string) error {
	prev, err := s.installStep(engine, "pin", InstallAccepted, InstallFailed)
	if err != nil {
		return err
	}
	s.installer.SetState(engine, EngineInstall{EngineID: engine, LicenseHash: prev.LicenseHash, LicenseName: prev.LicenseName, State: InstallPinned, Pinned: true, ManifestVer: manifestVer, Progress: prev.Progress})
	return nil
}

// Download requires pinned; preserves previous info.
func (s *Supervisor) Download(engine string) error {
	prev, err := s.installStep(engine, "download", InstallPinned, InstallFailed)
	if err != nil {
		return err
	}
	s.installer.SetState(engine, EngineInstall{EngineID: engine, LicenseHash: prev.LicenseHash, LicenseName: prev.LicenseName, State: InstallDownloading, Pinned: prev.Pinned, ManifestVer: prev.ManifestVer, Progress: 50})
	return nil
}

// Install requires downloading (or failed for retry); preserves info.
func (s *Supervisor) Install(engine string) error {
	prev, err := s.installStep(engine, "install", InstallDownloading, InstallFailed)
	if err != nil {
		return err
	}
	s.installer.SetState(engine, EngineInstall{EngineID: engine, LicenseHash: prev.LicenseHash, LicenseName: prev.LicenseName, State: InstallInstalled, Pinned: prev.Pinned, ManifestVer: prev.ManifestVer, Progress: 100})
	return nil
}

// Enable selects the engine only if installed; updates to enabled state.
func (s *Supervisor) Enable(engine string) (Status, error) {
	inst := s.installer.State(engine)
	if inst.State != InstallInstalled && inst.State != InstallEnabled {
		return s.Status(), fmt.Errorf("tts: engine %s not installed (state: %s)", engine, inst.State)
	}
	if inst.State == InstallInstalled {
		s.installer.SetState(engine, EngineInstall{EngineID: engine, LicenseHash: inst.LicenseHash, LicenseName: inst.LicenseName, State: InstallEnabled, Pinned: inst.Pinned, ManifestVer: inst.ManifestVer, Progress: inst.Progress})
	}
	// Preserve the user's saved mode/voice; only the engine changes here.
	cfg := s.Config()
	cfg.Engine = EngineID(engine)
	return s.Select(cfg), nil
}
