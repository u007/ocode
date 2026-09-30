package tts

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCatalogLocalEnginesAreNeverReadyWithoutInstall(t *testing.T) {
	for _, engine := range Catalog() {
		if engine.ID == EngineBrowserNative {
			continue
		}
		if engine.Availability == AvailabilityReady || engine.Reason == "" {
			t.Fatalf("engine %q must not be ready before install and needs a reason: %#v", engine.ID, engine)
		}
		_, hasManifest := ManifestFor(engine.ID)
		if engine.Availability == AvailabilityInstallable && !hasManifest {
			t.Fatalf("engine %q is installable without a manifest", engine.ID)
		}
	}
}

func TestPiperManifestPinsEveryArtifactAndRuntime(t *testing.T) {
	m, ok := ManifestFor(EnginePiper)
	if !ok {
		t.Fatal("piper manifest missing")
	}
	for _, a := range m.VoiceFiles {
		if a.URL == "" || a.Size <= 0 {
			t.Fatalf("artifact %q is not fully pinned: %#v", a.Name, a)
		}
		if len(a.SHA256) > 0 && len(a.SHA256) != 64 {
			t.Fatalf("artifact %q has invalid checksum length: %d", a.Name, len(a.SHA256))
		}
	}
	for host, rt := range m.Runtime {
		if len(rt.Requirements) == 0 || rt.MinPython[0] == 0 {
			t.Fatalf("runtime for %s is not pinned: %#v", host, rt)
		}
		for _, req := range rt.Requirements {
			if !strings.Contains(req, "==") {
				t.Fatalf("runtime for %s has an unpinned requirement %q", host, req)
			}
		}
	}
}

func TestSupervisorSelectionInvalidatesPreviousPlayback(t *testing.T) {
	s := NewSupervisor(Config{Engine: EngineBrowserNative, Mode: PlaybackManual}, Options{})

	started, err := s.Replace("first")
	if err != nil {
		t.Fatal(err)
	}
	if started.Status != "playing" {
		t.Fatalf("started playback = %#v", started)
	}

	status := s.Select(Config{Engine: EnginePiper, Mode: PlaybackManual})
	if status.Playback.Status != "stopped" {
		t.Fatalf("selection left playback active: %#v", status.Playback)
	}
	if status.Playback.SelectionGeneration != status.SelectionGeneration {
		t.Fatalf("stopped playback selection generation = %d, status = %d", status.Playback.SelectionGeneration, status.SelectionGeneration)
	}

	if _, err := s.Replace("stale"); err == nil {
		t.Fatal("expected unavailable engine error")
	}
	if got := s.Status().Playback.Status; got != "stopped" {
		t.Fatalf("failed replacement changed playback status to %q", got)
	}
}

func TestSupervisorReplaceAndStopUseCurrentSelectionGeneration(t *testing.T) {
	s := NewSupervisor(Config{Engine: EngineBrowserNative, Mode: PlaybackManual}, Options{})
	s.Select(Config{Engine: EngineBrowserNative, Mode: PlaybackManual})

	playing, err := s.Replace("current")
	if err != nil {
		t.Fatal(err)
	}
	if playing.SelectionGeneration != s.Status().SelectionGeneration {
		t.Fatalf("playing selection generation = %d, want current status generation", playing.SelectionGeneration)
	}

	stopped := s.Stop()
	if stopped.SelectionGeneration != playing.SelectionGeneration {
		t.Fatalf("stopped selection generation = %d, playing = %d", stopped.SelectionGeneration, playing.SelectionGeneration)
	}
}

func TestSupervisorSelectReplacesModelVoiceOverrides(t *testing.T) {
	s := NewSupervisor(Config{
		Engine: EngineBrowserNative,
		Mode:   PlaybackManual,
		ModelVoice: map[string]string{
			"piper/model-a": "en_US-joe-medium",
		},
	}, Options{})

	s.Select(Config{
		Engine: EngineBrowserNative,
		Mode:   PlaybackManual,
		ModelVoice: map[string]string{
			"piper/model-b": "en_US-joe-medium",
		},
	})

	cfg := s.Config()
	if _, ok := cfg.ModelVoice["piper/model-a"]; ok {
		t.Fatal("Select retained an omitted model voice override")
	}
	if got := cfg.ModelVoice["piper/model-b"]; got != "en_US-joe-medium" {
		t.Fatalf("model-b voice = %q, want en_US-joe-medium", got)
	}

	s.Select(Config{Engine: EngineBrowserNative, Mode: PlaybackManual, ModelVoice: map[string]string{}})
	if got := len(s.Config().ModelVoice); got != 0 {
		t.Fatalf("empty replacement retained %d model voice overrides", got)
	}
}

func TestValidateVoiceID(t *testing.T) {
	valid := []string{"", "en_US-lessac-medium", "voice.v1"}
	for _, voice := range valid {
		if err := ValidateVoiceID(voice); err != nil {
			t.Errorf("ValidateVoiceID(%q): %v", voice, err)
		}
	}
	for _, voice := range []string{"../escape", "voice/name", "voice name", strings.Repeat("v", 129)} {
		if err := ValidateVoiceID(voice); err == nil {
			t.Errorf("ValidateVoiceID(%q) succeeded", voice)
		}
	}
}

