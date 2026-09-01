//go:build linux

package sandbox

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// landlockUsable probes whether the kernel supports Landlock at all, and at
// which ABI. The query uses the documented version probe; kernels predating
// the LANDLOCK_CREATE_RULESET_VERSION flag (5.13–5.16) still support ABI 1 if
// the syscall exists and a plain ruleset creation succeeds.
func landlockUsable() bool {
	return landlockABI() > 0
}

// landlockABI returns the probed Landlock ABI version (1..5), or 0 when
// unsupported.
func landlockABI() int {
	version, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	switch {
	case errno == unix.ENOSYS:
		return 0 // syscall number unknown: kernel < 5.13
	case errno != 0:
		// Flag unknown (kernel 5.13–5.16): ABI 1 exists iff a plain ruleset
		// creation succeeds.
		var attr unix.LandlockRulesetAttr
		fd, _, err2 := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), landlockAttrSize, 0)
		if err2 != 0 || fd == 0 {
			return 0
		}
		_ = syscall.Close(int(fd))
		return 1
	default:
		n := int(version)
		if n < 1 {
			n = 1
		}
		if n > 5 {
			n = 5
		}
		return n
	}
}

// landlockAttrSize is the size of the LandlockRulesetAttr with only access_fs
// populated (offsetof(access_net)). Passing this tells the kernel to read only
// the filesystem rights — valid on every ABI and the only field we use (egress
// stays open, so access_net is deliberately not constrained).
const landlockAttrSize = 8

// applyConfineToSelf applies no_new_privs + the full Landlock ruleset for the
// given writable roots to the CURRENT process, then execve's /bin/bash -c.
// It never returns on success (exec replaces the process image).
func applyConfineToSelf(writableRoots []string, command string, env []string) error {
	abi := landlockABI()
	if abi == 0 {
		return errors.New("landlock unsupported")
	}
	if err := setNoNewPrivs(); err != nil {
		return fmt.Errorf("PR_SET_NO_NEW_PRIVS: %w", err)
	}

	mutation := landlockMutationForABI(abi)
	rulesetCap := landlockReadExec | mutation

	var attr unix.LandlockRulesetAttr
	attr.Access_fs = rulesetCap
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attr)), landlockAttrSize, 0)
	if errno != 0 {
		return fmt.Errorf("landlock_create_ruleset: %w", errno)
	}
	rulesetFd := int(fd)
	defer syscall.Close(rulesetFd)

	// Rule 1: broad read+exec on the whole filesystem (writes stay confined by
	// the default-deny of the writable-only rules below).
	if err := landlockAddPathBeneath(rulesetFd, "/", landlockReadExec); err != nil {
		return err
	}
	// Rules 2..n: full writable rights on each canonical writable root.
	for _, root := range writableRoots {
		if err := landlockAddPathBeneath(rulesetFd, root, landlockReadExec|mutation); err != nil {
			return fmt.Errorf("landlock rule for %q: %w", root, err)
		}
	}
	if err := landlockRestrictSelf(rulesetFd); err != nil {
		return fmt.Errorf("landlock_restrict_self: %w", err)
	}

	return syscall.Exec("/bin/bash", []string{"bash", "-c", command}, env)
}

// setNoNewPrivs disables gaining privileges via exec (required before Landlock
// so the confined tree cannot setuid/upgrade past the ruleset).
func setNoNewPrivs() error {
	_, _, errno := unix.RawSyscall6(unix.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// landlockAddPathBeneath grants allowed rights for path and everything beneath
// it, skipping paths that cannot be opened (missing roots — never widen).
func landlockAddPathBeneath(rulesetFd int, path string, allowed uint64) error {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil // missing/unopenable: skip
	}
	defer syscall.Close(fd)
	rule := unix.LandlockPathBeneathAttr{
		Allowed_access: allowed,
		Parent_fd:      int32(fd),
	}
	_, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE,
		uintptr(rulesetFd), unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&rule)), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// landlockRestrictSelf applies the built ruleset to the current process.
func landlockRestrictSelf(rulesetFd int) error {
	_, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, uintptr(rulesetFd), 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
