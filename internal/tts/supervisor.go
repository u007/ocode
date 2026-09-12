package tts

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/tool"
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

// Options wires the supervisor to the host. Root is the ocode data dir that
// holds models/tts; ProcSup is the shared process supervisor every synthesis
// process is registered with. Both are required for local engines; a
// supervisor built with zero Options only serves Browser Native.
type Options struct {
	Root    string
	ProcSup *tool.ProcessSupervisor
}

type Supervisor struct {
	mu        sync.Mutex
	config    Config
	state     string
	err       string
	playback  PlaybackManager
	selection atomic.Uint64
	installer *Installer
	opts      Options
	client    *http.Client

	// installJobs guards one background install per engine.
	installJobs sync.Map // engine -> struct{}

	synthMu     sync.Mutex
	synthCancel context.CancelFunc
}

func NewSupervisor(cfg Config, opts Options) *Supervisor {
	if cfg.Engine == "" {
		cfg = DefaultConfig()
	}
	if cfg.Mode == "" {
		cfg.Mode = PlaybackManual
	}
	s := &Supervisor{config: cfg, state: "ready", installer: NewInstaller(opts.Root), opts: opts,
		client: &http.Client{Timeout: 30 * time.Minute}}
	s.reconcileInstalled()
	s.refreshLocked()
	return s
}

// reconcileInstalled re-verifies cached artifacts at startup so a persisted
// "installed"/"enabled" state never outlives a deleted or corrupted cache,
// and a complete cache left by a crash mid-state-write is recognized.
func (s *Supervisor) reconcileInstalled() {
	if s.opts.Root == "" {
		return
	}
	for _, e := range Catalog() {
		m, ok := ManifestFor(e.ID)
		if !ok {
			continue
		}
		st := s.installer.State(string(e.ID))
		verr := (&piperInstaller{root: s.opts.Root, manifest: m}).Verify()
		switch {
		case verr == nil && st.State != InstallInstalled && st.State != InstallEnabled:
			s.installer.Update(string(e.ID), func(x *EngineInstall) {
				x.State, x.Progress, x.Error, x.Pinned, x.ManifestVer = InstallInstalled, 100, "", true, m.Version
			})
		case verr != nil && (st.State == InstallInstalled || st.State == InstallEnabled):
			log.Printf("tts: %s cache no longer verifies (%v); marking failed", e.ID, verr)
			s.installer.Update(string(e.ID), func(x *EngineInstall) {
				x.State, x.Error = InstallFailed, "cached install failed verification: "+verr.Error()
			})
		case verr != nil && st.State == InstallDownloading:
			// A job cannot survive a restart.
			s.installer.Update(string(e.ID), func(x *EngineInstall) {
				x.State, x.Error = InstallFailed, "install interrupted by restart"
			})
		}
	}
}

func (s *Supervisor) Config() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

// Engine returns the effective engine entry: catalog availability overlaid
// with the verified install state.
func (s *Supervisor) Engine(id EngineID) (Engine, bool) {
	e, ok := EngineForHost(id)
	if !ok {
		return Engine{}, false
	}
	if e.Availability == AvailabilityInstallable {
		st := s.installer.State(string(id))
		if st.State == InstallInstalled || st.State == InstallEnabled {
			e.Availability = AvailabilityReady
			e.Reason = ""
		}
	}
	return e, true
}

// Catalog lists engines with effective availability.
func (s *Supervisor) Catalog() []Engine {
	out := Catalog()
	for i := range out {
		out[i], _ = s.Engine(out[i].ID)
	}
	return out
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
	s.cancelSynth()
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

// Replace starts a new speech request. For local engines synthesis runs in
// the background; the returned playback is "synthesizing" and callers poll
// Status until it is "ready" (then fetch AudioPath) or "error".
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
	m, ok := ManifestFor(engine)
	if !ok {
		s.cancelSynth()
		return s.playback.ReplaceForSelection(engine, s.selection.Load(), text)
	}
	return s.replaceWithLocked(m, text)
}

// replaceWith is the local-engine half of Replace, split out so tests can
// drive a stand-in manifest. Caller must not hold s.mu.
func (s *Supervisor) replaceWith(m Manifest, text string) (Playback, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replaceWithLocked(m, text)
}

