package tts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// TestSpeakSampleRejectsUnknownEngine covers the no-manifest branch.
func TestSpeakSampleRejectsUnknownEngine(t *testing.T) {
	s := NewSupervisor(DefaultConfig(), Options{Root: t.TempDir()})
	if _, err := s.SpeakSample("no-such-engine", "", ""); err == nil {
		t.Fatal("SpeakSample accepted an unknown engine")
	}
}

// TestSpeakSampleRejectsNotInstalled covers the availability != ready branch.
// With no cache root the engine can never reach the installed state.
func TestSpeakSampleRejectsNotInstalled(t *testing.T) {
	s := NewSupervisor(DefaultConfig(), Options{Root: t.TempDir()})
	for _, engine := range []EngineID{EnginePiper, EngineKokoro, EngineKyutai, EngineMelo, EngineParadee} {
		if _, err := s.SpeakSample(engine, "", ""); err == nil {
			t.Fatalf("SpeakSample accepted uninstalled engine %q", engine)
		}
	}
}

// TestSpeakSampleRejectsInvalidVoice covers a voice that fails ID validation.
func TestSpeakSampleRejectsInvalidVoice(t *testing.T) {
	s := NewSupervisor(DefaultConfig(), Options{Root: t.TempDir()})
	if _, err := s.SpeakSample(EngineKokoro, "../escape", ""); err == nil {
		t.Fatal("SpeakSample accepted a path-traversal voice")
	}
}

// TestSpeakSampleRejectsVoiceNotInManifest covers a voice that is well-formed
// but not offered by the engine's manifest.
func TestSpeakSampleRejectsVoiceNotInManifest(t *testing.T) {
	s := NewSupervisor(DefaultConfig(), Options{Root: t.TempDir()})
	// Piper has exactly one voice; anything else is rejected.
	if _, err := s.SpeakSample(EnginePiper, "af_sarah", ""); err == nil {
		t.Fatal("SpeakSample accepted a voice not in piper's manifest")
	}
}

// TestSpeakSampleRejectsTooLongText caps caller-supplied sample text. The
// length check runs before the supervisor-wiring and availability checks, so
// an over-long text is rejected with a length error even when nothing is
// installed, and an exactly-limit text is not rejected for its length.
func TestSpeakSampleRejectsTooLongText(t *testing.T) {
	s := NewSupervisor(DefaultConfig(), Options{Root: t.TempDir()})
	const tooLong = "sample text is too long"
	if _, err := s.SpeakSample(EngineKokoro, "", strings.Repeat("a", maxSampleTextRunes+1)); err == nil || !strings.Contains(err.Error(), tooLong) {
		t.Fatalf("SpeakSample did not reject over-long sample text: %v", err)
	}
	if _, err := s.SpeakSample(EngineKokoro, "", strings.Repeat("a", maxSampleTextRunes)); err != nil && strings.Contains(err.Error(), tooLong) {
		t.Fatalf("SpeakSample rejected exactly-limit sample text for length: %v", err)
	}
}

// TestSampleAudioPathOnlyAcceptsSampleIds guards the sample file resolver
// against guessed or traversal ids.
func TestSampleAudioPathOnlyAcceptsSampleIds(t *testing.T) {
	s := NewSupervisor(DefaultConfig(), Options{Root: t.TempDir()})
	if _, err := s.SampleAudioPath("nope"); err == nil {
		t.Fatal("SampleAudioPath accepted a non-sample id")
	}
	if _, err := s.SampleAudioPath("sample-"); err == nil {
		t.Fatal("SampleAudioPath accepted an empty nonce")
	}
	if _, err := s.SampleAudioPath("sample-../escape"); err == nil {
		t.Fatal("SampleAudioPath accepted a traversal nonce")
	}
	if _, err := s.SampleAudioPath("sample-abc_123-"); err != nil {
		t.Fatalf("SampleAudioPath rejected a valid nonce: %v", err)
	}
}

// TestValidSampleNonce covers the nonce character validator.
func TestValidSampleNonce(t *testing.T) {
	for _, bad := range []string{"", "a/b", "a b", "a;b", "a`b", "a$b"} {
		if validSampleNonce(bad) {
			t.Fatalf("validSampleNonce accepted %q", bad)
		}
	}
	for _, good := range []string{"abc", "ABC", "123", "a-b_c", "a1B2-c3D"} {
		if !validSampleNonce(good) {
			t.Fatalf("validSampleNonce rejected %q", good)
		}
	}
}

