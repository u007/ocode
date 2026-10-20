package server

import (
	"context"
	"errors"
	"io"
	"log"
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
	// The client sets install=1 only after the user confirmed the one-time
	// dependency install; without it a missing runtime is a 409 that asks.
	opts.InstallApproved = r.URL.Query().Get("install") == "1"
	res, err := stt.Transcribe(r.Context(), opts, tmp.Name())
	if err != nil {
		var need *stt.InstallRequiredError
		if errors.As(err, &need) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":    need.Error(),
				"code":     "stt_install_required",
				"packages": need.Packages,
				"venv":     need.Venv,
			})
			return
		}
		status := http.StatusBadGateway
		if !stt.StatusFor(opts).ModelAvailable(stt.ResolveModel(opts.Model)) {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// sttPrewarmFunc is the prewarm call. It is a package var so a test can
// substitute a slow prewarm and prove the handler answers before the load
// finishes.
var sttPrewarmFunc = stt.Prewarm

// handlePrewarmSTT starts the local speech model in the background so it is
// loaded by the time the user finishes speaking.
//
// It is best-effort: a failure is reported as a non-OK status, but the
// client treats it as advisory and the transcription that follows starts the
// worker itself. The request must not block the caller for the whole load,
// or the microphone prompt would wait behind it — so the load runs in the
// background and the handler answers immediately.
func (s *Server) handlePrewarmSTT(w http.ResponseWriter, r *http.Request) {
	opts := s.sttOptions(sttSelectedModel())
	// Answer as soon as the request is accepted; the load continues in the
	// background. The caller's next action is to talk, not to wait.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), stt.PrewarmTimeout)
		defer cancel()
		if err := sttPrewarmFunc(ctx, opts); err != nil {
			log.Printf("stt: prewarm: %v", err)
		}
	}()
	writeJSON(w, http.StatusOK, map[string]any{"started": true})
}
