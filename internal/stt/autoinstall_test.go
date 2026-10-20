package stt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/pyinterp"
)

// writeInstallablePython writes a POSIX stand-in for a base Python whose
// `-m venv <dir>` creates a venv whose own python imports fine, and whose
// `-m pip install` succeeds. Every invocation is appended to log.
func writeInstallablePython(t *testing.T, log string) string {
	t.Helper()
	return writeFakePython(t, log, false)
}

// writeFakePython is writeInstallablePython with an optional failing pip, so a
// test can check that pip's own output reaches the error.
func writeFakePython(t *testing.T, log string, pipFails bool) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake interpreter scripts need a POSIX shell")
	}
	path := filepath.Join(t.TempDir(), "python3.18")
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + log + "\n" +
		"if [ \"$1\" = --version ]; then echo 'Python 3.18.0'; exit 0; fi\n" +
		"if [ \"$1\" = -m ] && [ \"$2\" = venv ]; then\n" +
		"  /bin/mkdir -p \"$3/bin\" && /bin/cp \"$0\" \"$3/bin/python\" && exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = -m ] && [ \"$2\" = pip ] && [ -n \"" + pipFailFlag(pipFails) + "\" ]; then\n" +
		"  echo 'ERROR: Could not find a version that satisfies onnxruntime' >&2; exit 1\n" +
		"fi\n" +
		"case \"$0\" in\n" +
		"  */bin/python) exit 0 ;;\n" +
		"esac\n" +
		"echo \"ModuleNotFoundError: $3\" >&2; exit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// isolatePath hides every interpreter except the test's fake from the
// interpreter search, so a test that expects a managed venv to be built cannot
// be short-circuited by the HOST's real Python. On a developer machine that
// Python usually has onnx-asr installed, so installLocal's "another caller
// already installed it" scan would return it and the venv would never be
// built; the hermetic test would then assert against the wrong interpreter.
//
// PATH holds ONLY the fake's directory: the scan consults every PATH entry for
// the unversioned "python3" as well as the versioned names, so keeping the
// host's PATH would reintroduce the real Python. The fake therefore calls
// /bin/mkdir and /bin/cp by absolute path — it cannot rely on PATH itself.
func isolatePath(t *testing.T, fake string) {
	t.Helper()
	isolateInterpreterSearch(t)
	t.Setenv("PATH", filepath.Dir(fake))
}

// An unpinned host with a base Python but no onnx_asr builds the managed venv,
// pip-installs the packages into it and returns the venv's interpreter.
func TestInstallLocalBuildsManagedVenv(t *testing.T) {
	resetProbeCache(t)
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	log := filepath.Join(t.TempDir(), "calls.log")
	base := writeInstallablePython(t, log)
	isolatePath(t, base)

	py, err := installLocal(context.Background(), []string{"sentencepiece"})
	if err != nil {
		t.Fatalf("installLocal: %v", err)
	}
	want := filepath.Join(managedVenvDir(), "bin", "python")
	if len(py.Command) != 1 || py.Command[0] != want {
		t.Fatalf("interpreter = %v, want [%s]", py.Command, want)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "-m venv "+managedVenvDir()) {
		t.Errorf("venv was not created; calls:\n%s", calls)
	}
	// The pip call goes to the venv's python (the log is shared by the copy).
	if !strings.Contains(string(calls), "-m pip install --disable-pip-version-check onnx-asr[cpu,hub] sentencepiece") {
		t.Errorf("pip install missing or wrong; calls:\n%s", calls)
	}
}

// A pinned interpreter is the user's own environment: never installed into.
func TestMayInstallSkipsPinnedPython(t *testing.T) {
	if (Options{Python: "/x/python3"}).mayInstall() {
		t.Error("install allowed for a pinned python")
	}
	if !(Options{}).mayInstall() {
		t.Error("install should be possible for an unpinned host")
	}
	if err := Install(context.Background(), Options{Python: "/x/python3"}); err == nil {
		t.Error("Install modified a pinned python")
	}
}

