package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/stt"
)

// maxSTTUploadBody bounds one voice upload. Matches stt's own 25 MB cap plus
// multipart framing.
const maxSTTUploadBody = 26 << 20

var sttExtPattern = regexp.MustCompile(`^\.[A-Za-z0-9]{1,5}$`)

// sttOptions builds the transcription options from the persisted selection.
// The config is re-read per call so a model change in Settings applies at once.
func (s *Server) sttOptions(model string) stt.Options {
	return stt.Options{
		Model:     model,
		OpenAIKey: func() string { return auth.ResolveKey("openai") },
	}
}

// sttSelectedModel is the persisted model id, or "" when none is saved.
func sttSelectedModel() string {
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.STT.Model
}

// handleGetSTT lists every speech model with its availability on this host.
func (s *Server) handleGetSTT(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, stt.StatusFor(s.sttOptions(sttSelectedModel())))
}

// handleSetSTT persists the selected model. An unknown id is rejected rather
// than silently falling back, so the Settings UI shows what was saved.
func (s *Server) handleSetSTT(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := decodeTTSJSON(w, r, &req, 4<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid speech-to-text config")
		return
	}
	if !stt.ValidModel(req.Model) {
		writeError(w, http.StatusBadRequest, stt.ErrUnknownModel.Error())
		return
	}
	if err := config.SaveOcodeSTTConfig(config.STTConfig{Model: req.Model}); err != nil {
		writeError(w, http.StatusInternalServerError, "save speech-to-text config: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stt.StatusFor(s.sttOptions(req.Model)))
}

// handleTranscribeSTT takes a multipart upload (field "audio") recorded in the
// browser and returns the transcript. The caller decides whether to send it:
// the web UI submits it as the next chat message.
func (s *Server) handleTranscribeSTT(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSTTUploadBody)
	if err := r.ParseMultipartForm(maxSTTUploadBody); err != nil {
		status := http.StatusBadRequest
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			status = http.StatusRequestEntityTooLarge
		}
		writeError(w, status, "expected a multipart audio upload")
		return
	}
	file, hdr, err := r.FormFile("audio")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing audio field")
		return
	}
	defer file.Close()

	ext := filepath.Ext(hdr.Filename)
	if !sttExtPattern.MatchString(ext) {
		ext = ".webm"
	}
	tmp, err := os.CreateTemp("", "ocode-stt-*"+ext)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create temp file: "+err.Error())
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		writeError(w, http.StatusBadRequest, "read audio: "+err.Error())
		return
	}
	if err := tmp.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, "write audio: "+err.Error())
		return
	}

	opts := s.sttOptions(sttSelectedModel())
	res, err := stt.Transcribe(r.Context(), opts, tmp.Name())
	if err != nil {
		status := http.StatusBadGateway
		if !stt.StatusFor(opts).ModelAvailable(stt.ResolveModel(opts.Model)) {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
