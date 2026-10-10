package stt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveModelFallsBackToDefault(t *testing.T) {
	if got := ResolveModel(""); got != DefaultModel {
		t.Fatalf("empty = %q, want %q", got, DefaultModel)
	}
	if got := ResolveModel("no-such-model"); got != DefaultModel {
		t.Fatalf("unknown = %q, want default", got)
	}
	if got := ResolveModel("whisper-1"); got != "whisper-1" {
		t.Fatalf("known id changed to %q", got)
	}
}

func TestStatusMarksOpenAIModelsUnavailableWithoutKey(t *testing.T) {
	st := StatusFor(Options{OpenAIKey: func() string { return "" }, Python: "/nonexistent/python"})
	if st.Selected != DefaultModel {
		t.Fatalf("selected = %q", st.Selected)
	}
	for _, m := range st.Models {
		if m.Engine == EngineOpenAI && m.Available {
			t.Fatalf("%s available without a key", m.ID)
		}
		if m.Engine == EngineOpenAI && m.Reason == "" {
			t.Fatalf("%s has no reason", m.ID)
		}
		if m.Engine == EngineLocal && m.Available {
			t.Fatalf("%s available with a missing interpreter", m.ID)
		}
	}
	if st.ModelAvailable(DefaultModel) {
		t.Fatalf("default model reported ready with no onnx-asr")
	}
}

func TestStatusOpenAIAvailableWithKey(t *testing.T) {
	st := StatusFor(Options{OpenAIKey: func() string { return "sk-test" }})
	if !st.ModelAvailable("gpt-4o-transcribe") || !st.ModelAvailable("whisper-1") {
		t.Fatalf("openai models should be ready with a key: %+v", st.Models)
	}
}

func writeAudio(t *testing.T, name string, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTranscribeOpenAISendsMultipartAndParsesText(t *testing.T) {
	var gotAuth, gotModel, gotFile string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		gotModel = r.FormValue("model")
		f, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("file field: %v", err)
		}
		b, _ := io.ReadAll(f)
		gotFile = string(b)
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "  hello world  "})
	}))
	defer srv.Close()

	audio := writeAudio(t, "clip.webm", "RIFFfake-audio")
	res, err := Transcribe(context.Background(), Options{
		Model:         "whisper-1",
		OpenAIKey:     func() string { return "sk-test" },
		OpenAIBaseURL: srv.URL + "/v1",
	}, audio)
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Text != "hello world" || res.Model != "whisper-1" {
		t.Fatalf("result = %+v", res)
	}
	if gotAuth != "Bearer sk-test" || gotModel != "whisper-1" || gotFile != "RIFFfake-audio" {
		t.Fatalf("auth=%q model=%q file=%q", gotAuth, gotModel, gotFile)
	}
}

func TestTranscribeOpenAIUpstreamErrorIsSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad key"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	audio := writeAudio(t, "clip.wav", "x")
	_, err := Transcribe(context.Background(), Options{
		Model: "whisper-1", OpenAIKey: func() string { return "sk-bad" }, OpenAIBaseURL: srv.URL,
	}, audio)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want 401", err)
	}
}

func TestTranscribeRejectsEmptyAndMissingKey(t *testing.T) {
	empty := writeAudio(t, "empty.wav", "")
	if _, err := Transcribe(context.Background(), Options{Model: "whisper-1", OpenAIKey: func() string { return "k" }}, empty); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty recording err = %v", err)
	}
	audio := writeAudio(t, "a.wav", "x")
	if _, err := Transcribe(context.Background(), Options{Model: "whisper-1", OpenAIKey: func() string { return "" }}, audio); err == nil {
		t.Fatalf("missing key should fail")
	}
}

func TestCaptureCommandSelection(t *testing.T) {
	name, args, interrupt, err := captureCommand("linux", "/tmp/x.wav", true)
	if err != nil || name != "arecord" || !interrupt || args[len(args)-1] != "/tmp/x.wav" {
		t.Fatalf("linux+arecord = %q %v %v %v", name, args, interrupt, err)
	}
	name, _, interrupt, err = captureCommand("linux", "/tmp/x.wav", false)
	if err != nil || name != "ffmpeg" || interrupt {
		t.Fatalf("linux ffmpeg fallback = %q interrupt=%v err=%v", name, interrupt, err)
	}
	if name, _, _, _ = captureCommand("darwin", "o.wav", false); name != "ffmpeg" {
		t.Fatalf("darwin = %q", name)
	}
	if _, _, _, err := captureCommand("plan9", "o.wav", false); err == nil {
		t.Fatalf("unsupported OS should error")
	}
}

func TestNormaliseSkipsWAVInput(t *testing.T) {
	in := writeAudio(t, "in.WAV", "x")
	out, cleanup, err := normaliseToWAV(context.Background(), in, "")
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}
	defer cleanup()
	if out != in {
		t.Fatalf("WAV input should pass through, got %q", out)
	}
}

func TestEmbeddedOnnxScriptIsPresent(t *testing.T) {
	if !strings.Contains(transcribeOnnxScript, "import onnx_asr") {
		t.Fatalf("embedded helper missing onnx_asr import")
	}
}
