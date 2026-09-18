package tts

import (
	"os"
	"os/exec"
	"strings"
)

// ortDisableTelemetryKey is the environment variable ONNX Runtime documents as
// the opt-out for its platform telemetry (ORT >= 1.29 on POSIX). Left unset,
// OrtEnv initialization writes a telemetry device-id/session sidecar named
// ":memory:.ses" into the process working directory — so a synthesis run from a
// project root litters the user's git working tree with an untracked file.
const ortDisableTelemetryKey = "ORT_DISABLE_TELEMETRY"

// applySynthProcessEnv hardens a TTS child process against cwd side effects:
// ONNX Runtime telemetry is disabled and the working directory is pinned to the
// engine's cache dir, so any remaining cwd-relative artifact stays inside
// ocode's own data tree instead of the user's project. Every synthesis path
// (script, model, voices, output, espeak data) is passed as an absolute
// argument, so pinning the cwd is safe.
func applySynthProcessEnv(cmd *exec.Cmd, dir string) {
	cmd.Dir = dir
	cmd.Env = ortTelemetryDisabledEnv(os.Environ())
}

// ortTelemetryDisabledEnv returns env with ORT_DISABLE_TELEMETRY=1, dropping any
// inherited value so a stale export cannot re-enable telemetry.
func ortTelemetryDisabledEnv(env []string) []string {
	prefix := ortDisableTelemetryKey + "="
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, prefix+"1")
}