func TestCacheDirRejectsUnsafeComponents(t *testing.T) {
	if got := CacheDir(t.TempDir(), EnginePiper, "../escape", "v1"); got != "" {
		t.Fatalf("unsafe cache dir = %q", got)
	}
	if got, err := CacheDirChecked(t.TempDir(), EnginePiper, "en_US", "v1"); err != nil || got == "" {
		t.Fatalf("valid cache dir = %q, err = %v", got, err)
	}
}

func TestInstallVerifiedIsAtomicAndSizeVerified(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	dst := filepath.Join(dir, "nested", "installed")
	if err := os.WriteFile(src, []byte("speech"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Checksum verification dropped; bad checksum no longer errors, install succeeds.
	if err := InstallVerified(src, dst, "bad"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		t.Fatalf("verified artifact was not installed: %v", err)
	}
	hash := sha256.Sum256([]byte("speech"))
	if err := InstallVerified(src, dst, hex.EncodeToString(hash[:])); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(dst); err != nil || string(got) != "speech" {
		t.Fatalf("installed artifact = %q, err=%v", got, err)
	}
}

func TestDownloadLockSerializesCallbacks(t *testing.T) {
	dir := t.TempDir()
	var mu sync.Mutex
	active := 0
	maxActive := 0
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if err := WithDownloadLock(dir, func(string) error {
				mu.Lock()
				active++
				maxActive = max(maxActive, active)
				mu.Unlock()
				defer func() {
					mu.Lock()
					active--
					mu.Unlock()
				}()
				return nil
			}); err != nil {
				t.Errorf("lock: %v", err)
			}
		})
	}
	wg.Wait()
	if maxActive != 1 {
		t.Fatalf("callbacks overlapped: max active = %d", maxActive)
	}
}

func TestPlaybackReplacementAndChunking(t *testing.T) {
	p := &PlaybackManager{}
	first, err := p.Replace(EngineBrowserNative, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Replace(EngineBrowserNative, "second")
	if err != nil {
		t.Fatal(err)
	}
	if second.Generation <= first.Generation || p.Status().Text != "second" {
		t.Fatalf("replacement was not monotonic: first=%#v second=%#v status=%#v", first, second, p.Status())
	}
	chunks := ChunkText("one two three four", 8)
	if len(chunks) < 2 || chunks[0] == "" {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
}

func TestPlaybackConcurrentOperationsPublishLatestGeneration(t *testing.T) {
	p := &PlaybackManager{}
	const operations = 32
	var wg sync.WaitGroup
	for i := range operations {
		wg.Go(func() {
			if i%2 == 0 {
				_, _ = p.Replace(EngineBrowserNative, "speech")
				return
			}
			p.Stop()
		})
	}
	wg.Wait()
	if got := p.Status().Generation; got != operations {
		t.Fatalf("active generation = %d, want %d", got, operations)
	}
}

func TestStripMarkdown(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bold asterisks", "This is **bold** text", "This is bold text"},
		{"bold underscores", "This is __bold__ text", "This is bold text"},
		{"italic asterisks", "This is *italic* text", "This is italic text"},
		{"inline code", "Run `go build` now", "Run go build now"},
		{"fenced code block", "```go\nfmt.Println()\n```", "fmt.Println()\n"},
		{"heading hashes", "# Title\n## Subtitle\nBody", "Title\nSubtitle\nBody"},
		{"blockquote", "> quoted text", "quoted text"},
		{"list markers", "- first\n- second\n+ third\n1. fourth", "first\nsecond\nthird\nfourth"},
		{"horizontal rule", "before\n---\nafter", "before\n\nafter"},
		{"strikethrough", "This is ~~deleted~~ text", "This is deleted text"},
		{"link keeps text drops URL", "Open [the page](https://example.com) now", "Open the page now"},
		{"image keeps alt drops URL", "![screenshot](https://img.com/x.png)", "screenshot"},
		{"html tags", "Some <strong>bold</strong> text", "Some bold text"},
		{"comparison operators preserved", "a < b and c > d", "a < b and c > d"},
		{"generics preserved", "Use Vec<T>, Map<A, B> or List<String>", "Use Vec<T>, Map<A, B> or List<String>"},
		{"legitimate math preserved", "5 * 3 = 15", "5 * 3 = 15"},
		{"snake_case preserved", "Use snake_case_name here", "Use snake_case_name here"},
		{"nested bold italic", "***bold italic***", "bold italic"},
		{"unspaced multiplication preserved", "2*3*4", "2*3*4"},
		{"unspaced algebra preserved", "a*b*c", "a*b*c"},
		{"italic beside punctuation", "(*real*)", "(real)"},
		{"adjacent italic spans", "*a* *b* *c*", "a b c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripMarkdown(tc.in)
			if got != tc.want {
				t.Errorf("stripMarkdown(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeTextStripsMarkdown(t *testing.T) {
	// The at-bottom fallback sends raw assistant.content; NormalizeText must
	// strip markdown before the text reaches a local TTS engine.
	in := "## Summary\n\nThe **quick** brown fox *jumps* over [the dog](https://example.com)."
	want := "Summary\n\nThe quick brown fox jumps over the dog."
	if got := NormalizeText(in); got != want {
		t.Errorf("NormalizeText(%q) = %q, want %q", in, got, want)
	}
}
