package tool

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
)

// fakeMediaBytes stands in for an mp4/wav payload: the tools copy the decoded
// base64 straight to disk and never inspect the container.
var fakeMediaBytes = []byte("fake-media-payload")

// interactionOK serves a successful Interactions API response and records the
// decoded request body into capture when it is non-nil.
func interactionOK(t *testing.T, partType, mime string, capture map[string]any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/interactions" {
			t.Errorf("path = %q, want /v1beta/interactions", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("request is not JSON: %v", err)
		}
		for k, v := range req {
			capture[k] = v
		}
		if got := r.Header.Get("x-goog-api-key"); got != "test-key" {
			t.Errorf("x-goog-api-key = %q, want test-key", got)
		}
		if req["model"] == nil || req["model"] == "" {
			t.Errorf("request missing model: %s", body)
		}
		resp := map[string]any{
			"status": "completed",
			"steps": []any{
				map[string]any{"type": "user_input", "content": []any{map[string]any{"type": "text", "text": "prompt"}}},
				map[string]any{"type": "model_output", "content": []any{
					map[string]any{
						"type":      partType,
						"mime_type": mime,
						"data":      base64.StdEncoding.EncodeToString(fakeMediaBytes),
					},
				}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// mediaTestOutput returns a relative output path plus a cleanup func. Relative
// paths stay inside the confined roots, so the tool writes exactly where the
// test expects instead of falling back to the working directory.
func mediaTestOutput(t *testing.T, name string) (string, func()) {
	t.Helper()
	dir := filepath.Join("testout", t.Name())
	out := filepath.Join(dir, name)
	return out, func() { _ = os.RemoveAll(dir) }
}

func newMediaTestConfig() *config.Config {
	return &config.Config{Ocode: config.OcodeConfig{
		VideoGen: config.VideoGenConfig{Enabled: true},
		AudioGen: config.AudioGenConfig{Enabled: true, Voice: "Kore"},
	}}
}

func TestVideoGenSavesFileAndSendsOptions(t *testing.T) {
	gotBody := map[string]any{}
	srv := httptest.NewServer(interactionOK(t, "video", "video/mp4", gotBody))
	defer srv.Close()

	out, cleanup := mediaTestOutput(t, "clip.mp4")
	defer cleanup()
	args, _ := json.Marshal(map[string]any{
		"prompt":       "a marble rolling on a track",
		"api_key":      "test-key",
		"base_url":     srv.URL,
		"output_path":  out,
		"aspect_ratio": "9:16",
		"resolution":   "1080p",
	})

	got, err := (&VideoGenTool{Config: newMediaTestConfig()}).Execute(args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var summary map[string]any
	if err := json.Unmarshal([]byte(got), &summary); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, got)
	}
	path, _ := summary["path"].(string)
	if !strings.HasSuffix(path, filepath.Join("testout", t.Name(), "clip.mp4")) {
		t.Errorf("path = %q, want suffix testout/%s/clip.mp4", path, t.Name())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("saved file: %v", err)
	}
	if string(data) != string(fakeMediaBytes) {
		t.Errorf("saved bytes = %q", data)
	}

	rf, _ := gotBody["response_format"].(map[string]any)
	if rf["type"] != "video" || rf["aspect_ratio"] != "9:16" || rf["resolution"] != "1080p" {
		t.Errorf("response_format = %#v", rf)
	}
}

func TestVideoGenDefaultsModelAndFormat(t *testing.T) {
	gotBody := map[string]any{}
	srv := httptest.NewServer(interactionOK(t, "video", "video/mp4", gotBody))
	defer srv.Close()

	out, cleanup := mediaTestOutput(t, "default.mp4")
	defer cleanup()
	args, _ := json.Marshal(map[string]any{
		"prompt":      "a city at night",
		"api_key":     "test-key",
		"base_url":    srv.URL,
		"output_path": out,
	})
	if _, err := (&VideoGenTool{Config: newMediaTestConfig()}).Execute(args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotBody["model"] != defaultVideoGenModel {
		t.Errorf("model = %v, want %s", gotBody["model"], defaultVideoGenModel)
	}
	rf, _ := gotBody["response_format"].(map[string]any)
	if rf["aspect_ratio"] != "16:9" || rf["resolution"] != "720p" {
		t.Errorf("defaults = %#v", rf)
	}
}

func TestVideoGenDisabledAndValidation(t *testing.T) {
	disabled := &config.Config{Ocode: config.OcodeConfig{VideoGen: config.VideoGenConfig{Enabled: false}}}
	_, err := (&VideoGenTool{Config: disabled}).Execute(json.RawMessage(`{"prompt":"x"}`))
	var noticed *NoticedError
	if !errors.As(err, &noticed) || !strings.Contains(noticed.Notice, "/video enable") {
		t.Fatalf("disabled error = %v, want NoticedError mentioning /video enable", err)
	}

	enabled := newMediaTestConfig()
	for _, tc := range []map[string]any{
		{"prompt": "x", "aspect_ratio": "4:3"},
		{"prompt": "x", "resolution": "8k"},
		{"prompt": ""},
	} {
		args, _ := json.Marshal(tc)
		if _, err := (&VideoGenTool{Config: enabled}).Execute(args); err == nil {
			t.Errorf("args %v: expected validation error", tc)
		}
	}
}

func TestAudioGenSingleSpeaker(t *testing.T) {
	gotBody := map[string]any{}
	srv := httptest.NewServer(interactionOK(t, "audio", "audio/wav", gotBody))
	defer srv.Close()

	out, cleanup := mediaTestOutput(t, "speech.wav")
	defer cleanup()
	args, _ := json.Marshal(map[string]any{
		"text":        "Have a wonderful day!",
		"voice":       "Puck",
		"style":       "cheerful",
		"api_key":     "test-key",
		"base_url":    srv.URL,
		"output_path": out,
	})
	got, err := (&AudioGenTool{Config: newMediaTestConfig()}).Execute(args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	sc, _ := gotBody["generation_config"].(map[string]any)
	speech, _ := sc["speech_config"].([]any)
	if len(speech) != 1 {
		t.Fatalf("speech_config = %#v, want one entry", speech)
	}
	if voice := speech[0].(map[string]any)["voice"]; voice != "Puck" {
		t.Errorf("voice = %v, want Puck", voice)
	}

	input, _ := gotBody["input"].([]any)
	content := input[0].(map[string]any)["content"].([]any)
	part := content[0].(map[string]any)
	if part["text"] != "Have a wonderful day!" {
		t.Errorf("text = %v", part["text"])
	}
	anns, _ := part["annotations"].([]any)
	if style := anns[0].(map[string]any)["style"]; style != "cheerful" {
		t.Errorf("style = %v", style)
	}

	if !strings.Contains(got, `"voice": "Puck"`) {
		t.Errorf("result missing voice: %s", got)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("saved file: %v", err)
	}
}

func TestAudioGenMultiSpeaker(t *testing.T) {
	gotBody := map[string]any{}
	srv := httptest.NewServer(interactionOK(t, "audio", "audio/wav", gotBody))
	defer srv.Close()

	out, cleanup := mediaTestOutput(t, "dialogue.wav")
	defer cleanup()
	args, _ := json.Marshal(map[string]any{
		"turns": []map[string]any{
			{"speaker": "Joe", "text": "How's it going?", "style": "friendly"},
			{"speaker": "Jane", "text": "Not too bad."},
		},
		"speakers": []map[string]any{
			{"name": "Joe", "voice": "Puck"},
			{"name": "Jane", "voice": "Kore"},
		},
		"api_key":     "test-key",
		"base_url":    srv.URL,
		"output_path": out,
	})
	got, err := (&AudioGenTool{Config: newMediaTestConfig()}).Execute(args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	sc, _ := gotBody["generation_config"].(map[string]any)
	speech, _ := sc["speech_config"].(map[string]any)
	speakers, _ := speech["speakers"].([]any)
	if len(speakers) != 2 {
		t.Fatalf("speakers = %#v, want 2", speakers)
	}
	// Deterministic order: sorted by speaker name.
	if name := speakers[0].(map[string]any)["speaker"]; name != "Jane" {
		t.Errorf("first speaker = %v, want Jane (sorted)", name)
	}

	input, _ := gotBody["input"].([]any)
	content := input[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content turns = %d, want 2", len(content))
	}
	first := content[0].(map[string]any)
	ann, _ := first["annotations"].([]any)
	meta := ann[0].(map[string]any)
	if meta["speaker"] != "Joe" || meta["style"] != "friendly" {
		t.Errorf("turn annotation = %#v", meta)
	}
	if !strings.Contains(got, `"speakers"`) {
		t.Errorf("result missing speakers: %s", got)
	}
}

func TestAudioGenValidation(t *testing.T) {
	enabled := newMediaTestConfig()
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"missing text", map[string]any{}, "text is required"},
		{"turns without speakers", map[string]any{"turns": []map[string]any{{"speaker": "A", "text": "hi"}}}, "speakers is required"},
		{"unknown turn speaker", map[string]any{
			"turns":    []map[string]any{{"speaker": "A", "text": "hi"}},
			"speakers": []map[string]any{{"name": "B", "voice": "Kore"}},
		}, "no matching entry"},
		{"unused speaker", map[string]any{
			"turns":    []map[string]any{{"speaker": "A", "text": "hi"}},
			"speakers": []map[string]any{{"name": "A", "voice": "Kore"}, {"name": "B", "voice": "Puck"}},
		}, "never used"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args, _ := json.Marshal(tc.args)
			_, err := (&AudioGenTool{Config: enabled}).Execute(args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}

	_, err := (&AudioGenTool{Config: &config.Config{}}).Execute(json.RawMessage(`{"text":"hi"}`))
	var noticed *NoticedError
	if !errors.As(err, &noticed) || !strings.Contains(noticed.Notice, "/audio enable") {
		t.Fatalf("disabled error = %v, want NoticedError mentioning /audio enable", err)
	}
}

// The credential lookup must use the auth provider id ("google"), never the
// tool-facing alias ("gemini"): ResolveKey("gemini") finds no provider and
// silently returns an empty key, which made live calls fail even with a stored
// Google key.
func TestGeminiGenerativeProviderID(t *testing.T) {
	if auth.FindProvider("google") == nil {
		t.Fatal(`auth provider "google" is missing; the tools resolve credentials by that id`)
	}
	if auth.FindProvider("gemini") != nil {
		t.Fatal(`auth provider "gemini" exists; update geminiGenerativeCredentials and this test`)
	}
}

func TestGeminiInteractionErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"bad prompt"}}`))
	}))
	defer srv.Close()

	args, _ := json.Marshal(map[string]any{
		"prompt":   "x",
		"api_key":  "test-key",
		"base_url": srv.URL,
	})
	if _, err := (&VideoGenTool{Config: newMediaTestConfig()}).Execute(args); err == nil ||
		!strings.Contains(err.Error(), "gemini interactions error (400)") {
		t.Fatalf("error = %v, want a 400 error", err)
	}
}

// storedKeyEnv gives every provider key source a stored value and a private,
// empty home. A leak then has a real key to send, and no earlier failure
// (missing key, missing profile) can mask it.
func storedKeyEnv(t *testing.T) {
	t.Helper()
	setHomeTree(t, t.TempDir())
	t.Setenv("OPENCODE_AUTH_TOKEN", "")
	t.Setenv("OCODE_PROFILE", "")
	t.Setenv("GOOGLE_API_KEY", "stored-google-secret")
	t.Setenv("OPENAI_API_KEY", "stored-openai-secret")
	t.Setenv("ANTHROPIC_API_KEY", "stored-anthropic-secret")
}

// countingServer counts every request it receives. A stored key on the wire
// shows up as a header value starting with "stored-". When location is set, the
// server answers with status and that Location instead of 200.
func countingServer(t *testing.T, hits *atomic.Int32, status int, location string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		for name, vals := range r.Header {
			for _, v := range vals {
				if strings.Contains(v, "stored-") {
					t.Errorf("stored key sent in header %s", name)
				}
			}
		}
		if location != "" {
			w.Header().Set("Location", location)
			w.WriteHeader(status)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newGenTool returns the named generative tool with its enable flag set.
func newGenTool(t *testing.T, name string) interface {
	Execute(json.RawMessage) (string, error)
} {
	t.Helper()
	switch name {
	case "imagegen":
		return &ImageGenTool{}
	case "videogen":
		return &VideoGenTool{Config: newMediaTestConfig()}
	case "audiogen":
		return &AudioGenTool{Config: newMediaTestConfig()}
	}
	t.Fatalf("unknown generative tool %q", name)
	return nil
}

// TestBaseURLWithoutAPIKeyNeverSendsStoredKey: a caller-chosen base_url with no
// api_key is refused before any key is resolved, so the stored key never reaches
// the model-chosen host. The anthropic row is not in imgRegistry but is a real
// auth provider: before the fix its stored key went to the base_url.
func TestBaseURLWithoutAPIKeyNeverSendsStoredKey(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"videogen", "videogen", map[string]any{"prompt": "x"}},
		{"audiogen", "audiogen", map[string]any{"text": "hello"}},
		{"imagegen gemini", "imagegen", map[string]any{"prompt": "x", "provider": "gemini"}},
		{"imagegen openai", "imagegen", map[string]any{"prompt": "x", "provider": "openai"}},
		{"imagegen anthropic", "imagegen", map[string]any{"prompt": "x", "provider": "anthropic"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			storedKeyEnv(t)
			var hits atomic.Int32
			srv := countingServer(t, &hits, http.StatusOK, "")
			args := tc.args
			args["base_url"] = srv.URL
			raw, _ := json.Marshal(args)

			_, err := newGenTool(t, tc.tool).Execute(raw)
			var noticed *NoticedError
			if !errors.As(err, &noticed) || !strings.Contains(noticed.Notice, "custom base_url requires an explicit api_key") {
				t.Fatalf("error = %v, want the explicit-api_key refusal", err)
			}
			if n := hits.Load(); n != 0 {
				t.Fatalf("%d request(s) reached the custom base_url", n)
			}
		})
	}
}

// TestCredentialedRequestsDoNotFollowRedirects: with an explicit api_key the
// request goes out, but a 302 from the endpoint must not be followed, because Go
// copies custom headers such as x-goog-api-key onto the redirect target.
func TestCredentialedRequestsDoNotFollowRedirects(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"videogen interactions", "videogen", map[string]any{"prompt": "x"}},
		{"audiogen interactions", "audiogen", map[string]any{"text": "hello"}},
		{"imagegen gemini generateContent", "imagegen", map[string]any{"prompt": "x", "provider": "gemini"}},
		{"imagegen openai images", "imagegen", map[string]any{"prompt": "x", "provider": "openai"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			storedKeyEnv(t)
			var landed atomic.Int32
			target := countingServer(t, &landed, http.StatusOK, "")
			redirector := countingServer(t, new(atomic.Int32), http.StatusFound, target.URL+"/landed")
			args := tc.args
			args["base_url"] = redirector.URL
			args["api_key"] = "explicit-test-key"
			raw, _ := json.Marshal(args)

			if _, err := newGenTool(t, tc.tool).Execute(raw); err == nil {
				t.Fatal("Execute succeeded on a 302; want an error")
			}
			if n := landed.Load(); n != 0 {
				t.Fatalf("redirect target received %d request(s); credentialed requests must not follow redirects", n)
			}
		})
	}
}
