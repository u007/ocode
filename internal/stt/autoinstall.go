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
const autoInstallNote = "needs a one-time Python dependency install (you are asked first)"

// installMu serialises dependency installs in this process, so two voice
// requests racing on a fresh host build one venv, not two.
var installMu sync.Mutex

// mayInstall reports whether a missing dependency can be installed for these
// options at all. A pinned interpreter is the user's own environment and is
// never modified. Whether the install actually runs is InstallApproved.
func (o Options) mayInstall() bool {
	return o.Python == ""
}

// InstallRequiredError says the local engine's Python dependencies are missing
// and can be installed, but the caller has not approved it. Hosts catch it with
// errors.As, ask the user, and retry with Options.InstallApproved (or call
// Install). Nothing has been downloaded or written when it is returned.
type InstallRequiredError struct {
	// Packages are the pip requirements that would be installed.
	Packages []string
	// Venv is the directory the managed environment would be created in.
	Venv string
}

func (e *InstallRequiredError) Error() string {
	return fmt.Sprintf("speech-to-text: the local model needs a one-time install of %s into %s",
		strings.Join(e.Packages, " "), e.Venv)
}

// installRequirement returns the pending install when a base Python 3.10+
// exists to build the venv from, or nil when none does (the original scan error
// is then the useful one).
func installRequirement(ctx context.Context, extra []string) *InstallRequiredError {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if _, err := baseInterpreter(ctx); err != nil {
		return nil
	}
	return &InstallRequiredError{Packages: installPackages(extra), Venv: managedVenvDir()}
}

func installPackages(extra []string) []string {
	return append([]string{"onnx-asr[cpu,hub]"}, extra...)
}

// Install performs the dependency install for the selected model, for hosts
// that confirm with the user before calling Transcribe. It refuses a pinned
// Python, which is never modified.
func Install(ctx context.Context, opts Options) error {
	s, _ := lookup(ResolveModel(opts.Model))
	if s.Engine != EngineLocal {
		return fmt.Errorf("speech-to-text: %s is hosted and needs no install", s.ID)
	}
	if !opts.mayInstall() {
		return errors.New("speech-to-text: a pinned Python is never modified; install onnx-asr into it yourself")
	}
	_, err := installLocal(ctx, s.Extra)
	return err
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
	args := append([]string{py, "-m", "pip", "install", "--disable-pip-version-check"}, installPackages(extra)...)
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
