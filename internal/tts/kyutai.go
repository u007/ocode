package tts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/u007/ocode/internal/tool"
)

const kyutaiScriptName = "kyutai_synth.py"

// kyutaiSynthScript reads text from stdin plus a voice id from ARGV and
// writes a WAV to the output path.
const kyutaiSynthScript = `import sys

import scipy.io.wavfile

from pocket_tts import TTSModel

voice = sys.argv[1]
out_path = sys.argv[2]
text = sys.stdin.read()

model = TTSModel.load_model()
voice_state = model.get_state_for_audio_prompt(voice)
audio = model.generate_audio(voice_state, text).detach().cpu().numpy()
scipy.io.wavfile.write(out_path, model.sample_rate, audio)
`

// kyutaiSynth runs Kyutai Pocket TTS in the per-engine venv.
func kyutaiSynth(ctx context.Context, sup *tool.ProcessSupervisor, root string, m Manifest, id, text, outPath, voice string) error {
	dir, err := CacheDirChecked(root, m.Engine, m.Voice, m.Version)
	if err != nil {
		return err
	}
	venv := filepath.Join(dir, "venv")
	scriptPath := filepath.Join(dir, kyutaiScriptName)
	// Write via a unique temp file + rename so concurrent syntheses never
	// read a half-written script.
	tmp, err := os.CreateTemp(dir, kyutaiScriptName+".*")
	if err != nil {
		return fmt.Errorf("write kyutai synth script: %w", err)
	}
	_, werr := tmp.WriteString(kyutaiSynthScript)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write kyutai synth script: %w", errors.Join(werr, cerr))
	}
	if err := os.Rename(tmp.Name(), scriptPath); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write kyutai synth script: %w", err)
	}
	if voice == "" {
		voice = "alba"
	}
	cmd := exec.CommandContext(ctx, venvPython(venv), scriptPath, voice, outPath)
	applySynthProcessEnv(cmd, dir)
	cmd.Stdin = strings.NewReader(text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	procID := "tts-kyutai-" + id
	if _, err := tool.StartSupervised(sup, cmd, tool.ProcessRegistration{
		ID:      procID,
		Name:    "Kyutai synth",
		Command: "python kyutai synth",
		Kind:    tool.ProcessKindTTS,
	}); err != nil {
		return fmt.Errorf("start kyutai: %w", err)
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
		return fmt.Errorf("kyutai exited: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	sup.MarkExited(procID, code)
	info, err := os.Stat(outPath)
	if err != nil || info.Size() == 0 {
		return fmt.Errorf("kyutai produced no audio: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

func kyutaiVerifyRuntime(ctx context.Context, venv, dir string) (string, error) {
	return runCmd(ctx, []string{venvPython(venv), "-c", "import pocket_tts, scipy"})
}
