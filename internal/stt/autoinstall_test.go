package stt

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeInstallablePython writes a POSIX stand-in for a base Python whose
// `-m venv <dir>` creates a venv whose own python imports fine, and whose
// `-m pip install` succeeds. Every invocation is appended to log.
func writeInstallablePython(t *testing.T, log string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake interpreter scripts need a POSIX shell")
	}
	path := filepath.Join(t.TempDir(), "python3.18")
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + log + "\n" +
		"if [ \"$1\" = --version ]; then echo 'Python 3.18.0'; exit 0; fi\n" +
		"if [ \"$1\" = -m ] && [ \"$2\" = venv ]; then\n" +
		"  mkdir -p \"$3/bin\" && cp \"$0\" \"$3/bin/python\" && exit 0\n" +
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

// An unpinned host with a base Python but no onnx_asr builds the managed venv,
// pip-installs the packages into it and returns the venv's interpreter.
func TestInstallLocalBuildsManagedVenv(t *testing.T) {
	resetProbeCache(t)
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	log := filepath.Join(t.TempDir(), "calls.log")
	base := writeInstallablePython(t, log)
	t.Setenv("PATH", filepath.Dir(base)+string(os.PathListSeparator)+os.Getenv("PATH"))

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
func TestAutoInstallSkipsPinnedPythonAndPartials(t *testing.T) {
	if (Options{Python: "/x/python3"}).autoInstall() {
		t.Error("auto-install enabled for a pinned python")
	}
	if (Options{NoAutoInstall: true}).autoInstall() {
		t.Error("auto-install enabled despite NoAutoInstall")
	}
	if !(Options{}).autoInstall() {
		t.Error("auto-install should be on by default")
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
	t.Setenv("PATH", filepath.Dir(base)+string(os.PathListSeparator)+os.Getenv("PATH"))

	st := StatusFor(Options{})
	for _, m := range st.Models {
		if m.Engine != EngineLocal {
			continue
		}
		if !m.Available || m.Reason != autoInstallNote {
			t.Errorf("%s available=%v reason=%q, want installable", m.ID, m.Available, m.Reason)
		}
	}
	st = StatusFor(Options{NoAutoInstall: true})
	for _, m := range st.Models {
		if m.Engine == EngineLocal && m.Available {
			t.Errorf("%s available with NoAutoInstall and no deps", m.ID)
		}
	}
}
