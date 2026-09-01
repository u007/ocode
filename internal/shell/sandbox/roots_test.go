package sandbox

import (
	"reflect"
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