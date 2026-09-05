package sandbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// seatbeltProfileSafe builds the Seatbelt (SBPL) profile for roots, rejecting
// — not sanitizing — any path that would break out of the profile syntax or
// void the write boundary (quotes, newlines, null bytes, or a writable
// filesystem root). The generated profile is a security boundary, so a path we
// cannot emit faithfully must fail the whole command.
// Missing (not-yet-created) roots are SKIPPED, matching the Linux backends
// (canonicalExistingWritables / buildBwrapArgv): binding a missing source
// would fail startup, and skipping never widens the boundary — nothing exists
// there to write to. A missing cache subdir (e.g. ~/.cache/bun) is already
// covered when its existing parent (e.g. ~/.cache) is writable via subpath.
func seatbeltProfileSafe(roots RootSet) (string, error) {
	var sb strings.Builder
	sb.WriteString(`(version 1)
(allow default)
(deny file-write*)
`)
	// Writes confined to the writable roots; reads/exec stay global
	// (write-integrity only). Each root is realpath'd so a symlink-alias input
	// (/tmp, /var/folders — both symlinks on macOS) yields the canonical path
	// the kernel observes; a subpath filter on a symlink alias would not match
	// the real write and would wrongly block writable-root writes (or, worse,
	// fall through to the default deny).
	for _, root := range roots.WritableRoots {
		var canonical string
		canonical, err := canonicalRoot(root)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", err
		}
		if canonical == "/" {
			return "", fmt.Errorf("sandbox: writable root must not be the filesystem root")
		}
		fmt.Fprintf(&sb, "(allow file-write* (subpath %q))\n", canonical)
	}
	// /dev/null sits outside every writable root but is opened O_WRONLY/O_RDWR
	// as a pure discard target by many tools (git, ssh, pagers); a write there
	// is a no-op, so it is granted explicitly. /dev/tty is DELIBERATELY NOT
	// granted: a fresh open of it would let a confined subprocess paint
	// terminal-control sequences over the running TUI, bypassing the captured
	// output path (alt-screen corruption class). Interactive tty users (sudo/gpg
	// passphrase prompts, ssh password prompts, pager keyboards, isatty probes)
	// belong to the unsandboxed PTY terminal, not the agent's shell tool — and
	// those flows should fail closed here rather than silently find the
	// controlling terminal. Writes to already-inherited descriptors (stdout/
	// stderr pipes, the PTY) are not path-resolved operations and are
	// unaffected; /dev/stdout, /dev/stderr, and /dev/fd/* therefore need no
	// path grants either. Empirically probed: git status/log/stash/diff/
	// rev-parse, less --version, ssh -V, gpg --version, npm --version all work
	// without a /dev/tty grant.
	sb.WriteString("(allow file-write* (path \"/dev/null\"))\n")
	// Reads/exec open, so toolchains and interpreters keep working.
	sb.WriteString("(allow file-read*)\n")
	sb.WriteString("(allow process-exec*)\n")
	sb.WriteString("(allow process-fork)\n")
	// Signal delivery within this sandbox so foreground/background lifecycle
	// and kill_shell keep working.
	sb.WriteString("(allow signal (target self))\n")
	// Network egress open (documented design).
	sb.WriteString("(allow network*)\n")
	if roots.NetworkEgress {
		sb.WriteString("(allow network-outbound)\n")
	}
	// Mach lookups bash/toolchains need.
	sb.WriteString("(allow mach-lookup)\n")
	return sb.String(), nil
}

// seatbeltProfile is the panic-free convenience used by the wrapper; it relies
// on the RootSet already being validated by NewRootSet + AllowedRootsClassified
// (the "/" writable guard and escaping are re-checked defensively here).
func seatbeltProfile(roots RootSet) string {
	prof, err := seatbeltProfileSafe(roots)
	if err != nil {
		// Reached only via direct unit-test calls on hostile roots; the real
		// Wrap path pares through seatbeltProfileSafe and returns the error.
		return ""
	}
	return prof
}

// canonicalRoot resolves root to its canonical path (following symlinks) and
// validates it can be emitted safely into an SBPL string — rejecting quotes,
// newlines, null bytes, or an empty path (fail-closed: a root we cannot emit
// faithfully must not broaden or wrongly constrain the boundary).
func canonicalRoot(root string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("sandbox: empty writable root")
	}
	if strings.ContainsAny(root, "\"\n\x00") {
		return "", fmt.Errorf("sandbox: writable root %q contains a quote/newline/null", root)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("sandbox: cannot resolve writable root %q: %w", root, err)
	}
	if strings.ContainsAny(canonical, "\"\n\x00") {
		return "", fmt.Errorf("sandbox: canonical writable root %q contains a quote/newline/null", canonical)
	}
	return canonical, nil
}
