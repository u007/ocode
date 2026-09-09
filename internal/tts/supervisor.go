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
}

func NewSupervisor(cfg Config) *Supervisor {
	if cfg.Engine == "" {
		cfg = DefaultConfig()
	}
	if cfg.Mode == "" {
		cfg.Mode = PlaybackManual
	}
	s := &Supervisor{config: cfg, state: "ready"}
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
