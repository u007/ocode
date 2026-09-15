package server

import (
	"bytes"
	"encoding/json"
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

func TestValidateTTSConfigRejectsInvalidModelVoice(t *testing.T) {
	s := newTTSTestServer()
	cases := []tts.Config{
		{Engine: tts.EngineBrowserNative, Mode: tts.PlaybackManual, ModelVoice: map[string]string{"unknown/model": "voice"}},
		{Engine: tts.EngineBrowserNative, Mode: tts.PlaybackManual, ModelVoice: map[string]string{"piper/": "en_US-joe-medium"}},
		{Engine: tts.EngineBrowserNative, Mode: tts.PlaybackManual, ModelVoice: map[string]string{"piper/model": "not-a-kokoro-voice"}},
	}
	for _, cfg := range cases {
		if err := s.validateTTSConfig(cfg); err == nil {
			t.Errorf("validateTTSConfig accepted invalid model_voice %#v", cfg.ModelVoice)
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

func TestHandleTTSAcceptLicenseValidatesMetadata(t *testing.T) {
	manifest, ok := tts.ManifestFor(tts.EnginePiper)
	if !ok {
		t.Fatal("piper manifest missing")
	}

	testCases := []struct {
		name        string
		licenseHash string
		licenseName string
		wantStatus  int
	}{
		{name: "valid", licenseHash: manifest.LicenseHash(), licenseName: manifest.LicenseName, wantStatus: 200},
		{name: "synthetic hash", licenseHash: "manifest:piper", licenseName: manifest.LicenseName, wantStatus: 409},
		{name: "stale name", licenseHash: manifest.LicenseHash(), licenseName: "old license", wantStatus: 409},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTTSTestServer()
			body, err := json.Marshal(map[string]string{
				"engine":       string(tts.EnginePiper),
				"license_hash": tc.licenseHash,
				"license_name": tc.licenseName,
			})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/api/tts/license", bytes.NewReader(body))
			w := httptest.NewRecorder()
			s.handleTTSAcceptLicense(w, r)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}
