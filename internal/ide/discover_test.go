package ide

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathContainsLength(t *testing.T) {
	tests := []struct {
		name          string
		parent, child string
		wantPositive  bool
	}{
		{"child inside parent", "/a/b", "/a/b/c/d", true},
		{"equal paths", "/a/b", "/a/b", true},
		{"child outside parent", "/a/b", "/a/c", false},
		{"parent inside child (reversed)", "/a/b/c", "/a/b", false},
		{"sibling prefix not contained", "/a/bc", "/a/bcd", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pathContainsLength(tt.parent, tt.child)
			if (got > 0) != tt.wantPositive {
				t.Fatalf("pathContainsLength(%q,%q)=%d, wantPositive=%v", tt.parent, tt.child, got, tt.wantPositive)
			}
		})
	}
}

// withLockDir points lockDir() at a temp HOME and returns the ide dir.
func withLockDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "ide")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeLock(t *testing.T, dir, port, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, port+".lock"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscover_NoMatch(t *testing.T) {
	dir := withLockDir(t)
	writeLock(t, dir, "4096", `{"transport":"ws","workspaceFolders":["/some/other/project"],"authToken":"x"}`)
	if _, ok := Discover("/Users/me/code/mine"); ok {
		t.Fatal("expected no match")
	}
}

func TestDiscover_LongestWorkspaceWins(t *testing.T) {
	dir := withLockDir(t)
	cwd := filepath.Join(t.TempDir(), "repo", "sub")
	if err := os.MkdirAll(cwd, 0755); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Dir(cwd) // .../repo
	parent := filepath.Dir(repo)

	writeLock(t, dir, "5000", `{"transport":"ws","workspaceFolders":["`+parent+`"],"authToken":"short"}`)
	writeLock(t, dir, "6000", `{"transport":"ws","workspaceFolders":["`+repo+`"],"authToken":"long"}`)

	got, ok := Discover(cwd)
	if !ok {
		t.Fatal("expected a match")
	}
	if got.AuthToken != "long" {
		t.Fatalf("expected longest-match lock (token=long), got %q (port %d)", got.AuthToken, got.Port)
	}
	if got.Port != 6000 {
		t.Fatalf("expected port 6000, got %d", got.Port)
	}
}

func TestDiscover_SkipsNonWSTransport(t *testing.T) {
	dir := withLockDir(t)
	cwd := t.TempDir()
	writeLock(t, dir, "7000", `{"transport":"sse","workspaceFolders":["`+cwd+`"],"authToken":"x"}`)
	if _, ok := Discover(cwd); ok {
		t.Fatal("expected non-ws transport to be skipped")
	}
}
