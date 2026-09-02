// Package sandbox confines agent shell commands to a write-integrity boundary:
// filesystem writes outside the classified allowed roots fail at the OS level,
// while reads, exec, and network egress stay open. It is not a confidentiality
// boundary — a sandboxed command can still read secrets and send them out.
package sandbox

import "sort"

// RootSpec is one capability-classified filesystem root. Writable=true means
// the sandbox must allow mutating operations inside the root; Writable=false
// means reads/exec only (the root's integrity is preserved under sandbox).
type RootSpec struct {
	Path     string
	Writable bool
}

// RootSet is the compiled sandbox boundary. ReadRoots empty ⇒ the whole
// filesystem remains readable/executable; only writes are confined, to
// WritableRoots. The two slices are sorted for deterministic profile output.
type RootSet struct {
	WritableRoots []string
	ReadRoots     []string
	NetworkEgress bool
}

// NewRootSet compiles capability-classified specs into the RootSet consumed by
// sandbox backends. The "/" writable boundary guard lives here too: a writable
// filesystem root voids the entire boundary, so it is dropped from
// WritableRoots unconditionally (the permission layer additionally filters
// specs before they reach us).
func NewRootSet(specs []RootSpec) RootSet {
	var writable []string
	for _, spec := range specs {
		if !spec.Writable {
			continue
		}
		if spec.Path == "" || spec.Path == "/" {
			continue
		}
		writable = append(writable, spec.Path)
	}
	sort.Strings(writable)
	return RootSet{
		WritableRoots: writable,
		ReadRoots:     nil, // intentional: whole FS readable/executable
		NetworkEgress: true,
	}
}
