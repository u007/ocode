package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
)

// These are the only tests in this package that hit the real Gemini APIs and
// cost money. They are opt-in:
//
//	OCODE_LIVE_MEDIA=1 go test ./internal/tool -run 'TestVideoGenLive|TestAudioGenLive' -timeout 20m -v
//
// The credential is resolved exactly like the tool does it (env, opencode
// config, auth store); no key is ever logged.
func liveMediaConfig(t *testing.T) *config.Config {
	t.Helper()
	if os.Getenv("OCODE_LIVE_MEDIA") == "" {
		t.Skip("set OCODE_LIVE_MEDIA=1 to run the live Gemini media tests (they cost money)")
	}
	_ = auth.LoadStore()
	// The auth provider id is "google"; "gemini" is the tool-facing alias.
	if auth.ResolveKey("google") == "" {
		t.Skip("no Gemini API key resolvable")
	}
	video := config.DefaultVideoGenConfig()
	video.Enabled = true
	audio := config.DefaultAudioGenConfig()
	audio.Enabled = true
	return &config.Config{Ocode: config.OcodeConfig{
		VideoGen: video,
		AudioGen: audio,
	}}
}

func TestVideoGenLive(t *testing.T) {
	cfg := liveMediaConfig(t)
	out := filepath.Join(t.TempDir(), "live.mp4")

	args, _ := json.Marshal(map[string]any{
		"prompt":       "A single red apple on a wooden table, slow push-in, soft daylight.",
		"output_path":  out,
		"aspect_ratio": "16:9",
		"resolution":   "720p",
	})
	got, err := (&VideoGenTool{Config: cfg}).Execute(args)
	if err != nil {
		t.Fatalf("videogen live call failed: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("no video file written: %v (result %s)", err, got)
	}
	if info.Size() == 0 {
		t.Fatalf("video file is empty")
	}
	head := make([]byte, 12)
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open video: %v", err)
	}
	defer f.Close()
	if _, err := f.Read(head); err != nil {
		t.Fatalf("read video header: %v", err)
	}
	// An ISO-BMFF mp4 has a box type in bytes 4-8; accept any non-text box.
	if !strings.Contains(string(head[4:8]), "ftyp") {
		t.Logf("note: header %q is not ftyp; server may return a different mp4 flavour", head[4:8])
	}
	t.Logf("videogen OK: %d bytes, header=%q, result=%s", info.Size(), head[4:8], got)
}

func TestAudioGenLive(t *testing.T) {
	cfg := liveMediaConfig(t)
	out := filepath.Join(t.TempDir(), "live.wav")

	args, _ := json.Marshal(map[string]any{
		"text":        "Hello from ocode. This is a live text to speech check.",
		"voice":       "Kore",
		"style":       "cheerful and friendly",
		"output_path": out,
	})
	got, err := (&AudioGenTool{Config: cfg}).Execute(args)
	if err != nil {
		t.Fatalf("audiogen live call failed: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("no audio file written: %v (result %s)", err, got)
	}
	if info.Size() == 0 {
		t.Fatalf("audio file is empty")
	}
	head := make([]byte, 4)
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open audio: %v", err)
	}
	defer f.Close()
	if _, err := f.Read(head); err != nil {
		t.Fatalf("read audio header: %v", err)
	}
	if string(head) != "RIFF" {
		t.Errorf("audio header = %q, want RIFF (the API returned non-WAV bytes)", head)
	}
	t.Logf("audiogen OK: %d bytes, result=%s", info.Size(), got)
}