// Without approval Transcribe asks instead of installing: it returns the typed
// error and leaves no venv behind. Approval then performs the install.
func TestTranscribeAsksBeforeInstalling(t *testing.T) {
	resetProbeCache(t)
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	log := filepath.Join(t.TempDir(), "calls.log")
	base := writeInstallablePython(t, log)
	isolatePath(t, base)
	wav := filepath.Join(t.TempDir(), "a.wav")
	if err := os.WriteFile(wav, []byte("RIFFxxxxWAVE"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Transcribe(context.Background(), Options{ModelDir: t.TempDir()}, wav)
	var req *InstallRequiredError
	if !errors.As(err, &req) {
		t.Fatalf("err = %v, want *InstallRequiredError", err)
	}
	if req.Venv != managedVenvDir() || len(req.Packages) == 0 {
		t.Errorf("requirement = %+v", req)
	}
	if _, statErr := os.Stat(managedVenvDir()); statErr == nil {
		t.Error("venv created without approval")
	}
	if calls, _ := os.ReadFile(log); strings.Contains(string(calls), "pip") || strings.Contains(string(calls), "venv") {
		t.Errorf("install ran without approval:\n%s", calls)
	}

	// The same options plus approval install, then fail only at the (fake)
	// transcription, never with InstallRequiredError.
	_, err = Transcribe(context.Background(), Options{ModelDir: t.TempDir(), InstallApproved: true}, wav)
	if errors.As(err, &req) {
		t.Fatalf("approved transcribe still asked: %v", err)
	}
	if _, statErr := os.Stat(managedVenvDir()); statErr != nil {
		t.Errorf("approved transcribe did not build the venv: %v", statErr)
	}
}

// A model whose deps are missing is selectable when a base Python exists,
// with the reason saying it installs on first use.
func TestStatusForReportsInstallableModelAvailable(t *testing.T) {
	resetProbeCache(t)
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	base := writeInstallablePython(t, filepath.Join(t.TempDir(), "calls.log"))
	isolatePath(t, base)

	st := StatusFor(Options{})
	for _, m := range st.Models {
		if m.Engine != EngineLocal {
			continue
		}
		if !m.Available || m.Reason != autoInstallNote {
			t.Errorf("%s available=%v reason=%q, want installable", m.ID, m.Available, m.Reason)
		}
	}
	st = StatusFor(Options{Python: filepath.Join(t.TempDir(), "no-such-python")})
	for _, m := range st.Models {
		if m.Engine == EngineLocal && m.Available {
			t.Errorf("%s available with a broken pinned python", m.ID)
		}
	}
}

func pipFailFlag(fail bool) string {
	if fail {
		return "1"
	}
	return ""
}

// A failed pip install must reach the caller with pip's own message, so the web
// banner and the TUI chat show why, not just "install failed".
func TestInstallLocalSurfacesPipOutput(t *testing.T) {
	resetProbeCache(t)
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	base := writeFakePython(t, filepath.Join(t.TempDir(), "calls.log"), true)
	isolatePath(t, base)

	err := Install(context.Background(), Options{})
	if err == nil {
		t.Fatal("Install succeeded with a failing pip")
	}
	for _, want := range []string{"pip install", "Could not find a version that satisfies onnxruntime"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

// A venv whose creation was interrupted has bin/python but no working pip. The
// next install must rebuild it instead of failing on "No module named pip".
func TestInstallLocalRepairsBrokenVenv(t *testing.T) {
	resetProbeCache(t)
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	log := filepath.Join(t.TempDir(), "calls.log")
	base := writeInstallablePython(t, log)
	isolatePath(t, base)

	// A leftover venv whose python cannot run pip.
	broken := pyinterpVenvPython(managedVenvDir())
	if err := os.MkdirAll(filepath.Dir(broken), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Install(context.Background(), Options{}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "-m venv "+managedVenvDir()) {
		t.Errorf("broken venv was not rebuilt; calls:\n%s", calls)
	}
}

// Live previews never install and never ask, even if the caller approved.
func TestPartialTranscribeNeverInstalls(t *testing.T) {
	resetProbeCache(t)
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	log := filepath.Join(t.TempDir(), "calls.log")
	base := writeInstallablePython(t, log)
	isolatePath(t, base)
	raw, err := os.ReadFile(filepath.Join("testdata", "speech-16k.wav"))
	if err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(t.TempDir(), "live.wav")
	if err := os.WriteFile(live, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = PartialTranscribe(context.Background(),
		Options{ModelDir: t.TempDir(), InstallApproved: true}, &Recording{path: live})
	var req *InstallRequiredError
	if !errors.As(err, &req) {
		t.Fatalf("err = %v, want the preview to be refused with *InstallRequiredError", err)
	}
	if calls, _ := os.ReadFile(log); strings.Contains(string(calls), "pip") || strings.Contains(string(calls), "-m venv") {
		t.Errorf("a preview installed:\n%s", calls)
	}
}

// A second install waiting behind a running one gives up when its context ends.
func TestInstallLocalWaiterHonoursContext(t *testing.T) {
	installSem <- struct{}{}
	defer func() { <-installSem }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := installLocal(ctx, nil); err == nil || !strings.Contains(err.Error(), "waiting") {
		t.Fatalf("err = %v, want a waiting/cancelled error", err)
	}
}

func pyinterpVenvPython(venv string) string { return pyinterp.VenvPython(venv) }
