package server

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Rejection paths only: they return before any config write or model call, so
// the tests never touch the real HOME or spawn Python.
func TestSetSTTRejectsUnknownModel(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodPut, "/api/stt", strings.NewReader(`{"model":"not-a-model"}`))
	w := httptest.NewRecorder()
	s.handleSetSTT(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestSetSTTRejectsTrailingJSON(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodPut, "/api/stt", strings.NewReader(`{"model":"whisper-1"} {}`))
	w := httptest.NewRecorder()
	s.handleSetSTT(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestTranscribeSTTRequiresMultipartAudio(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodPost, "/api/stt/transcribe", strings.NewReader(`{"text":"x"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTranscribeSTT(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-multipart status = %d, want 400", w.Code)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormField("other")
	_, _ = fw.Write([]byte("x"))
	_ = mw.Close()
	r = httptest.NewRequest(http.MethodPost, "/api/stt/transcribe", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w = httptest.NewRecorder()
	s.handleTranscribeSTT(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing audio field status = %d, want 400", w.Code)
	}
}
