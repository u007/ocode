// Package sandbox confines agent shell commands to a write-integrity boundary:
// filesystem writes outside the classified allowed roots fail at the OS level,
// while reads, exec, and network egress stay open. It is not a confidentiality
// boundary — a sandboxed command can still read secrets and send them out.
package sandbox

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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
//
// A non-writable spec that resolves strictly inside a writable spec's
// directory is honored as a protected-file carve-out: rather than grant the
// writable root's whole subtree (which would silently re-expose the
// protected path — e.g. auth.json living inside the writable global data
// dir — to every backend), the root is expanded into its directory entries,
// recursing into and excluding whichever branch actually contains the
// protected path. This lets a caller mark a directory writable while still
// keeping specific files inside it non-writable at the OS level, independent
// of any permission-layer Ask-gate.
func NewRootSet(specs []RootSpec) RootSet {
	var writableRoots, protectedPaths []string
	for _, spec := range specs {
		if spec.Path == "" {
			continue
		}
		if spec.Writable {
			if spec.Path == "/" {
				continue
			}
			writableRoots = append(writableRoots, spec.Path)
			continue
		}
		protectedPaths = append(protectedPaths, spec.Path)
	}
	var writable []string
	for _, root := range writableRoots {
		nested := descendantsOf(root, protectedPaths)
		if len(nested) == 0 {
			writable = append(writable, root)
			continue
		}
		writable = append(writable, expandWritableRoot(root, nested)...)
	}
	sort.Strings(writable)
	return RootSet{
		WritableRoots: writable,
		ReadRoots:     nil, // intentional: whole FS readable/executable
		NetworkEgress: true,
	}
}

// descendantsOf returns the subset of paths that resolve strictly inside dir
// (dir itself never counts as its own descendant).
func descendantsOf(dir string, paths []string) []string {
	var out []string
	for _, p := range paths {
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// expandWritableRoot lists dir's immediate entries and grants each as its own
// writable root, except one that IS a protected path (dropped entirely) or
// contains one (recursed into, so only the offending branch loses the
// coarse grant). dir itself is never returned, so protected files directly
// inside it get no write grant from this root at all. If dir cannot be
// listed, it returns no grants rather than falling back to the unexpanded
// (unprotected) root — a protected file must never leak from a read failure.
func expandWritableRoot(dir string, protected []string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		child := filepath.Join(dir, e.Name())
		if containsPath(protected, child) {
			continue
		}
		nested := descendantsOf(child, protected)
		if len(nested) == 0 {
			out = append(out, child)
			continue
		}
		if !e.IsDir() {
			// A protected path claims to live inside a non-directory entry —
			// should not happen, but skip rather than grant defensively.
			continue
		}
		out = append(out, expandWritableRoot(child, nested)...)
	}
	return out
}

func containsPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}