func (s *Supervisor) replaceWithLocked(m Manifest, text string) (Playback, error) {
	if s.opts.ProcSup == nil || s.opts.Root == "" {
		return Playback{}, errors.New("tts: local engines are not wired to a process supervisor")
	}
	s.cancelSynth()
	playback, err := s.playback.ReplaceForSelection(m.Engine, s.selection.Load(), text)
	if err != nil {
		return Playback{}, err
	}
	playback.Status = PlaybackStatusSynthesizing
	playback.AudioID = strconv.FormatUint(playback.Generation, 10)
	s.playback.Transition(playback.Generation, func(p *Playback) { *p = playback })
	ctx, cancel := context.WithTimeout(context.Background(), synthTimeout)
	s.synthMu.Lock()
	s.synthCancel = cancel
	s.synthMu.Unlock()
	go s.runSynth(ctx, cancel, m, playback)
	return playback, nil
}

func (s *Supervisor) runSynth(ctx context.Context, cancel context.CancelFunc, m Manifest, playback Playback) {
	defer cancel()
	audioDir := filepath.Join(s.opts.Root, "models", "tts", "audio")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		s.failSynth(playback.Generation, fmt.Errorf("create audio dir: %w", err))
		return
	}
	// Only the newest rendering is kept; earlier ones were replaced or stopped.
	if old, err := filepath.Glob(filepath.Join(audioDir, "*.wav")); err == nil {
		for _, f := range old {
			if filepath.Base(f) != playback.AudioID+".wav" {
				_ = os.Remove(f)
			}
		}
	}
	out := filepath.Join(audioDir, playback.AudioID+".wav")
	if err := piperSynth(ctx, s.opts.ProcSup, s.opts.Root, m, playback.AudioID, playback.Text, out); err != nil {
		if errors.Is(err, context.Canceled) {
			_ = os.Remove(out)
			return // stopped or replaced; the newer state already published.
		}
		log.Printf("tts: synthesis %s failed: %v", playback.AudioID, err)
		s.failSynth(playback.Generation, err)
		return
	}
	s.playback.Transition(playback.Generation, func(p *Playback) { p.Status = PlaybackStatusReady })
}

func (s *Supervisor) failSynth(generation uint64, err error) {
	s.playback.Transition(generation, func(p *Playback) {
		p.Status = PlaybackStatusError
		p.Error = err.Error()
	})
}

// cancelSynth aborts any in-flight synthesis process.
func (s *Supervisor) cancelSynth() {
	s.synthMu.Lock()
	defer s.synthMu.Unlock()
	if s.synthCancel != nil {
		s.synthCancel()
		s.synthCancel = nil
	}
}

// AudioPath resolves a ready audio id to its file. It refuses ids that are not
// the current ready playback so a stale or guessed id never serves a file.
func (s *Supervisor) AudioPath(id string) (string, error) {
	active := s.playback.Status()
	if active.AudioID == "" || active.AudioID != id {
		return "", fmt.Errorf("audio %q is not the active playback", id)
	}
	if active.Status != PlaybackStatusReady {
		return "", fmt.Errorf("audio %q is %s", id, active.Status)
	}
	return filepath.Join(s.opts.Root, "models", "tts", "audio", id+".wav"), nil
}

func (s *Supervisor) Stop() Playback {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelSynth()
	return s.playback.StopForSelection(s.selection.Load())
}

func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

func (s *Supervisor) refreshLocked() {
	engine, ok := s.Engine(s.config.Engine)
	if !ok {
		s.state = "error"
		s.err = "unknown TTS engine"
		return
	}
	if engine.Availability != AvailabilityReady {
		s.state = "unavailable"
		s.err = engine.Reason
		return
	}
	s.state = "ready"
	s.err = ""
}

func (s *Supervisor) statusLocked() Status {
	engine, _ := s.Engine(s.config.Engine)
	hardware := "browser"
	if !engine.BrowserOnly {
		hardware = "cpu"
	}
	return Status{Config: s.config, Engine: engine, Host: Host(), State: s.state, Hardware: hardware, Error: s.err, Playback: s.playback.Status(), SelectionGeneration: s.selection.Load()}
}

// installStateError reports a rejected install-pipeline step so callers can
// surface the real failure instead of a later confusing "not installed".
func installStateError(engine, step string, prev EngineInstall, allowed ...InstallState) error {
	return fmt.Errorf("tts: cannot %s engine %s in state %q (requires one of %v)", step, engine, prev.State, allowed)
}

