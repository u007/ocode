package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tts"
)

const maxTTSConfigBody = 16 << 10

func (s *Server) handleTTSEngines(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"engines": tts.Catalog()})
}

func (s *Server) handleTTSStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.tts.Status())
}

func (s *Server) handleGetTTSConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.tts.Config())
}

func (s *Server) handleSetTTSConfig(w http.ResponseWriter, r *http.Request) {
	var cfg tts.Config
	if err := decodeTTSJSON(w, r, &cfg, maxTTSConfigBody); err != nil {
		status := http.StatusBadRequest
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			status = http.StatusRequestEntityTooLarge
		}
		writeError(w, status, "invalid TTS config")
		return
	}
	if err := validateTTSConfig(cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := config.SaveOcodeTTSConfig(config.TTSConfig{Engine: string(cfg.Engine), Voice: cfg.Voice, Mode: string(cfg.Mode)}); err != nil {
		writeError(w, http.StatusInternalServerError, "save TTS config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.tts.Select(cfg))
}

func (s *Server) handleTTSSelect(w http.ResponseWriter, r *http.Request) {
	s.handleSetTTSConfig(w, r)
}

func (s *Server) handleTTSSpeak(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := decodeTTSJSON(w, r, &req, 1<<20); err != nil {
		status := http.StatusBadRequest
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			status = http.StatusRequestEntityTooLarge
		}
		writeError(w, status, "invalid speech request")
		return
	}
	if utf8.RuneCountInString(req.Text) > 100_000 {
		writeError(w, http.StatusRequestEntityTooLarge, "speech text is too long")
		return
	}
	if s.tts.Status().Config.Engine == tts.EngineBrowserNative {
		writeError(w, http.StatusConflict, "Browser Native speech runs in the browser")
		return
	}
	playback, err := s.tts.Replace(req.Text)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, playback)
}

func (s *Server) handleTTSStop(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.tts.Stop())
}

func (s *Server) handleTTSAudio(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "no server-generated TTS audio is available")
}

func validateTTSConfig(cfg tts.Config) error {
	engine, ok := tts.EngineForHost(cfg.Engine)
	if !ok {
		return errors.New("unknown TTS engine")
	}
	if engine.Availability != tts.AvailabilityReady {
		return errors.New(engine.Label + ": " + engine.Reason)
	}
	if cfg.Mode != tts.PlaybackManual && cfg.Mode != tts.PlaybackAtBottom {
		return errors.New("invalid TTS playback mode")
	}
	return tts.ValidateVoiceID(cfg.Voice)
}

func (s *Server) handleTTSAcceptLicense(w http.ResponseWriter, r *http.Request) {
	var req struct{ Engine string `json:"engine"`; LicenseHash string `json:"license_hash"`; LicenseName string `json:"license_name"` }
	if err := decodeTTSJSON(w, r, &req, 1024); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.tts.AcceptLicense(req.Engine, req.LicenseHash, req.LicenseName); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state":"license-accepted","engine":req.Engine})
}

func (s *Server) handleTTSPin(w http.ResponseWriter, r *http.Request) {
	var req struct{ Engine string `json:"engine"`; ManifestVer string `json:"manifest_version"` }
	if err := decodeTTSJSON(w, r, &req, 1024); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.tts.Pin(req.Engine, req.ManifestVer); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state":"pinned","engine":req.Engine,"manifest_version":req.ManifestVer})
}

func (s *Server) handleTTSDownload(w http.ResponseWriter, r *http.Request) {
	var req struct{ Engine string `json:"engine"` }
	if err := decodeTTSJSON(w, r, &req, 1024); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.tts.Download(req.Engine); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state":"downloading","engine":req.Engine,"progress":"50"})
}

func (s *Server) handleTTSInstall(w http.ResponseWriter, r *http.Request) {
	var req struct{ Engine string `json:"engine"` }
	if err := decodeTTSJSON(w, r, &req, 1024); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.tts.Install(req.Engine); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state":"installed","engine":req.Engine})
}

func (s *Server) handleTTSEnable(w http.ResponseWriter, r *http.Request) {
	var req struct{ Engine string `json:"engine"` }
	if err := decodeTTSJSON(w, r, &req, 1024); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	status, err := s.tts.Enable(req.Engine)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleTTSInstallStates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.tts.InstallStates())
}

func decodeTTSJSON(w http.ResponseWriter, r *http.Request, dst any, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
