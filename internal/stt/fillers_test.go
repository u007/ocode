package stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoveFillerWords(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"comma pair", "we should, um, go", "we should go"},
		{"leading filler", "Um, hello", "Hello"},
		{"leading filler no comma", "um hello there", "Hello there"},
		{"plain middle", "and um the thing", "and the thing"},
		{"repeated", "uh, uh, I think so", "I think so"},
		{"trailing", "go um", "go"},
		{"trailing full stop", "go um.", "go."},
		{"only filler", "Um.", ""},
		{"only filler words", "uh um mmm", ""},
		{"sentence boundary", "Hello. Um, we go", "Hello. We go"},
		{"after comma", "well, um yeah", "well, yeah"},
		{"hyphenated affirmation", "uh-huh", "uh-huh"},
		{"affirmation pair", "mm hmm", "mm hmm"},
		{"affirmation after filler", "so uh huh", "so uh huh"},
		{"part of a word", "umbrella and humming", "umbrella and humming"},
		{"discourse marker kept", "it was, like, huge", "it was, like, huge"},
		{"oh kept", "oh, that one", "oh, that one"},
		{"case insensitive", "UMM, Actually", "Actually"},
		{"newlines preserved", "first line\num, second line", "first line\nsecond line"},
		{"no filler untouched", "already  clean .", "already  clean ."},
		{"empty", "", ""},
		{"punctuation only", "...", "..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RemoveFillerWords(tc.in); got != tc.want {
				t.Fatalf("RemoveFillerWords(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestOptionsCleanHonorsKeepFillers(t *testing.T) {
	const raw = "um, hello"
	if got := (Options{}).clean(raw); got != "Hello" {
		t.Fatalf("default clean = %q, want %q", got, "Hello")
	}
	if got := (Options{KeepFillers: true}).clean(raw); got != raw {
		t.Fatalf("KeepFillers clean = %q, want %q", got, raw)
	}
}

// TestStatusJSONCarriesFillerSetting pins the wire key the Settings form reads.
func TestStatusJSONCarriesFillerSetting(t *testing.T) {
	raw, err := json.Marshal(Status{RemoveFillers: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"remove_fillers":true`) {
		t.Fatalf("status JSON missing remove_fillers: %s", raw)
	}
}

// TestTranscribeCleansEveryEngine proves the cleanup is wired into the shared
// transcription path, not just callable on its own: the hosted engine returns
// its transcript through the same step the local engine does.
func TestTranscribeCleansEveryEngine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "Um, hello there."})
	}))
	defer srv.Close()

	audio := writeAudio(t, "clip.webm", "RIFFfake-audio")
	base := Options{
		Model:         "whisper-1",
		OpenAIKey:     func() string { return "sk-test" },
		OpenAIBaseURL: srv.URL,
	}

	got, err := Transcribe(context.Background(), base, audio)
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got.Text != "Hello there." {
		t.Fatalf("cleaned transcript = %q, want %q", got.Text, "Hello there.")
	}

	base.KeepFillers = true
	raw, err := Transcribe(context.Background(), base, audio)
	if err != nil {
		t.Fatalf("Transcribe (KeepFillers): %v", err)
	}
	if raw.Text != "Um, hello there." {
		t.Fatalf("verbatim transcript = %q, want %q", raw.Text, "Um, hello there.")
	}
}
