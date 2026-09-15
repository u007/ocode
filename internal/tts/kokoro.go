package tts

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/u007/ocode/internal/tool"
)

// kokoroSynth runs one Kokoro synthesis under the shared process supervisor.
// It returns after the WAV at outPath is complete.
func kokoroSynth(ctx context.Context, sup *tool.ProcessSupervisor, root string, m Manifest, id string, text, outPath string, voice string) error {
	if voice == "" {
		voice = m.Voice
	}
	dir, err := CacheDirChecked(root, m.Engine, m.Voice, m.Version)
	if err != nil {
		return err
	}
	model := filepath.Join(dir, "kokoro-v1.0.onnx")
	voices := filepath.Join(dir, "voices-v1.0.bin")
	venv := filepath.Join(dir, "venv")
	pyScript := `from kokoro_onnx import Kokoro
import soundfile as sf
import sys
model = sys.argv[1]
voices = sys.argv[2]
voice = sys.argv[3]
out = sys.argv[4]
text = sys.stdin.read().strip()
kokoro = Kokoro(model, voices)
samples, sr = kokoro.create(text, voice=voice, speed=1.0, lang="en-us")
sf.write(out, samples, sr)
`
	scriptPath := filepath.Join(dir, "synthesize.py")
	if err := os.WriteFile(scriptPath, []byte(pyScript), 0o644); err != nil {
		return fmt.Errorf("write synth script: %w", err)
	}
	cmd := exec.CommandContext(ctx, venvPython(venv), scriptPath, model, voices, voice, outPath)
	cmd.Stdin = strings.NewReader(text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	if _, err := tool.StartSupervised(sup, cmd, tool.ProcessRegistration{
		ID:      "tts-kokoro-" + id,
		Name:    "kokoro synth",
		Command: "python kokoro synth",
		Kind:    tool.ProcessKindTTS,
	}); err != nil {
		return fmt.Errorf("start kokoro: %w", err)
	}
	err = cmd.Wait()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		sup.MarkKilled("tts-kokoro-"+id, code)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("kokoro exited: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	sup.MarkExited("tts-kokoro-"+id, code)
	info, err := os.Stat(outPath)
	if err != nil || info.Size() == 0 {
		return fmt.Errorf("kokoro produced no audio: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}
