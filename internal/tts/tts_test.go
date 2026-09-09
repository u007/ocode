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

func TestCatalogKeepsLocalEnginesExplicitlyUnavailable(t *testing.T) {
	for _, engine := range Catalog() {
		if engine.ID == EngineBrowserNative {
			continue
		}
		if engine.Availability != AvailabilityUnavailable || engine.Reason == "" {
			t.Fatalf("engine %q must be explicitly unavailable with a reason: %#v", engine.ID, engine)
		}
	}
}

func TestSupervisorSelectionInvalidatesPreviousPlayback(t *testing.T) {
	s := NewSupervisor(Config{Engine: EngineBrowserNative, Mode: PlaybackManual})

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
	s := NewSupervisor(Config{Engine: EngineBrowserNative, Mode: PlaybackManual})
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

func TestInstallVerifiedIsAtomicAndRejectsMismatch(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	dst := filepath.Join(dir, "nested", "installed")
	if err := os.WriteFile(src, []byte("speech"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallVerified(src, dst, "bad"); err == nil {
		t.Fatal("expected checksum mismatch")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("mismatched install created destination: %v", err)
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
