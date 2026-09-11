package tailscale

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSanitizePath(t *testing.T) {
	if got := SanitizePath("ses_abc-123_X"); got != "/ses_abc-123_X" {
		t.Fatalf("got %q", got)
	}
	if got := SanitizePath("a/b.c"); got != "/abc" {
		t.Fatalf("got %q", got)
	}
	if got := SanitizePath(""); got != "/ocode" {
		t.Fatalf("got %q", got)
	}
	if got := SanitizePath("desktop"); got != "/desktop" {
		t.Fatalf("got %q", got)
	}
}

func TestURLWithPathPrefixReplacesStalePrefix(t *testing.T) {
	got := URLWithPathPrefix("https://host.ts.net/stale", "/ours")
	if got != "https://host.ts.net/ours" {
		t.Fatalf("got %q", got)
	}
	if got := URLWithPathPrefix("", "/ours"); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := URLWithPathPrefix("https://host.ts.net", ""); got != "https://host.ts.net" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildSessionURL(t *testing.T) {
	got := BuildSessionURL("https://host.ts.net/desktop/", "ses_1", "tok")
	if got != "https://host.ts.net/desktop/session/ses_1?token=tok" {
		t.Fatalf("got %q", got)
	}
}

func TestFindCLIImpl(t *testing.T) {
	tmpDir := t.TempDir()

	executable := filepath.Join(tmpDir, "tailscale")
	nonExecutable := filepath.Join(tmpDir, "stale")

	// Create an executable file (mimics a real CLI binary).
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	// Create a non-executable file (mimics a stale/partial install).
	if err := os.WriteFile(nonExecutable, []byte("partial"), 0644); err != nil {
		t.Fatal(err)
	}

	// PATH resolution wins over known candidates.
	lookPath := func(string) (string, error) { return "/from/path/tailscale", nil }
	if got := findCLIImpl([]string{executable}, lookPath); got != "/from/path/tailscale" {
		t.Fatalf("PATH lookup: got %q, want /from/path/tailscale", got)
	}

	// PATH miss falls through to first executable candidate.
	lookPathMiss := func(string) (string, error) { return "", exec.ErrNotFound }
	if got := findCLIImpl([]string{nonExecutable, executable}, lookPathMiss); got != executable {
		t.Fatalf("fallback: got %q, want %q", got, executable)
	}

	// Non-executable candidate is skipped, next valid candidate wins.
	if got := findCLIImpl([]string{nonExecutable, executable}, lookPathMiss); got != executable {
		t.Fatalf("skip-nonexec: got %q, want %q", got, executable)
	}

	// All candidates invalid returns empty.
	if got := findCLIImpl([]string{nonExecutable}, lookPathMiss); got != "" {
		t.Fatalf("all-invalid: got %q, want empty", got)
	}

	// No candidates at all returns empty even with PATH miss.
	if got := findCLIImpl(nil, lookPathMiss); got != "" {
		t.Fatalf("no-candidates: got %q, want empty", got)
	}
}
