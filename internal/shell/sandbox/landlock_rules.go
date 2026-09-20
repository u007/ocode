package sandbox

import (
	"path/filepath"
)

// bwrapReadOnlyRoot is the trusted absolute path for bubblewrap. Using a
// fixed absolute path (not $PATH) matches the plan's trusted-resolution
// requirement and avoids PATH-shadowing bypasses.
const bwrapReadOnlyAbs = "/usr/bin/bwrap"

// buildBwrapArgv constructs the bubblewrap argv that confines writes to
// writableRoots while keeping the rest of the filesystem read-only, network
// egress open, and /proc mounted. The command (bash -c <script>) is appended
// verbatim by the caller. Non-existent writable roots are SKIPPED (binding a
// missing source would fail bwrap startup; skipping never widens the boundary
// — nothing exists there to write to). Roots are realpath'd first so a
// symlink alias resolves to the canonical path bwrap binds.
func buildBwrapArgv(writableRoots []string, bashArgs []string) []string {
	argv := []string{bwrapReadOnlyAbs,
		"--ro-bind", "/", "/",
		"--proc", "/proc",
		"--dev", "/dev",
		"--share-net",
		"--die-with-parent",
	}
	for _, root := range writableRoots {
		if root == "" || root == "/" {
			continue
		}
		canonical, err := filepath.EvalSymlinks(root)
		if err != nil {
			// Missing/unresolvable root: skip — never widen.
			continue
		}
		argv = append(argv, "--bind", canonical, canonical)
	}
	argv = append(argv, bashArgs...)
	return argv
}

// Named Landlock filesystem access rights (values mirror <linux/landlock.h>).
// landlockWriteFile lives with the mutation bits below; the rest are grouped
// here so the file-vs-directory split is legible.
const (
	landlockExecute  = 0x1
	landlockReadFile = 0x4
	landlockReadDir  = 0x8
)

// landlockReadExec is the broad read+exec right set granted over the whole
// filesystem (ABI-v1-safe core, no TRUNCATE/REFER/IOCTL).
const landlockReadExec = landlockExecute | landlockReadFile | landlockReadDir

// landlockFileRights is the subset of the filesystem rights Landlock accepts
// when a rule's parent_fd refers to a regular file rather than a directory:
// landlock_add_rule(2) returns EINVAL if the allowed mask carries a
// directory-only right (READ_DIR, REMOVE_DIR/REMOVE_FILE, every MAKE_*, and
// REFER) for a non-directory. A writable root is normally a directory, but the
// permission layer also grants individual FILES for write — projects.json and
// the global git-ignore files, which NewRootSet's protected-file carve-out
// expands out of the writable global data dir. IOCTL_DEV is deliberately
// absent: the mutation set never handles it (see landlockMutationForABI), so it
// can never appear in a rule's allowed mask.
const landlockFileRights = landlockExecute | landlockWriteFile | landlockReadFile | landlockTruncate

// landlockApplicableRights masks allowed down to the rights Landlock accepts
// for the object type behind fd. Without this a FILE writable root aborts the
// whole ruleset build ("landlock rule for ...: invalid argument") and, because
// the confiner is fail-closed, every sandboxed command dies before it runs.
func landlockApplicableRights(allowed uint64, isDir bool) uint64 {
	if isDir {
		return allowed
	}
	return allowed & landlockFileRights
}

// landlockNullDevice is the singleton discard target outside every writable
// root that tools open O_WRONLY/O_RDWR (`2>/dev/null`, git, ssh, pagers).
// Landlock denies an open that no rule grants, so it gets an explicit write
// grant — the Linux counterpart of the Seatbelt profile's /dev/null path rule
// (see profile.go). /dev/tty is DELIBERATELY NOT granted, for the same
// terminal-control reason documented there.
const landlockNullDevice = "/dev/null"

// landlockMutation is the full write/mutation right set, gated per ABI below.
const (
	landlockWriteFile  = 0x2
	landlockRemoveDir  = 0x10
	landlockRemoveFile = 0x20
	landlockMakeBlock  = 0x800
	landlockMakeChar   = 0x40
	landlockMakeDir    = 0x80
	landlockMakeFifo   = 0x400
	landlockMakeReg    = 0x100
	landlockMakeSock   = 0x200
	landlockMakeSym    = 0x1000
	landlockRefer      = 0x2000 // ABI v2: rename/link across roots
	landlockTruncate   = 0x4000 // ABI v3: truncate
	landlockIoctlDev   = 0x8000 // ABI v4: ioctl on devices
)

// landlockMutationForABI returns the mutation bits available at the given
// Landlock ABI version (1..5). ABI v1 has no REFER/TRUNCATE; v3 adds
// TRUNCATE; v4 adds IOCTL_DEV (excluded — ioctl on devices is out of scope
// for a write-integrity sandbox and broad ioctl access is risky).
func landlockMutationForABI(abi int) uint64 {
	mut := uint64(landlockWriteFile | landlockRemoveDir | landlockRemoveFile |
		landlockMakeBlock | landlockMakeChar | landlockMakeDir | landlockMakeFifo |
		landlockMakeReg | landlockMakeSock | landlockMakeSym)
	if abi >= 2 {
		mut |= landlockRefer
	}
	if abi >= 3 {
		mut |= landlockTruncate
	}
	return mut
}

// landlockWritableRights is the full right set for a writable root: the broad
// read+exec set plus every mutation bit available at the ABI.
func landlockWritableRights(abi int) uint64 {
	return landlockReadExec | landlockMutationForABI(abi)
}
