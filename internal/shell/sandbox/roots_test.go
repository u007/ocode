package sandbox

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// TestNewRootSetSplitsByCapability locks the RootSet contract: WritableRoots
// holds exactly the writable, non-"/" specs; ReadRoots stays empty (whole FS
// readable/executable — write-integrity only); NetworkEgress is always true.
func TestNewRootSetSplitsByCapability(t *testing.T) {
	specs := []RootSpec{
		{Path: "/Users/test/project", Writable: true},
		{Path: "/tmp", Writable: true},
		{Path: "/", Writable: true}, // boundary guard: must be dropped
		{Path: "/Users/test/.local/share/opencode", Writable: false},
		{Path: "/Users/test/.cache", Writable: false},
	}
	rs := NewRootSet(specs)

	want := []string{"/Users/test/project", "/tmp"}
	if !reflect.DeepEqual(rs.WritableRoots, want) {
		t.Fatalf("WritableRoots = %v, want %v", rs.WritableRoots, want)
	}
	if len(rs.ReadRoots) != 0 {
		t.Fatalf("ReadRoots = %v, want empty (whole FS readable)", rs.ReadRoots)
	}
	if !rs.NetworkEgress {
		t.Fatal("NetworkEgress = false, want true (egress intentionally open)")
	}
}

// TestNewRootSetEmptySpecs covers the degenerate input: nil specs still produce
// a usable RootSet.
func TestNewRootSetEmptySpecs(t *testing.T) {
	rs := NewRootSet(nil)
	if len(rs.WritableRoots) != 0 {
		t.Fatalf("WritableRoots = %v, want empty for nil specs", rs.WritableRoots)
	}
	if !rs.NetworkEgress {
		t.Fatal("NetworkEgress must default true")
	}
}

// TestNewRootSetCarvesOutProtectedFile locks the protected-file contract: a
// non-writable spec nested inside a writable one (e.g. auth.json inside the
// writable data dir) must never appear in WritableRoots, and must not cause
// its writable siblings to lose their grant either.
func TestNewRootSetCarvesOutProtectedFile(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"auth.json", "sessions", "memory"} {
		if name == "auth.json" {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	authPath := filepath.Join(dir, "auth.json")
	specs := []RootSpec{
		{Path: dir, Writable: true},
		{Path: authPath, Writable: false},
	}
	rs := NewRootSet(specs)
	for _, r := range rs.WritableRoots {
		if r == authPath {
			t.Fatalf("auth.json leaked into WritableRoots: %v", rs.WritableRoots)
		}
		if r == dir {
			t.Fatalf("data dir itself must be expanded, not granted wholesale: %v", rs.WritableRoots)
		}
	}
	want := []string{filepath.Join(dir, "memory"), filepath.Join(dir, "sessions")}
	sort.Strings(want)
	if !reflect.DeepEqual(rs.WritableRoots, want) {
		t.Fatalf("WritableRoots = %v, want %v", rs.WritableRoots, want)
	}
}

// TestNewRootSetCarvesOutNestedProtectedFile covers a protected file that
// lives one directory deeper than the writable root: only the offending
// branch should lose its coarse grant, expanded down to its own siblings.
func TestNewRootSetCarvesOutNestedProtectedFile(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "project")
	if err := os.MkdirAll(filepath.Join(sub, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	protectedFile := filepath.Join(sub, "secret.json")
	if err := os.WriteFile(protectedFile, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	specs := []RootSpec{
		{Path: dir, Writable: true},
		{Path: protectedFile, Writable: false},
	}
	rs := NewRootSet(specs)
	for _, r := range rs.WritableRoots {
		if r == protectedFile || r == sub || r == dir {
			t.Fatalf("protected file's branch was not expanded correctly: %v", rs.WritableRoots)
		}
	}
	want := []string{filepath.Join(dir, "memory"), filepath.Join(sub, "sessions")}
	sort.Strings(want)
	if !reflect.DeepEqual(rs.WritableRoots, want) {
		t.Fatalf("WritableRoots = %v, want %v", rs.WritableRoots, want)
	}
}
