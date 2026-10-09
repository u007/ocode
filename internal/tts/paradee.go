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

// Artifact names inside the Paradee cache dir. The driver scripts are written
// there at install and synthesis time.
const (
	paradeeModelName  = "paradee_int8.onnx"
	paradeeConfigName = "config.json"
	paradeeScriptName = "paradee_synthesize.py"
	paradeeProbeName  = "paradee_probe.py"
)

// paradeeRuntimePreamble is shared by the synth driver and the install probe,
// so the check exercises exactly the code path synthesis will run. It follows
// the inference core of the Paradee Space (paradee/tts.py, Apache-2.0, Sahil
// Mahendrakar): misaki phonemes -> token ids -> one ONNX graph. argv is model,
// config, espeak data directory.
const paradeeRuntimePreamble = `import json
import re
import sys

import numpy as np
import onnxruntime as ort
from misaki import en, espeak
from phonemizer.backend.espeak.wrapper import EspeakWrapper

MAX_PHONEMES = 510

model, config, data_path = sys.argv[1:4]

# Importing misaki.espeak points espeak-ng at the wheel's bundled data, which
# can exceed espeak's fixed path buffer. Re-point it at the short copy before
# the G2P backend is constructed.
EspeakWrapper.set_data_path(data_path)

with open(config, encoding="utf-8") as f:
    vocab = json.load(f)["vocab"]
so = ort.SessionOptions()
so.intra_op_num_threads = 1
so.inter_op_num_threads = 1
session = ort.InferenceSession(model, so, providers=["CPUExecutionProvider"])
g2p = en.G2P(trf=False, british=False, fallback=espeak.EspeakFallback(british=False), unk="")


def speak_phonemes(ps):
    ids = [0] + [vocab[c] for c in ps if c in vocab][:MAX_PHONEMES] + [0]
    feed = {
        "input_ids": np.array([ids], dtype=np.int64),
        "speed": np.array([1.0], dtype=np.float32),
    }
    return session.run(None, feed)[0][0]
`

// paradeeSynthScript reads the text on stdin and writes a 24 kHz float32 WAV to
// argv[4]. Sentences are phonemized one at a time, as the Space does.
const paradeeSynthScript = paradeeRuntimePreamble + `import soundfile as sf

SAMPLE_RATE = 24000
out = sys.argv[4]
text = sys.stdin.read().strip()


def chunks(body):
    for sentence in re.split(r"(?<=[.!?…])\s+|\n+", body.strip()):
        if not sentence.strip():
            continue
        ps, _ = g2p(sentence)
        while len(ps) > MAX_PHONEMES:
            cut = ps.rfind(" ", 0, MAX_PHONEMES)
            cut = cut if cut > 0 else MAX_PHONEMES
            yield ps[:cut]
            ps = ps[cut:].lstrip()
        if ps:
            yield ps


parts = [speak_phonemes(ps) for ps in chunks(text)]
audio = np.concatenate(parts) if parts else np.zeros(0, dtype=np.float32)
if audio.size == 0:
    sys.exit("no speech produced")
sf.write(out, audio, SAMPLE_RATE)
`

// paradeeProbeScript runs one phrase through the same G2P and graph that
// synthesis uses, and fails if no audio comes back. It writes nothing.
const paradeeProbeScript = paradeeRuntimePreamble + `
ps, _ = g2p("Paradee is ready.")
if speak_phonemes(ps).size == 0:
    sys.exit("probe produced no speech")
`

// paradeeSynth runs one Paradee synthesis under the shared process supervisor.
// The model has a single built-in voice, so no voice argument is taken.
func paradeeSynth(ctx context.Context, sup *tool.ProcessSupervisor, root string, m Manifest, id string, text, outPath string) error {
	dir, err := CacheDirChecked(root, m.Engine, m.Voice, m.Version)
	if err != nil {
		return err
	}
	venv := filepath.Join(dir, "venv")
	bundled, err := bundledEspeakDataPath(ctx, venv)
	if err != nil {
		return err
	}
	dataPath, err := espeakDataPath(dir, bundled)
	if err != nil {
		return err
	}
	scriptPath := filepath.Join(dir, paradeeScriptName)
	if err := os.WriteFile(scriptPath, []byte(paradeeSynthScript), 0o644); err != nil {
		return fmt.Errorf("write paradee synth script: %w", err)
	}
	cmd := exec.CommandContext(ctx, venvPython(venv), scriptPath,
		filepath.Join(dir, paradeeModelName), filepath.Join(dir, paradeeConfigName), dataPath, outPath)
	applySynthProcessEnv(cmd, dir)
	cmd.Stdin = strings.NewReader(text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	procID := "tts-paradee-" + id
	if _, err := tool.StartSupervised(sup, cmd, tool.ProcessRegistration{
		ID:      procID,
		Name:    "Paradee synth",
		Command: "python paradee synth",
		Kind:    tool.ProcessKindTTS,
	}); err != nil {
		return fmt.Errorf("start paradee: %w", err)
	}
	err = cmd.Wait()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		sup.MarkKilled(procID, code)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("paradee exited: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	sup.MarkExited(procID, code)
	info, err := os.Stat(outPath)
	if err != nil || info.Size() == 0 {
		return fmt.Errorf("paradee produced no audio: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

// paradeeVerifyRuntime runs the probe in the freshly built venv. An import
// alone is not enough: misaki loads its spaCy pipeline and espeak backend on
// first use, so a missing model or an unusable espeak data path would
// otherwise first surface on the user's first playback.
func paradeeVerifyRuntime(ctx context.Context, venv, dir string) (string, error) {
	scriptPath := filepath.Join(dir, paradeeProbeName)
	if err := os.WriteFile(scriptPath, []byte(paradeeProbeScript), 0o644); err != nil {
		return "", fmt.Errorf("write paradee probe script: %w", err)
	}
	bundled, err := bundledEspeakDataPath(ctx, venv)
	if err != nil {
		return "", err
	}
	dataPath, err := espeakDataPath(dir, bundled)
	if err != nil {
		return "", err
	}
	return runCmdEnv(ctx, ortTelemetryDisabledEnv(os.Environ()), dir,
		[]string{venvPython(venv), scriptPath},
		filepath.Join(dir, paradeeModelName), filepath.Join(dir, paradeeConfigName), dataPath)
}
