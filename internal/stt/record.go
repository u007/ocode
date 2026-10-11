package stt

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Recording is one microphone capture to a 16 kHz mono WAV file. It is used
// by the TUI, which has no browser to record for it. Capture runs in a child
// process whose output is captured, never inherited, so nothing paints over
// the alt-screen.
type Recording struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	path   string
	stderr *bytes.Buffer
	done   chan error
	// interrupt is true when the recorder stops on SIGINT (arecord) rather than
	// a "q" on stdin (ffmpeg).
	interrupt bool
}

// captureCommand picks a capture tool for goos. It returns the program, its
// arguments, and whether it stops on SIGINT rather than "q" on stdin.
func captureCommand(goos string, out string, hasArecord bool) (name string, args []string, interrupt bool, err error) {
	switch goos {
	case "linux":
		if hasArecord {
			return "arecord", []string{"-q", "-f", "S16_LE", "-r", "16000", "-c", "1", "-t", "wav", out}, true, nil
		}
		return "ffmpeg", ffmpegArgs("pulse", "default", out), false, nil
	case "darwin":
		return "ffmpeg", ffmpegArgs("avfoundation", ":default", out), false, nil
	case "windows":
		return "ffmpeg", ffmpegArgs("dshow", "audio=default", out), false, nil
	}
	return "", nil, false, fmt.Errorf("microphone capture is not supported on %s", goos)
}

func ffmpegArgs(format, device, out string) []string {
	return []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", format, "-i", device, "-ac", "1", "-ar", "16000", out}
}

// RecordingAvailable reports whether this host has a capture tool, so the UI
// can disable the mic instead of failing on press.
func RecordingAvailable() error {
	_, hasArecord := lookPath("arecord")
	name, _, _, err := captureCommand(runtime.GOOS, "", hasArecord)
	if err != nil {
		return err
	}
	if _, ok := lookPath(name); !ok {
		return fmt.Errorf("%s is not installed (needed for microphone capture)", name)
	}
	return nil
}

// StartRecording begins capturing into dir. The caller must call Stop or
// Cancel; an abandoned recording keeps the device open.
func StartRecording(dir string) (*Recording, error) {
	if err := RecordingAvailable(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	out := filepath.Join(dir, fmt.Sprintf("voice-%d.wav", time.Now().UnixNano()))
	_, hasArecord := lookPath("arecord")
	name, args, interrupt, err := captureCommand(runtime.GOOS, out, hasArecord)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(name, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", name, err)
	}
	r := &Recording{cmd: cmd, stdin: stdin, path: out, stderr: &stderr, done: make(chan error, 1), interrupt: interrupt}
	go func() { r.done <- cmd.Wait() }()
	return r, nil
}

// Stop ends the capture, waits for the file to be finalised, and returns its
// path. A recording with no audio is an error, not an empty success.
func (r *Recording) Stop(timeout time.Duration) (string, error) {
	if r.interrupt {
		_ = r.cmd.Process.Signal(os.Interrupt)
	} else {
		_, _ = io.WriteString(r.stdin, "q\n")
		_ = r.stdin.Close()
	}
	select {
	case <-r.done:
	case <-time.After(timeout):
		_ = r.cmd.Process.Kill()
		<-r.done
		return "", errors.New("microphone capture did not stop in time")
	}
	if err := checkSize(r.path); err != nil {
		return "", errors.New("no audio was captured — check the microphone")
	}
	return r.path, nil
}

// Cancel stops the capture and deletes the file.
func (r *Recording) Cancel() {
	_, _ = r.Stop(3 * time.Second)
	_ = os.Remove(r.path)
}

// Snapshot copies the audio captured so far into a standalone WAV in the
// recording's directory and returns its path; the caller removes it. The
// capture process is never signalled or reopened: the live file is only read.
//
// The live file cannot be decoded as-is. ffmpeg writes RIFF and data sizes of
// 0xFFFFFFFF until it closes the file (verified with ffmpeg 6.1.1: the header
// reads RIFF=FFFFFFFF, data=FFFFFFFF, and Python's wave module reports
// 2147483647 frames). The copy gets corrected sizes and a trailing partial
// sample is dropped.
func (r *Recording) Snapshot() (string, error) {
	raw, err := os.ReadFile(r.path)
	if err != nil {
		return "", err
	}
	fixed, err := repairWAVSizes(raw)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(filepath.Dir(r.path), "partial-*.wav")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(fixed); err != nil {
		f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// repairWAVSizes returns a copy of a (possibly still-growing) PCM WAV whose
// RIFF and data chunk sizes describe the bytes actually present. Chunks are
// walked by their own sizes, so a LIST chunk before data (which ffmpeg writes)
// is handled. A data chunk that is still open runs to the end of the input,
// truncated to whole sample frames. The input is not modified.
func repairWAVSizes(b []byte) ([]byte, error) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, errors.New("speech-to-text: not a WAV file")
	}
	blockAlign := 0
	off := 12
	for off+8 <= len(b) {
		id := string(b[off : off+4])
		body := off + 8
		switch id {
		case "fmt ":
			if body+16 > len(b) {
				return nil, errors.New("speech-to-text: recording has no audio yet")
			}
			blockAlign = int(binary.LittleEndian.Uint16(b[body+12 : body+14]))
		case "data":
			if blockAlign <= 0 {
				return nil, errors.New("speech-to-text: data chunk before fmt chunk")
			}
			n := len(b) - body
			n -= n % blockAlign
			if n == 0 {
				return nil, errors.New("speech-to-text: recording has no audio yet")
			}
			out := make([]byte, body+n)
			copy(out, b[:body+n])
			binary.LittleEndian.PutUint32(out[off+4:off+8], uint32(n))
			binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
			return out, nil
		}
		size := uint64(binary.LittleEndian.Uint32(b[off+4 : off+8]))
		next := uint64(body) + size + size&1
		if next > uint64(len(b)) {
			// The chunk being written has not reached its end yet; no data
			// chunk is readable.
			break
		}
		off = int(next)
	}
	return nil, errors.New("speech-to-text: recording has no audio yet")
}

// lookPath reports whether name is on PATH, returning the resolved path.
func lookPath(name string) (string, bool) {
	p, err := exec.LookPath(name)
	return p, err == nil
}