// TestSpeakSampleHappyPath uses the test-only seams to exercise synthesis
// without a real Python venv: an installed engine + a stubbed synth produces a
// wav, and the returned nonce points at a real, serveable file.
func TestSpeakSampleHappyPath(t *testing.T) {
	tmp := t.TempDir()
	s := NewSupervisor(DefaultConfig(), Options{Root: tmp, ProcSup: tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})})
	origAvail, origSynth := AvailabilitySeam, SynthSeam
	AvailabilitySeam = func(EngineID) (Availability, bool) { return AvailabilityReady, true }
	SynthSeam = func(_ context.Context, _ *tool.ProcessSupervisor, root string, _ Manifest, id, _ string, outPath, _ string) error {
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(outPath, []byte("RIFF-stub-wav"), 0o644)
	}
	t.Cleanup(func() { AvailabilitySeam, SynthSeam = origAvail, origSynth })
	res, err := s.SpeakSample(EngineKokoro, "af_sarah", "")
	if err != nil {
		t.Fatalf("SpeakSample (stubbed) failed: %v", err)
	}
	if !strings.HasPrefix(res.AudioID, "sample-") {
		t.Fatalf("bad audio id: %q", res.AudioID)
	}
	if res.Voice != "af_sarah" {
		t.Fatalf("voice not echoed: %q", res.Voice)
	}
	path, err := s.SampleAudioPath(res.AudioID)
	if err != nil {
		t.Fatalf("SampleAudioPath failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "RIFF-stub-wav" {
		t.Fatalf("sample wav not served: %v %q", err, data)
	}
	// default text echoed
	if res.Text != defaultSampleText {
		t.Fatalf("default text not echoed: %q", res.Text)
	}
}

// TestSpeakSampleReapsPreviousSamples proves files don't accumulate.
func TestSpeakSampleReapsPreviousSamples(t *testing.T) {
	tmp := t.TempDir()
	s := NewSupervisor(DefaultConfig(), Options{Root: tmp, ProcSup: tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})})
	origAvail, origSynth := AvailabilitySeam, SynthSeam
	AvailabilitySeam = func(EngineID) (Availability, bool) { return AvailabilityReady, true }
	SynthSeam = func(_ context.Context, _ *tool.ProcessSupervisor, _ string, _ Manifest, _, _, outPath, _ string) error {
		return os.WriteFile(outPath, []byte("wav"), 0o644)
	}
	t.Cleanup(func() { AvailabilitySeam, SynthSeam = origAvail, origSynth })
	r1, _ := s.SpeakSample(EngineKokoro, "", "")
	_, _ = s.SpeakSample(EngineKokoro, "", "")
	p1, _ := s.SampleAudioPath(r1.AudioID)
	if _, err := os.Stat(p1); err == nil {
		t.Fatalf("previous sample file %s was not reaped", p1)
	}
}

// TestSpeakSampleBoundedConcurrency: a second concurrent call is rejected with
// "already being synthesised" instead of spawning a second process.
func TestSpeakSampleBoundedConcurrency(t *testing.T) {
	tmp := t.TempDir()
	s := NewSupervisor(DefaultConfig(), Options{Root: tmp, ProcSup: tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})})
	origAvail, origSynth := AvailabilitySeam, SynthSeam
	AvailabilitySeam = func(EngineID) (Availability, bool) { return AvailabilityReady, true }
	gate := make(chan struct{})
	SynthSeam = func(_ context.Context, _ *tool.ProcessSupervisor, _ string, _ Manifest, _, _, outPath, _ string) error {
		<-gate
		return os.WriteFile(outPath, []byte("wav"), 0o644)
	}
	t.Cleanup(func() { AvailabilitySeam, SynthSeam = origAvail, origSynth })
	done := make(chan error, 1)
	go func() {
		_, err := s.SpeakSample(EngineKokoro, "", "")
		done <- err
	}()
	// wait for first to acquire the lock and start blocking in synth
	time.Sleep(50 * time.Millisecond)
	if _, err := s.SpeakSample(EngineKokoro, "", ""); err == nil || !strings.Contains(err.Error(), "already being") {
		t.Fatalf("second concurrent SpeakSample was not rejected: %v", err)
	}
	close(gate)
	if err := <-done; err != nil {
		t.Fatalf("first SpeakSample failed: %v", err)
	}
}
