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

// landlockReadExec is the broad read+exec right set granted over the whole
// filesystem (ABI-v1-safe core, no TRUNCATE/REFER/IOCTL).
const landlockReadExec = 0x1 | 0x4 | 0x8 // EXECUTE | READ_FILE | READ_DIR

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
