package tts

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// TestParadeeInstallAndSynthesizeReal is the end-to-end check for the Paradee
// engine: it runs the real installer (download and SHA-256 verify every pinned
// artifact, build a venv from the pinned requirements, install the spaCy model
// wheel, run the probe synthesis) and then the real paradeeSynth, asserting
// that a WAV comes out.
//
// It is opt-in because it downloads ~22 MB and builds a venv with spaCy and
// onnxruntime:
//
//	PARADEE_TTS_E2E=1 go test ./internal/tts/ -run TestParadeeInstallAndSynthesizeReal -timeout 20m
//
// The cache root is created under /tmp rather than t.TempDir(): on macOS the
// temp dir is deep enough that the espeak data path overflows espeak's fixed
// buffer, which is the failure docs/gotchas/kokoro-espeak-ng-path-limit.md
// describes. A real user's cache sits under a short data root.
func TestParadeeInstallAndSynthesizeReal(t *testing.T) {
	if os.Getenv("PARADEE_TTS_E2E") == "" {
		t.Skip("set PARADEE_TTS_E2E=1 to run the real download + install + synthesis check")
	}
	if _, ok := paradeeManifest.HostRuntime(); !ok {
		t.Skipf("no paradee runtime pinned for %s", Host())
	}
	root, err := os.MkdirTemp("/tmp", "paradee-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	var lastStep string
	inst := &piperInstaller{
		root:     root,
		manifest: paradeeManifest,
		client:   &http.Client{Timeout: 10 * time.Minute},
		progress: func(pct int, step string) { lastStep = step },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := inst.Install(ctx); err != nil {
		t.Fatalf("install failed at step %q: %v", lastStep, err)
	}
	if err := inst.Verify(); err != nil {
		t.Fatalf("verify after install: %v", err)
	}

	dir, err := CacheDirChecked(root, EngineParadee, paradeeManifest.Voice, paradeeManifest.Version)
	if err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "e2e.wav")
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	text := "Paradee is a small voice that runs anywhere. White dwarf stars, if they have a near companion, may then become Type Ia supernovae."
	if err := paradeeSynth(ctx, sup, root, paradeeManifest, "e2e", text, outPath); err != nil {
		t.Fatalf("paradeeSynth: %v", err)
	}
	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte("RIFF")) {
		t.Fatalf("output is not a WAV: %q", b[:min(len(b), 16)])
	}
	// Two sentences at 24 kHz float32 is several seconds of audio; a near-empty
	// file means the G2P or the graph produced nothing usable.
	if len(b) < 24000*2 {
		t.Fatalf("WAV is only %d bytes, expected several seconds of speech", len(b))
	}
}
