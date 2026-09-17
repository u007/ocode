package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The configured terminal_shell override must be honored when it is usable.
func TestResolveTerminalShellPrefersUsableOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell resolution only")
	}
	dir := t.TempDir()
	shell := filepath.Join(dir, "myshell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", "/bin/sh")
	if got := ResolveTerminalShell(shell); got != shell {
		t.Fatalf("ResolveTerminalShell(%q) = %q, want the override", shell, got)
	}
}

// The failure this guards: a terminal_shell value naming a shell the host does
// not have (the local config synced to a remote, or a stale value) must fall
// back instead of being handed to exec.
func TestResolveTerminalShellRejectsUnusableOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell resolution only")
	}
	got := ResolveTerminalShell("/nonexistent-xyz/zsh")
	if got == "/nonexistent-xyz/zsh" {
		t.Fatal("ResolveTerminalShell returned the unusable override")
	}
	if got == "" {
		t.Fatal("ResolveTerminalShell returned an empty shell")
	}
}

// DefaultTerminalShell must never return a $SHELL that does not exist.
func TestDefaultTerminalShellRejectsStaleSHELL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell resolution only")
	}
	t.Setenv("SHELL", "/nonexistent-xyz/zsh")
	got := DefaultTerminalShell()
	if got == "/nonexistent-xyz/zsh" {
		t.Fatal("DefaultTerminalShell returned a stale $SHELL")
	}
	if got == "" {
		t.Fatal("DefaultTerminalShell returned an empty shell")
	}
}

// A usable $SHELL still wins over the built-in fallbacks.
func TestDefaultTerminalShellHonorsUsableSHELL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell resolution only")
	}
	t.Setenv("SHELL", "/bin/sh")
	if got := DefaultTerminalShell(); got != "/bin/sh" {
		t.Fatalf("DefaultTerminalShell() = %q, want /bin/sh", got)
	}
}

// AvailableShells is the picker's source; every entry must be executable.
func TestAvailableShellsAreExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix /etc/shells only")
	}
	for _, s := range AvailableShells() {
		info, err := os.Stat(s)
		if err != nil || info.IsDir() {
			t.Fatalf("AvailableShells reported unusable %q", s)
		}
	}
}
