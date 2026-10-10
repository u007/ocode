package stt

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// liveWAV builds a WAV shaped like ffmpeg's output while it is still open:
// RIFF and data sizes are 0xFFFFFFFF, a LIST chunk sits between fmt and data,
// and the payload holds samples 16-bit mono frames plus extra stray bytes (a
// sample that was only half flushed).
func liveWAV(samples, extra int) []byte {
	var b []byte
	b = append(b, "RIFF"...)
	b = binary.LittleEndian.AppendUint32(b, 0xFFFFFFFF)
	b = append(b, "WAVE"...)
	b = append(b, "fmt "...)
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1)     // PCM
	b = binary.LittleEndian.AppendUint16(b, 1)     // mono
	b = binary.LittleEndian.AppendUint32(b, 16000) // rate
	b = binary.LittleEndian.AppendUint32(b, 32000) // byte rate
	b = binary.LittleEndian.AppendUint16(b, 2)     // block align
	b = binary.LittleEndian.AppendUint16(b, 16)    // bits
	info := []byte("INFOISFTxLavf60.16.100\x00")
	b = append(b, "LIST"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(info)))
	b = append(b, info...)
	if len(info)%2 == 1 {
		b = append(b, 0) // RIFF pad byte for an odd-sized chunk
	}
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, 0xFFFFFFFF)
	for i := 0; i < samples; i++ {
		b = binary.LittleEndian.AppendUint16(b, uint16(i))
	}
	for i := 0; i < extra; i++ {
		b = append(b, 0x7f)
	}
	return b
}

// readDataChunk walks the RIFF chunks like a strict reader and returns the
// declared RIFF size, the declared data size and the data payload length.
func readDataChunk(t *testing.T, b []byte) (riff, data uint32, payload int) {
	t.Helper()
	if string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		t.Fatal("not a RIFF/WAVE file")
	}
	riff = binary.LittleEndian.Uint32(b[4:8])
	off := 12
	for off+8 <= len(b) {
		id := string(b[off : off+4])
		size := int(binary.LittleEndian.Uint32(b[off+4 : off+8]))
		if id == "data" {
			return riff, uint32(size), len(b) - (off + 8)
		}
		off += 8 + size + size&1
	}
	t.Fatal("no data chunk")
	return
}

func TestRepairWAVSizesYieldsDecodableFileOfRightLength(t *testing.T) {
	const samples = 16000 // one second at 16 kHz
	raw := liveWAV(samples, 1)

	fixed, err := repairWAVSizes(raw)
	if err != nil {
		t.Fatal(err)
	}
	riff, data, payload := readDataChunk(t, fixed)
	if want := uint32(samples * 2); data != want {
		t.Fatalf("data size = %d, want %d", data, want)
	}
	if payload != samples*2 {
		t.Fatalf("payload = %d bytes, want %d (stray byte must be dropped)", payload, samples*2)
	}
	if riff != uint32(len(fixed)-8) {
		t.Fatalf("RIFF size = %d, want %d", riff, len(fixed)-8)
	}
	// The input must not be modified.
	if binary.LittleEndian.Uint32(raw[4:8]) != 0xFFFFFFFF {
		t.Fatal("repair modified its input")
	}
}

func TestRepairWAVSizesRejectsNoAudioYet(t *testing.T) {
	if _, err := repairWAVSizes(liveWAV(0, 0)); err == nil {
		t.Fatal("header with an empty data chunk must not produce a snapshot")
	}
	if _, err := repairWAVSizes([]byte("RIFF")); err == nil {
		t.Fatal("truncated header must be rejected")
	}
	if _, err := repairWAVSizes([]byte("not a wav file at all")); err == nil {
		t.Fatal("non-WAV input must be rejected")
	}
}

func TestRecordingSnapshotIsDecodableCopyInRecordingDir(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "voice-1.wav")
	if err := os.WriteFile(live, liveWAV(800, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Recording{path: live}
	snap, err := r.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(snap) != dir {
		t.Fatalf("snapshot dir = %s, want %s", filepath.Dir(snap), dir)
	}
	b, err := os.ReadFile(snap)
	if err != nil {
		t.Fatal(err)
	}
	if _, data, payload := readDataChunk(t, b); data != 1600 || payload != 1600 {
		t.Fatalf("snapshot data=%d payload=%d, want 1600", data, payload)
	}
	// The live file is untouched.
	if raw, _ := os.ReadFile(live); binary.LittleEndian.Uint32(raw[4:8]) != 0xFFFFFFFF {
		t.Fatal("snapshot modified the live file")
	}
	if ffmpeg, err := exec.LookPath("ffmpeg"); err == nil {
		out, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-i", snap, "-f", "null", "-").CombinedOutput()
		if err != nil {
			t.Fatalf("ffmpeg could not decode snapshot: %v: %s", err, out)
		}
	}
	_ = os.Remove(snap)
}

func TestPartialTranscribeUsesSnapshotAndCleansUp(t *testing.T) {
	var gotBytes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			t.Errorf("form file: %v", err)
			return
		}
		defer f.Close()
		buf := make([]byte, 1<<20)
		n, _ := f.Read(buf)
		gotBytes = n
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "partial words"})
	}))
	defer srv.Close()

	dir := t.TempDir()
	live := filepath.Join(dir, "voice-2.wav")
	if err := os.WriteFile(live, liveWAV(1600, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Model:         "whisper-1",
		OpenAIKey:     func() string { return "test-key" },
		OpenAIBaseURL: srv.URL,
	}
	res, err := PartialTranscribe(context.Background(), opts, &Recording{path: live})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "partial words" || res.Model != "whisper-1" {
		t.Fatalf("result = %+v", res)
	}
	if gotBytes < 3200 {
		t.Fatalf("uploaded %d bytes, want the snapshot payload", gotBytes)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "partial-*.wav"))
	if len(left) != 0 {
		t.Fatalf("snapshot not removed: %v", left)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatal("live recording must survive a partial decode")
	}
}

func TestPartialTranscribeFailureIsReturnedNotPrinted(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "voice-3.wav")
	if err := os.WriteFile(live, liveWAV(0, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := PartialTranscribe(context.Background(), Options{Model: "whisper-1", OpenAIKey: func() string { return "k" }}, &Recording{path: live})
	if err == nil {
		t.Fatal("expected an error for a recording with no audio yet")
	}
}
