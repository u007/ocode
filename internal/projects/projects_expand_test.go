package projects

import (
	"os"
	"path/filepath"
	"testing"
)

// ExpandHome rewrites a leading ~ or ~/ using os.UserHomeDir.
func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"tilde only", "~", home},
		{"tilde slash", "~/x", filepath.Join(home, "x")},
		{"tilde dotdot", "~/../a", filepath.Join(home, "..", "a")},
		{"abs unchanged", "/abs/path", "/abs/path"},
		{"dot unchanged", "./rel", "./rel"},
		{"user prefix unchanged", "~bob/x", "~bob/x"},
		{"empty unchanged", "", ""},
		{"no prefix unchanged", "hello", "hello"},
		{"two tilde unchanged", "~~", "~~"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandHome(tt.in)
			if err != nil {
				t.Fatalf("ExpandHome(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ExpandHome(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ExpandHome returns an error when os.UserHomeDir fails.
func TestExpandHomeUserHomeDirError(t *testing.T) {
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", "")
	origProfile := os.Getenv("USERPROFILE")
	t.Setenv("USERPROFILE", "")
	defer func() {
		if origHome != "" {
			os.Setenv("HOME", origHome)
		}
		if origProfile != "" {
			os.Setenv("USERPROFILE", origProfile)
		}
	}()
	_, err := ExpandHome("~/x")
	if err == nil {
		t.Fatal("expected error when os.UserHomeDir fails")
	}
}

// Store.Add expands ~ paths against the server's home directory.
func TestStoreAddExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Add("~/myapp"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	list := store.List()
	if len(list) != 1 {
		t.Fatalf("list length = %d, want 1", len(list))
	}
	want := filepath.Join(home, "myapp")
	if list[0].Path != want {
		t.Errorf("list[0].Path = %q, want %q", list[0].Path, want)
	}
	if list[0].Name != "myapp" {
		t.Errorf("list[0].Name = %q, want myapp", list[0].Name)
	}
}

// Store.Add stores absolute paths unchanged.
func TestStoreAddAbsolutePathUnchanged(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Add("/some/abs/path"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	list := store.List()
	if len(list) != 1 {
		t.Fatalf("list length = %d, want 1", len(list))
	}
	if list[0].Path != "/some/abs/path" {
		t.Errorf("list[0].Path = %q, want /some/abs/path", list[0].Path)
	}
}
