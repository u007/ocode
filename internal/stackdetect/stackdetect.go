// Package stackdetect answers one question: which known tech stacks does the
// repository at a given root use? It is the runtime half of the detection
// contract documented in docs/okf/_schema/stack-detection.md — the Kaizen
// per-model benchmark uses these stack ids to decide which per-(stack × model)
// tuning applies.
//
// Detection is intentionally cheap and dependency-free: it reads package.json
// (for dependency markers) and checks for marker files (globs). It does NOT
// walk the whole tree or grep file contents; that keeps it fast enough to run
// once per session on the working directory.
package stackdetect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// markerKind distinguishes the two supported marker types.
type markerKind int

const (
	// markerDep matches a dependency name present in package.json
	// (dependencies or devDependencies).
	markerDep markerKind = iota
	// markerFile matches when a file glob resolves to at least one path
	// under the repo root.
	markerFile
)

type marker struct {
	kind markerKind
	// dep: the exact dependency name to look for in package.json.
	dep string
	// file: a glob (relative to root) that must match ≥1 path.
	glob string
}

// stack is a detectable tech stack and its markers. A stack is detected when
// ANY of its markers matches (OR semantics), mirroring `detection.mode: any`
// in the meta.yaml contract.
type stack struct {
	id      string
	markers []marker
}

// registry mirrors the detection blocks in docs/okf/<stack>/meta.yaml. Keep the
// two in sync: meta.yaml is the documented contract, this is its runtime
// mirror. When a new stack corpus is added under docs/okf/, add its markers
// here (and a test case).
//
// The `content` marker type in the contract (regex over file bodies) is
// deliberately not implemented here — every shipped stack is identifiable by a
// dependency or a marker file, which is far cheaper than scanning file bodies.
var registry = []stack{
	{
		id: "react",
		markers: []marker{
			{kind: markerDep, dep: "react"},
		},
	},
	{
		id: "tanstack",
		markers: []marker{
			{kind: markerDep, dep: "@tanstack/react-query"},
			{kind: markerDep, dep: "@tanstack/react-router"},
		},
	},
	{
		id: "nextjs",
		markers: []marker{
			{kind: markerDep, dep: "next"},
			{kind: markerFile, glob: "next.config.*"},
		},
	},
	{
		id: "golang",
		markers: []marker{
			{kind: markerFile, glob: "go.mod"},
		},
	},
	{
		id: "rust",
		markers: []marker{
			{kind: markerFile, glob: "Cargo.toml"},
		},
	},
}

// Detect returns the sorted ids of every known stack the repo at root uses.
// A non-existent or unreadable root yields an empty slice, not an error — the
// caller treats "no stacks detected" and "couldn't tell" the same way (append
// no tuning). Errors reading an individual manifest are swallowed per-marker so
// one malformed package.json can't hide a go.mod sibling.
func Detect(root string) []string {
	if root == "" {
		return nil
	}
	deps := readPackageJSONDeps(root) // parsed once, reused across dep markers

	var found []string
	for _, s := range registry {
		if stackMatches(root, s, deps) {
			found = append(found, s.id)
		}
	}
	sort.Strings(found)
	return found
}

func stackMatches(root string, s stack, deps map[string]struct{}) bool {
	for _, m := range s.markers {
		switch m.kind {
		case markerDep:
			if _, ok := deps[m.dep]; ok {
				return true
			}
		case markerFile:
			matches, err := filepath.Glob(filepath.Join(root, m.glob))
			// filepath.Glob only errors on a malformed pattern (a static
			// registry bug), never on I/O. Treat as no-match and move on;
			// the registry is covered by tests so a bad pattern surfaces there.
			if err == nil && len(matches) > 0 {
				return true
			}
		}
	}
	return false
}

// readPackageJSONDeps returns the union of dependencies and devDependencies
// declared in <root>/package.json as a set. A missing file (the common case in
// non-JS repos) or a parse failure returns an empty set — dep markers simply
// won't match, and file-marker stacks are unaffected.
func readPackageJSONDeps(root string) map[string]struct{} {
	out := map[string]struct{}{}
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		// intentionally not logged: absent package.json is the normal case
		// for Go/Rust repos and is not an error condition.
		return out
	}
	var pkg struct {
		Dependencies    map[string]json.RawMessage `json:"dependencies"`
		DevDependencies map[string]json.RawMessage `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		// intentionally not logged: a malformed package.json must not break
		// detection of file-marker stacks; dep markers just won't match.
		return out
	}
	for name := range pkg.Dependencies {
		out[name] = struct{}{}
	}
	for name := range pkg.DevDependencies {
		out[name] = struct{}{}
	}
	return out
}
