package tts

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/u007/ocode/internal/tool"
)

// espeak-ng stores its data directory in a fixed-size buffer: N_PATH_HOME is
// 160 bytes on POSIX and 230 on Windows (espeak-ng 1.52.0 speech.h).
// espeak_Initialize silently truncates a longer path, fails to read phontab,
// and then falls back to the data path compiled into the espeakng-loader wheel
// on its CI builder (an absolute /Users/runner/work/... path that does not
// exist on the user's machine), after which it calls exit(1). ocode's bundled
// data directory nests under venv/lib/pythonX.Y/site-packages/espeakng_loader/
// espeak-ng-data, which can exceed the limit for a user-configured data root,
// so a short copy of the data directory is handed to espeak when needed.
func espeakDataPathLimit() int {
	if runtime.GOOS == "windows" {
		return 230
	}
	return 160
}

// espeakDataDirName is the short copy of the espeak-ng data directory that
// espeakDataPath creates when the bundled path does not fit the limit.
const espeakDataDirName = ".espeak-data"

// bundledEspeakDataPath asks the venv's espeakng-loader where its bundled
// espeak-ng data directory lives.
func bundledEspeakDataPath(ctx context.Context, venv string) (string, error) {
	out, err := runCmd(ctx, []string{venvPython(venv)}, "-c",
		"import espeakng_loader; print(espeakng_loader.get_data_path())")
	if err != nil {
		return "", fmt.Errorf("locate espeak data: %w: %s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1]), nil
}

// espeakDataPath returns a path to the espeak-ng data directory that fits
// espeak-ng's fixed N_PATH_HOME buffer. A symlink cannot be used here: espeak's
// own path check, and phonemizer's Path.resolve() before it, both follow
// symlinks and would expand the link back past the buffer. When the bundled
// path is too long, the data directory is copied to a real short directory
// beside the synth script instead.
func espeakDataPath(dir, bundled string) (string, error) {
	if len(bundled) < espeakDataPathLimit() {
		return bundled, nil
	}
	dest := filepath.Join(dir, espeakDataDirName)
	if len(dest) >= espeakDataPathLimit() {
		return "", fmt.Errorf("espeak data path %q is %d bytes and no short data directory fits the %d-byte limit", bundled, len(bundled), espeakDataPathLimit())
	}
	if info, err := os.Stat(filepath.Join(dest, "phontab")); err == nil && !info.IsDir() {
		return dest, nil
	}
	tmp, err := os.MkdirTemp(dir, espeakDataDirName+"-")
	if err != nil {
		return "", fmt.Errorf("create espeak data dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := os.CopyFS(tmp, os.DirFS(bundled)); err != nil {
		return "", fmt.Errorf("copy espeak data: %w", err)
	}
	if err := os.RemoveAll(dest); err != nil {
		return "", fmt.Errorf("replace espeak data dir: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", fmt.Errorf("install espeak data dir: %w", err)
	}
	return dest, nil
}

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
	bundled, err := bundledEspeakDataPath(ctx, venv)
	if err != nil {
		return err
	}
	dataPath, err := espeakDataPath(dir, bundled)
	if err != nil {
		return err
	}
	pyScript := `from kokoro_onnx import Kokoro
from kokoro_onnx.config import EspeakConfig
import soundfile as sf
import sys
model = sys.argv[1]
voices = sys.argv[2]
voice = sys.argv[3]
out = sys.argv[4]
data_path = sys.argv[5] if len(sys.argv) > 5 else None
text = sys.stdin.read().strip()
espeak_config = EspeakConfig(data_path=data_path) if data_path else None
kokoro = Kokoro(model, voices, espeak_config=espeak_config)
samples, sr = kokoro.create(text, voice=voice, speed=1.0, lang="en-us")
sf.write(out, samples, sr)
`
	scriptPath := filepath.Join(dir, "synthesize.py")
	if err := os.WriteFile(scriptPath, []byte(pyScript), 0o644); err != nil {
		return fmt.Errorf("write synth script: %w", err)
	}
	cmd := exec.CommandContext(ctx, venvPython(venv), scriptPath, model, voices, voice, outPath, dataPath)
	applySynthProcessEnv(cmd, dir)
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
