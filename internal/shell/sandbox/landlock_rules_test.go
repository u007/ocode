package sandbox

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestBuildBwrapArgvLocksShape locks the bwrap argv contract: ro-bind "/" ->
// rw binds per writable (canonical) root -> command argv; egress shared, /proc
// mounted, die-with-parent.
func TestBuildBwrapArgvLocksShape(t *testing.T) {
	work := t.TempDir()
	argv := buildBwrapArgv([]string{work}, []string{"/bin/bash", "-c", "echo hi"})

	want := []string{
		"/usr/bin/bwrap",
		"--ro-bind", "/", "/",
		"--proc", "/proc",
		"--dev", "/dev",
		"--share-net",
		"--die-with-parent",
	}
	canonical, err := filepath.EvalSymlinks(work)
	if err != nil {
		canonical = work
	}
	want = append(want, "--bind", canonical, canonical)
	want = append(want, "/bin/bash", "-c", "echo hi")

	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("argv = %v\nwant = %v", argv, want)
	}
}

// TestBuildBwrapArgvSkipsMissingRoots locks the never-widen rule: a
// non-existent writable root is skipped, not bound.
func TestBuildBwrapArgvSkipsMissingRoots(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	work := t.TempDir()
	argv := buildBwrapArgv([]string{missing, work}, []string{"/bin/bash", "-c", "true"})
	for i, a := range argv {
		if a == missing {
			t.Fatalf("missing root %q leaked into argv at %d: %v", missing, i, argv)
		}
	}
	canonical, _ := filepath.EvalSymlinks(work)
	if canonical != "" {
		found := false
		for i := 0; i < len(argv)-1; i++ {
			if argv[i] == "--bind" && argv[i+1] == canonical {
				found = true
			}
		}
		if !found {
			t.Fatalf("existing root %s not bound in %v", canonical, argv)
		}
	}
}

// TestBuildBwrapArgvSkipsFilesystemRoot locks the "/" boundary guard.
func TestBuildBwrapArgvSkipsFilesystemRoot(t *testing.T) {
	argv := buildBwrapArgv([]string{"/"}, []string{"/bin/bash", "-c", "true"})
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "--bind" && argv[i+1] == "/" {
			t.Fatalf("writable root / bound: %v", argv)
		}
	}
}

// TestLandlockMutationForABI locks the ABI gating: REFER only from ABI 2,
// TRUNCATE only from ABI 3, ioctl never granted.
func TestLandlockMutationForABI(t *testing.T) {
	if got := landlockMutationForABI(1); got&landlockRefer != 0 || got&landlockTruncate != 0 {
		t.Fatalf("ABI1 mutation 0x%x must exclude REFER/TRUNCATE", got)
	}
	if got := landlockMutationForABI(2); got&landlockRefer == 0 || got&landlockTruncate != 0 {
		t.Fatalf("ABI2 mutation 0x%x must include REFER, exclude TRUNCATE", got)
	}
	if got := landlockMutationForABI(3); got&landlockRefer == 0 || got&landlockTruncate == 0 {
		t.Fatalf("ABI3 mutation 0x%x must include REFER and TRUNCATE", got)
	}
	for abi := 1; abi <= 5; abi++ {
		if landlockMutationForABI(abi)&landlockIoctlDev != 0 {
			t.Fatalf("ABI%d must never grant IOCTL_DEV", abi)
		}
	}
}

// TestLandlockWritableRightsIncludesReadExecAndMutation locks the writable
// root right set: broad read+exec plus all mutation bits.
func TestLandlockWritableRightsIncludesReadExecAndMutation(t *testing.T) {
	rights := landlockWritableRights(1)
	if rights&landlockReadExec == 0 {
		t.Fatalf("writable rights 0x%x missing read+exec core %#x", rights, landlockReadExec)
	}
	if rights&landlockWriteFile == 0 || rights&landlockMakeReg == 0 || rights&landlockRemoveFile == 0 {
		t.Fatalf("writable rights 0x%x missing mutation bits", rights)
	}
}

// TestConfineEnvFilterRoundTrip checks the OCODE_SANDBOX_* protocol vars are
// stripped for the confined child.
func TestConfineEnvFilterRoundTrip(t *testing.T) {
	base := []string{"PATH=/bin", "OCODE_SANDBOX_ROOTS=[\"/x\"]", "HOME=/home/u", "OCODE_SANDBOX_DIR=/x"}
	got := stripSandboxEnv(base)
	if len(got) != 2 || got[0] != "PATH=/bin" || got[1] != "HOME=/home/u" {
		t.Fatalf("stripSandboxEnv = %v, want just PATH/HOME", got)
	}
}
