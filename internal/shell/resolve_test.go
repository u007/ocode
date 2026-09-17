package shell

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix exec bits only")
	}
	dir := t.TempDir()
	exec := filepath.Join(dir, "exec")
	if err := os.WriteFile(exec, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !IsExecutable(exec) {
		t.Fatalf("IsExecutable(%q) = false, want true", exec)
	}
	if IsExecutable(plain) {
		t.Fatalf("IsExecutable(%q) = true for a non-executable file, want false", plain)
	}
	if IsExecutable(dir) {
		t.Fatalf("IsExecutable(%q) = true for a directory, want false", dir)
	}
	if IsExecutable(filepath.Join(dir, "missing")) {
		t.Fatal("IsExecutable(missing) = true, want false")
	}
	if IsExecutable("") {
		t.Fatal("IsExecutable(\"\") = true, want false")
	}
}

// A preferred shell that does not exist must NOT be returned — that is exactly
// the `fork/exec /bin/zsh: no such file or directory` failure mode.
func TestResolveRejectsMissingPreferred(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell resolution only")
	}
	t.Setenv("SHELL", "")
	got := Resolve("/nonexistent-xyz/zsh")
	if got == "/nonexistent-xyz/zsh" {
		t.Fatal("Resolve returned the unusable preferred shell")
	}
	if !IsExecutable(got) {
		t.Fatalf("Resolve returned a non-executable shell %q", got)
	}
}

// A present-but-non-executable preferred shell must also be rejected.
func TestResolveRejectsNonExecutablePreferred(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell resolution only")
	}
	plain := filepath.Join(t.TempDir(), "notexec")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", "")
	if got := Resolve(plain); got == plain {
		t.Fatalf("Resolve returned the non-executable %q", got)
	}
}

// A usable preferred shell wins over $SHELL and the fallbacks.
func TestResolvePrefersUsablePreferred(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell resolution only")
	}
	dir := t.TempDir()
	pref := filepath.Join(dir, "myshell")
	if err := os.WriteFile(pref, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", "/bin/sh")
	if got := Resolve(pref); got != pref {
		t.Fatalf("Resolve(%q) = %q, want the preferred shell", pref, got)
	}
}

// With no preferred shell, a stale $SHELL must fall through to a usable one.
func TestResolveFallsThroughStaleSHELL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell resolution only")
	}
	t.Setenv("SHELL", "/nonexistent-xyz/zsh")
	got := Resolve("")
	if got == "/nonexistent-xyz/zsh" {
		t.Fatal("Resolve trusted a stale $SHELL")
	}
	if !IsExecutable(got) && got != "sh" {
		t.Fatalf("Resolve returned unusable %q", got)
	}
}

// Resolve always yields something exec-able on a normal Unix host.
func TestResolveReturnsUsableShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell resolution only")
	}
	t.Setenv("SHELL", "")
	got := Resolve("")
	if got == "" {
		t.Fatal("Resolve returned an empty string")
	}
	if !IsExecutable(got) && got != "sh" {
		t.Fatalf("Resolve returned unusable %q", got)
	}
}

// SystemShells must only report entries that exist and are executable.
func TestSystemShellsAreExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix /etc/shells only")
	}
	for _, s := range SystemShells() {
		if !IsExecutable(s) {
			t.Fatalf("SystemShells reported non-executable %q", s)
		}
	}
}
