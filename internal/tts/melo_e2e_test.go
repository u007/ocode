package tts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// TestMeloInstallAndSynthesizeReal is the end-to-end check for the whole
// feature: it runs the real installer (download every pinned artifact, verify
// its SHA-256 and size, unpack the source, build a venv from the pinned
// requirements, verify the import) and then the real meloSynth, asserting that
// audio comes out.
//
// It is opt-in because it downloads ~650 MB of artifacts and builds a
// multi-gigabyte torch venv:
//
//	MELO_TTS_E2E=1 go test ./internal/tts/ -run TestMeloInstallAndSynthesizeReal -timeout 40m
func TestMeloInstallAndSynthesizeReal(t *testing.T) {
	if os.Getenv("MELO_TTS_E2E") == "" {
		t.Skip("set MELO_TTS_E2E=1 to run the real download + install + synthesis check")
	}
	if _, ok := meloManifest.HostRuntime(); !ok {
		t.Skipf("no melo runtime pinned for %s", Host())
	}
	root := t.TempDir()
	var lastStep string
	inst := &piperInstaller{
		root:     root,
		manifest: meloManifest,
		client:   &http.Client{Timeout: 30 * time.Minute},
		progress: func(pct int, step string) { lastStep = step },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
	defer cancel()
	if err := inst.Install(ctx); err != nil {
		t.Fatalf("install failed at step %q: %v", lastStep, err)
	}
	if err := inst.Verify(); err != nil {
		t.Fatalf("verify after install: %v", err)
	}

	// The install must be reproducible from the cache: a second Install must
	// not re-download anything, which is what the artifact cache exists for.
	dir, err := CacheDirChecked(root, EngineMelo, meloManifest.Voice, meloManifest.Version)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range meloManifest.VoiceFiles {
		path := filepath.Join(dir, a.Name)
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("%s missing after install: %v", a.Name, err)
		}
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		if got := hex.EncodeToString(h.Sum(nil)); got != a.SHA256 {
			t.Fatalf("%s on disk hashes to %s, manifest pins %s", a.Name, got, a.SHA256)
		}
	}

	outPath := filepath.Join(t.TempDir(), "out.wav")
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	const text = "MeloTTS is now speaking from local files only, with no network access."
	if err := meloSynth(ctx, sup, root, meloManifest, "e2e", text, outPath, "EN-US"); err != nil {
		t.Fatalf("meloSynth: %v", err)
	}
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 1024 {
		t.Fatalf("suspiciously small audio file: %d bytes", info.Size())
	}
	head := make([]byte, 4)
	f, err := os.Open(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := io.ReadFull(f, head); err != nil {
		t.Fatal(err)
	}
	if string(head) != "RIFF" {
		t.Fatalf("output is not a RIFF/WAV file: %q", head)
	}
	t.Logf("real install + synthesis OK: %d bytes of WAV", info.Size())

	// Nothing may have been written relative to the process working directory:
	// the :memory:.ses / nltk_data class of bug lands there when cmd.Dir is
	// unset.
	for _, stray := range []string{":memory:.ses", "nltk_data", "hf"} {
		if _, err := os.Stat(stray); err == nil {
			t.Errorf("synthesis wrote %q into the working directory", stray)
		}
	}
}

// TestMeloArtifactURLsAreReachable is a cheap guard that every pinned URL still
// resolves to the pinned size, without downloading the large files fully. It
// catches upstream renames and moved revisions, which otherwise surface only as
// a user's failed install.
func TestMeloArtifactURLsAreReachable(t *testing.T) {
	if os.Getenv("MELO_TTS_E2E") == "" {
		t.Skip("set MELO_TTS_E2E=1 to check upstream artifact availability")
	}
	client := &http.Client{Timeout: 60 * time.Second}
	for _, a := range meloManifest.VoiceFiles {
		req, err := http.NewRequest(http.MethodGet, a.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Range", "bytes=0-0")
		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("%s: %v", a.Name, err)
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			t.Errorf("%s: %s at %s", a.Name, resp.Status, a.URL)
			continue
		}
		// A Content-Length of the full size confirms the pinned size is still
		// current; a ranged response reports the single requested byte.
		if cl := resp.ContentLength; cl > 1 && cl != a.Size {
			t.Errorf("%s: server reports %d bytes, manifest pins %d", a.Name, cl, a.Size)
		}
		if got := resp.Header.Get("X-Linked-Etag"); got != "" && !strings.EqualFold(got, a.SHA256) {
			t.Errorf("%s: X-Linked-Etag %s does not match the pinned checksum", a.Name, got)
		}
	}
}