func (s *Supervisor) installStep(engine, step string, allowed ...InstallState) (EngineInstall, error) {
	e, ok := EngineForHost(EngineID(engine))
	if !ok {
		return EngineInstall{}, fmt.Errorf("tts: unknown engine %q", engine)
	}
	if e.Availability == AvailabilityUnavailable {
		return EngineInstall{}, fmt.Errorf("tts: %s: %s", e.Label, e.Reason)
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

// Pin requires accepted; the version must match the engine's manifest.
func (s *Supervisor) Pin(engine string, manifestVer string) error {
	prev, err := s.installStep(engine, "pin", InstallAccepted, InstallFailed)
	if err != nil {
		return err
	}
	m, ok := ManifestFor(EngineID(engine))
	if !ok {
		return fmt.Errorf("tts: engine %s has no manifest to pin", engine)
	}
	if manifestVer != m.Version {
		return fmt.Errorf("tts: manifest version %q is not the pinned %q", manifestVer, m.Version)
	}
	s.installer.SetState(engine, EngineInstall{EngineID: engine, LicenseHash: prev.LicenseHash, LicenseName: prev.LicenseName, State: InstallPinned, Pinned: true, ManifestVer: manifestVer, Progress: 0})
	return nil
}

// Download requires pinned (or failed for retry) and starts the background
// download + venv install job. The state moves to downloading immediately and
// to installed or failed when the job ends; poll InstallStates for progress.
func (s *Supervisor) Download(engine string) error {
	prev, err := s.installStep(engine, "download", InstallPinned, InstallFailed)
	if err != nil {
		return err
	}
	if s.opts.Root == "" {
		return errors.New("tts: no cache root configured for downloads")
	}
	m, ok := ManifestFor(EngineID(engine))
	if !ok {
		return fmt.Errorf("tts: engine %s has no manifest", engine)
	}
	if _, running := s.installJobs.LoadOrStore(engine, struct{}{}); running {
		return fmt.Errorf("tts: engine %s install is already running", engine)
	}
	s.installer.SetState(engine, EngineInstall{EngineID: engine, LicenseHash: prev.LicenseHash, LicenseName: prev.LicenseName, State: InstallDownloading, Pinned: true, ManifestVer: m.Version, Progress: 0, Step: "starting"})
	go s.runInstall(engine, m)
	return nil
}

func (s *Supervisor) runInstall(engine string, m Manifest) {
	defer s.installJobs.Delete(engine)
	inst := &piperInstaller{root: s.opts.Root, manifest: m, client: s.client, progress: func(pct int, step string) {
		s.installer.Update(engine, func(x *EngineInstall) { x.Progress, x.Step = pct, step })
	}}
	err := WithDownloadLock(s.opts.Root, func(string) error {
		return inst.Install(context.Background())
	})
	if err != nil {
		log.Printf("tts: install %s failed: %v", engine, err)
		s.installer.Update(engine, func(x *EngineInstall) {
			x.State, x.Error, x.Step = InstallFailed, err.Error(), ""
		})
		return
	}
	s.installer.Update(engine, func(x *EngineInstall) {
		x.State, x.Error, x.Step, x.Progress = InstallInstalled, "", "", 100
	})
}

// Install re-verifies cached artifacts and marks the engine installed. It is
// the manual path when a complete cache exists but state was lost.
func (s *Supervisor) Install(engine string) error {
	prev, err := s.installStep(engine, "install", InstallDownloading, InstallFailed, InstallPinned)
	if err != nil {
		return err
	}
	if prev.State == InstallDownloading {
		return fmt.Errorf("tts: engine %s is still installing (%d%%)", engine, prev.Progress)
	}
	m, ok := ManifestFor(EngineID(engine))
	if !ok {
		return fmt.Errorf("tts: engine %s has no manifest", engine)
	}
	if err := (&piperInstaller{root: s.opts.Root, manifest: m}).Verify(); err != nil {
		return fmt.Errorf("tts: engine %s is not installed: %w", engine, err)
	}
	s.installer.SetState(engine, EngineInstall{EngineID: engine, LicenseHash: prev.LicenseHash, LicenseName: prev.LicenseName, State: InstallInstalled, Pinned: true, ManifestVer: m.Version, Progress: 100})
	return nil
}

// Enable selects the engine only if installed; updates to enabled state.
func (s *Supervisor) Enable(engine string) (Status, error) {
	inst := s.installer.State(engine)
	if inst.State != InstallInstalled && inst.State != InstallEnabled {
		return s.Status(), fmt.Errorf("tts: engine %s not installed (state: %s)", engine, inst.State)
	}
	if inst.State == InstallInstalled {
		s.installer.Update(engine, func(x *EngineInstall) { x.State = InstallEnabled })
	}
	// Preserve the user's saved mode; the voice is the manifest's.
	cfg := s.Config()
	cfg.Engine = EngineID(engine)
	if m, ok := ManifestFor(EngineID(engine)); ok {
		cfg.Voice = m.Voice
	}
	return s.Select(cfg), nil
}

// InstallStates returns the per-engine install records keyed by engine id.
func (s *Supervisor) InstallStates() map[string]EngineInstall {
	states := s.installer.All()
	out := make(map[string]EngineInstall, len(states))
	for _, st := range states {
		out[st.EngineID] = st
	}
	return out
}
