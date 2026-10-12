package stt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/pyinterp"
)

// installTimeout bounds one dependency install (venv build plus pip). The
// onnxruntime wheel is the bulk of it and can take minutes on a slow link.
const installTimeout = localTimeout

// autoInstallNote is the status reason shown for a model whose Python
// dependencies are missing but can be installed on first use.
const autoInstallNote = "Python dependencies install automatically on first use"

// installMu serialises dependency installs in this process, so two voice
// requests racing on a fresh host build one venv, not two.
var installMu sync.Mutex

// autoInstall reports whether a missing dependency may be installed for these
// options. A pinned interpreter is the user's own environment and is never
// modified; live partials must not start a multi-minute install on every poll.
func (o Options) autoInstall() bool {
	return o.Python == "" && !o.NoAutoInstall
}

// managedVenvDir is the venv ocode builds for the local engine, next to the
// downloaded models. Returns "" when no cache dir exists.
func managedVenvDir() string {
	cache, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(cache, "ocode", "stt-venv")
}

// managedPython is the managed venv's interpreter when it exists.
func managedPython() string {
	dir := managedVenvDir()
	if dir == "" {
		return ""
	}
	py := pyinterp.VenvPython(dir)
	if _, err := os.Stat(py); err != nil {
		return ""
	}
	return py
}

// baseInterpreter finds a Python 3.10+ to build the venv from, with no module
// requirement. It is the precondition for an auto-install to be possible.
func baseInterpreter(ctx context.Context) (pyinterp.Spec, error) {
	return pyinterp.Select(ctx, localRange, pyinterp.Candidates(localRange), nil)
}

// installLocal builds (or repairs) the managed venv and pip-installs onnx-asr
// plus the model's extra modules into it, then returns the interpreter. It is
// additive: a later model with more extras installs only those into the same
// venv. Output is captured and surfaced in the error, never inherited, so
// nothing paints over the TUI's alt-screen.
func installLocal(ctx context.Context, extra []string) (pyinterp.Spec, error) {
	installMu.Lock()
	defer installMu.Unlock()

	// Another caller may have finished the install while this one waited.
	if py, err := scanLocal("", extra); err == nil {
		return py, nil
	}

	venv := managedVenvDir()
	if venv == "" {
		return pyinterp.Spec{}, errors.New("no user cache dir to hold the Python environment")
	}
	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()

	if managedPython() == "" {
		base, err := baseInterpreter(ctx)
		if err != nil {
			return pyinterp.Spec{}, err
		}
		if err := os.RemoveAll(venv); err != nil {
			return pyinterp.Spec{}, fmt.Errorf("reset %s: %w", venv, err)
		}
		if err := os.MkdirAll(filepath.Dir(venv), 0o755); err != nil {
			return pyinterp.Spec{}, err
		}
		if out, err := runInstallCmd(ctx, append(append([]string{}, base.Command...), "-m", "venv", venv)); err != nil {
			return pyinterp.Spec{}, fmt.Errorf("create venv with %s: %w: %s",
				strings.Join(base.Command, " "), err, out)
		}
	}

	py := pyinterp.VenvPython(venv)
	args := []string{py, "-m", "pip", "install", "--disable-pip-version-check", "onnx-asr[cpu,hub]"}
	args = append(args, extra...)
	if out, err := runInstallCmd(ctx, args); err != nil {
		return pyinterp.Spec{}, fmt.Errorf("pip install: %w: %s", err, out)
	}
	if out, err := runInstallCmd(ctx, []string{py, "-I", "-c", "import " + strings.Join(localModules(extra), ", ")}); err != nil {
		return pyinterp.Spec{}, fmt.Errorf("installed packages do not import: %w: %s", err, out)
	}
	forgetProbes()
	return pyinterp.Spec{Command: []string{py}}, nil
}

// forgetProbes drops every cached interpreter scan so the next status poll and
// transcription see the freshly installed venv.
func forgetProbes() {
	probeCache.mu.Lock()
	probeCache.entries = map[string]*probeEntry{}
	probeCache.mu.Unlock()
}

// runInstallCmd runs argv with combined output captured and returns its tail.
func runInstallCmd(ctx context.Context, argv []string) (string, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "PIP_NO_INPUT=1", "PIP_DISABLE_PIP_VERSION_CHECK=1")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if len(out) > 800 {
		out = out[len(out)-800:]
	}
	if err != nil && ctx.Err() != nil {
		err = fmt.Errorf("%w (%v)", ctx.Err(), err)
	}
	return out, err
}
