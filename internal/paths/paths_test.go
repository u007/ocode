package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGlobalDataDir(t *testing.T) {
	dir, err := GlobalDataDir()
	if err != nil {
		t.Fatalf("GlobalDataDir() error: %v", err)
	}
	if dir == "" {
		t.Fatal("GlobalDataDir() returned empty string")
	}

	// Verify directory was created
	if info, err := os.Stat(dir); err != nil {
		t.Fatalf("GlobalDataDir() dir does not exist: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("GlobalDataDir() path is not a directory: %s", dir)
	}

	// Verify platform-specific expectations
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		expected := filepath.Join(home, "Library", "Application Support", AppName)
		if dir != expected {
			t.Errorf("macOS: got %s, want %s", dir, expected)
		}
	case "linux":
		xdg := os.Getenv("XDG_DATA_HOME")
		var expected string
		if xdg != "" {
			expected = filepath.Join(xdg, AppName)
		} else {
			expected = filepath.Join(home, ".local", "share", AppName)
		}
		if dir != expected {
			t.Errorf("linux: got %s, want %s", dir, expected)
		}
	}
}

func TestProjectSessionsDir(t *testing.T) {
	dir, err := ProjectSessionsDir("abc123")
	if err != nil {
		t.Fatalf("ProjectSessionsDir() error: %v", err)
	}
	base, _ := GlobalDataDir()
	expected := filepath.Join(base, "project", "abc123", "sessions")
	if dir != expected {
		t.Errorf("got %s, want %s", dir, expected)
	}
}

func TestProjectUsageDir(t *testing.T) {
	dir, err := ProjectUsageDir()
	if err != nil {
		t.Fatalf("ProjectUsageDir() error: %v", err)
	}
	base, _ := GlobalDataDir()
	expected := filepath.Join(base, "usage")
	if dir != expected {
		t.Errorf("got %s, want %s", dir, expected)
	}
}
