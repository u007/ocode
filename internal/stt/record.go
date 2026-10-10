package stt

import (
	"bytes"
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

// lookPath reports whether name is on PATH, returning the resolved path.
func lookPath(name string) (string, bool) {
	p, err := exec.LookPath(name)
	return p, err == nil
}
