package stt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/pyinterp"
)

// writeVersionOnlyPython writes a fake interpreter that reports a Python
// version but cannot import onnx_asr.
//
// The two interpreter probes ask different questions, and the difference is
// what makes the install gate reachable in a hermetic test:
//
//   - baseInterpreter (installRequirement) asks for NO module: the first
//     interpreter that reports a version wins. It decides whether an install
//     is even POSSIBLE.
//   - localInterpreter (Transcribe) asks for onnx_asr: the first interpreter
//     that can import it wins.
//
// So a fake that answers the version probe but fails the import probe is
// exactly "a Python 3.10+ exists, but onnx-asr is not installed in it" — the
// state the install gate exists to report.
func writeVersionOnlyPython(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "python3.13")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = --version ]; then echo 'Python 3.13.0'; exit 0; fi\n" +
		// The module probe runs as "<python> -I -c \"import onnx_asr\"".
		// The -c flag is not the first argument, so scan for it.
		"for a in \"$@\"; do\n" +
		"  if [ \"$a\" = -c ]; then echo 'ModuleNotFoundError: onnx_asr' >&2; exit 1; fi\n" +
		"done\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// isolateInterpreterSearch removes the host's real Python from the
// interpreter search, so the scan can only find the test's fake.
//
// The search consults absolute paths (/opt/homebrew/bin, /usr/local/bin,
// /usr/bin, and the python.org framework) in addition to PATH, so rewriting
// PATH alone is not enough: on a developer machine the real python3 — which
// has onnx-asr installed — would be found and the install gate would never be
// reached. These vars are the seam that makes the gate testable.
//
// The vars are process-wide, so a test that mutates them must not run in
// parallel with another that does. Nothing in this package calls
// t.Parallel, so that holds today.
func isolateInterpreterSearch(t *testing.T) {
	t.Helper()
	origDirs := pyinterp.ExtraCandidateDirs
	origGlobs := pyinterp.FrameworkCandidateGlobs
	pyinterp.ExtraCandidateDirs = nil
	pyinterp.FrameworkCandidateGlobs = nil
	t.Cleanup(func() {
		pyinterp.ExtraCandidateDirs = origDirs
		pyinterp.FrameworkCandidateGlobs = origGlobs
	})
}

// TestTranscribeAsksForInstallBeforeStartingAWorker is the hermetic install
// gate test.
//
// It pins the whole contract in one place: when the local engine's
// dependencies are missing and the user has not approved the install,
// Transcribe must return *InstallRequiredError and must NOT start a worker.
// The 409 the server turns that error into is what prompts the user, so a
// worker started before the prompt would mean a model download behind a
// dialog the user never saw.
//
// The test is hermetic: the interpreter search is restricted to a fake that
// cannot be bypassed, so nothing is downloaded, no venv is built, and the
// test finishes in milliseconds.
//
// The pre-existing TestPartialTranscribeNeverInstalls and
// TestTranscribeAsksBeforeInstalling cannot make that guarantee: their
// interpreter scan falls through to the host's real python3 (which has
// onnx_asr installed), the install gate is skipped, and the test downloads
// the whole model into a temp dir.
func TestTranscribeAsksForInstallBeforeStartingAWorker(t *testing.T) {
	resetProbeCache(t)
	isolateInterpreterSearch(t)
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)

	fake := writeVersionOnlyPython(t)
	// A PATH with ONLY the fake's directory, so the unversioned "python3"
	// candidate cannot resolve either.
	t.Setenv("PATH", filepath.Dir(fake))

	stopActiveWorker()
	t.Cleanup(stopActiveWorker)
	before := workerStartCount()

	wav := filepath.Join(t.TempDir(), "a.wav")
	if err := os.WriteFile(wav, []byte("RIFFxxxxWAVE"), 0o600); err != nil {
		t.Fatal(err)
	}

	opts := Options{Model: "parakeet-tdt-0.6b-v3", ModelDir: t.TempDir()}
	res, err := Transcribe(context.Background(), opts, wav)

	var req *InstallRequiredError
	if !errors.As(err, &req) {
		t.Fatalf("err = %v, want *InstallRequiredError", err)
	}
	if len(req.Packages) == 0 {
		t.Errorf("requirement = %+v, want the packages to be named", req)
	}
	if res.Text != "" {
		t.Errorf("text = %q, want no transcript when the install was refused", res.Text)
	}
	// The load-bearing assertion: the gate must run BEFORE the worker.
	//
	// The count of spawn ATTEMPTS is the right thing to assert, not whether a
	// worker is running: a spawn that fails leaves no active worker, so
	// "is a worker running" cannot tell "the gate ran first" from "the worker
	// was started and then failed".
	if n := workerStartCount() - before; n != 0 {
		t.Fatalf("%d worker spawn(s) were attempted even though the install was not approved; "+
			"the install gate must be evaluated before any worker is started", n)
	}
}

// TestPartialTranscribeAlsoAsksForInstallBeforeStartingAWorker is the same
// contract for the live-preview path.
//
// A preview is display-only and must never prompt, so it must also never
// start a worker that would download a model behind the user's back.
func TestPartialTranscribeAlsoAsksForInstallBeforeStartingAWorker(t *testing.T) {
	resetProbeCache(t)
	isolateInterpreterSearch(t)
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)

	fake := writeVersionOnlyPython(t)
	t.Setenv("PATH", filepath.Dir(fake))

	stopActiveWorker()
	t.Cleanup(stopActiveWorker)
	before := workerStartCount()

	raw, err := os.ReadFile(filepath.Join("testdata", "speech-16k.wav"))
	if err != nil {
		t.Skip("no test wav")
	}
	live := filepath.Join(t.TempDir(), "live.wav")
	if err := os.WriteFile(live, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	opts := Options{Model: "parakeet-tdt-0.6b-v3", ModelDir: t.TempDir()}
	_, err = PartialTranscribe(context.Background(), opts, &Recording{path: live})

	var req *InstallRequiredError
	if !errors.As(err, &req) {
		t.Fatalf("err = %v, want *InstallRequiredError", err)
	}
	if n := workerStartCount() - before; n != 0 {
		t.Fatalf("a preview attempted %d worker spawn(s) even though the install was not approved", n)
	}
}

// TestTranscribeInstallGateRunsBeforeTheWorker proves the ORDER, not just
// the outcome.
//
// A test that only checks the returned error would still pass if the worker
// started first and the gate happened to run afterwards. This one points the
// model directory at a path that cannot be used, so reaching the worker
// would produce a DIFFERENT error, and asserts the error is still the install
// requirement.
func TestTranscribeInstallGateRunsBeforeTheWorker(t *testing.T) {
	resetProbeCache(t)
	isolateInterpreterSearch(t)
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)

	fake := writeVersionOnlyPython(t)
	t.Setenv("PATH", filepath.Dir(fake))

	stopActiveWorker()
	t.Cleanup(stopActiveWorker)

	wav := filepath.Join(t.TempDir(), "a.wav")
	if err := os.WriteFile(wav, []byte("RIFFxxxxWAVE"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A ModelDir that does not exist: the worker could not start even if it
	// were reached, and the failure would be a different error.
	opts := Options{
		Model:    "parakeet-tdt-0.6b-v3",
		ModelDir: filepath.Join(t.TempDir(), "not-created"),
	}
	_, err := Transcribe(context.Background(), opts, wav)
	var req *InstallRequiredError
	if !errors.As(err, &req) {
		t.Fatalf("err = %v, want *InstallRequiredError (the gate must run before the worker)", err)
	}
}
