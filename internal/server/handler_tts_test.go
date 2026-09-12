package server

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tts"
)

func newTTSTestServer() *Server {
	return &Server{tts: tts.NewSupervisor(tts.DefaultConfig(), tts.Options{})}
}

func TestValidateTTSConfigRejectsUnavailableEngines(t *testing.T) {
	s := newTTSTestServer()
	for _, engine := range []tts.EngineID{tts.EnginePiper, tts.EngineKokoro, tts.EngineFishAudio, tts.EngineBreeze} {
		cfg := tts.Config{Engine: engine, Mode: tts.PlaybackManual}
		if err := s.validateTTSConfig(cfg); err == nil {
			t.Errorf("validateTTSConfig accepted unavailable engine %q", engine)
		}
	}
}

func TestValidateTTSConfigRejectsUnsafeVoice(t *testing.T) {
	s := newTTSTestServer()
	for _, voice := range []string{"../escape", "voice/name", strings.Repeat("v", 129)} {
		cfg := tts.Config{Engine: tts.EngineBrowserNative, Mode: tts.PlaybackManual, Voice: voice}
		if err := s.validateTTSConfig(cfg); err == nil {
			t.Errorf("validateTTSConfig accepted voice %q", voice)
		}
	}
}

func TestDecodeTTSJSONRejectsTrailingValues(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/tts/select", strings.NewReader(`{"engine":"browser-native"} {}`))
	w := httptest.NewRecorder()
	var cfg tts.Config
	if err := decodeTTSJSON(w, r, &cfg, 1024); err == nil {
		t.Fatal("decodeTTSJSON accepted trailing JSON")
	}
}

func TestDecodeTTSJSONHonorsBodyLimit(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/tts/select", bytes.NewReader(bytes.Repeat([]byte("x"), 1025)))
	w := httptest.NewRecorder()
	var cfg tts.Config
	if err := decodeTTSJSON(w, r, &cfg, 1024); err == nil {
		t.Fatal("decodeTTSJSON accepted an oversized body")
	}
}
