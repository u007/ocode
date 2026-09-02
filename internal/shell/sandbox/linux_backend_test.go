package sandbox

import (
	"errors"
	"strings"
	"syscall"
	"testing"
)

// TestLinuxBackendSelectsLandlockOrBwrap locks the Linux backend selection
// matrix with injected probes:
//   - Landlock usable → the re-exec confiner is chosen.
//   - only bwrap usable → the bwrap argv is chosen.
//   - neither usable → Available()==false and Wrap errors (fail-closed).
func TestLinuxBackendSelectsLandlockOrBwrap(t *testing.T) {
	fakeExe := "/fake/ocode"
	base := bashCmd("/bin/bash", "-c", "echo hi")

	t.Run("landlock", func(t *testing.T) {
		w := newLinuxWrapper(linuxBackendProbes{
			landlockUsable: func() bool { return true },
			bwrapUsable:    func() bool { return true },
			executable:     func() (string, error) { return fakeExe, nil },
		})
		if !w.Available() {
			t.Fatal("Available() false with landlock usable")
		}
		got, err := w.Wrap(base, RootSet{WritableRoots: []string{"/tmp"}, NetworkEgress: true})
		if err != nil {
			t.Fatalf("wrap error: %v", err)
		}
		if got.Path != fakeExe {
			t.Fatalf("Path = %q, want re-exec %q", got.Path, fakeExe)
		}
		if len(got.Args) < 3 || got.Args[1] != confinerSubcommand || got.Args[2] != "echo hi" {
			t.Fatalf("Args = %v, want [exe sandbox-confine echo hi]", got.Args)
		}
		hasRootsEnv := false
		for _, kv := range got.Env {
			if strings.HasPrefix(kv, envConfineRoots+"=") {
				hasRootsEnv = true
			}
		}
		if !hasRootsEnv {
			t.Fatal("re-exec missing OCODE_SANDBOX_ROOTS env carry")
		}
	})

	t.Run("bwrap-fallback", func(t *testing.T) {
		w := newLinuxWrapper(linuxBackendProbes{
			landlockUsable: func() bool { return false },
			bwrapUsable:    func() bool { return true },
			executable:     func() (string, error) { return fakeExe, nil },
		})
		if !w.Available() {
			t.Fatal("Available() false with bwrap usable")
		}
		got, err := w.Wrap(base, RootSet{WritableRoots: []string{"/tmp"}, NetworkEgress: true})
		if err != nil {
			t.Fatalf("wrap error: %v", err)
		}
		if got.Path != bwrapReadOnlyAbs {
			t.Fatalf("Path = %q, want trusted %q", got.Path, bwrapReadOnlyAbs)
		}
		if got.Args[0] != bwrapReadOnlyAbs {
			t.Fatalf("Args[0] = %q, want bwrap", got.Args[0])
		}
		// The original command argv tail is preserved verbatim.
		n := len(got.Args)
		if n < 3 || got.Args[n-3] != base.Args[0] || got.Args[n-2] != "-c" || got.Args[n-1] != "echo hi" {
			t.Fatalf("bwrap argv tail = %v, want [%s -c echo hi]", got.Args[n-3:], base.Args[0])
		}
	})

	t.Run("neither-fails-closed", func(t *testing.T) {
		w := newLinuxWrapper(linuxBackendProbes{
			landlockUsable: func() bool { return false },
			bwrapUsable:    func() bool { return false },
		})
		if w.Available() {
			t.Fatal("Available() true with no backend")
		}
		if _, err := w.Wrap(base, RootSet{WritableRoots: []string{"/tmp"}, NetworkEgress: true}); err == nil {
			t.Fatal("Wrap with no backend must error (fail-closed)")
		}
	})
}

// TestLinuxWrapPreservesCommandProperties locks property preservation through
// both wrappers: Dir/Env/SysProcAttr/Std pipes all survive the rewrite.
func TestLinuxWrapPreservesCommandProperties(t *testing.T) {
	base := bashCmd("/bin/bash", "-c", "echo hi")
	base.Dir = "/session/root"
	base.Env = []string{"A=B"}
	base.SysProcAttr = &syscall.SysProcAttr{}
	fakeExe := "/fake/ocode"

	for name, tc := range map[string]linuxBackendProbes{
		"landlock": {landlockUsable: func() bool { return true }, executable: func() (string, error) { return fakeExe, nil }},
		"bwrap":    {bwrapUsable: func() bool { return true }},
	} {
		t.Run(name, func(t *testing.T) {
			w := newLinuxWrapper(tc)
			got, err := w.Wrap(base, RootSet{WritableRoots: []string{"/tmp"}, NetworkEgress: true})
			if err != nil {
				t.Fatalf("wrap: %v", err)
			}
			if got.Dir != "/session/root" {
				t.Fatalf("Dir = %q, want preserved", got.Dir)
			}
			if len(got.Env) == 0 || got.Env[0] != "A=B" {
				t.Fatalf("Env = %v, want A=B preserved", got.Env)
			}
			if got.SysProcAttr == nil {
				t.Fatal("SysProcAttr lost through wrap")
			}
		})
	}
}

// TestLinuxWrapDropsMissingRoots locks the never-widen rule in Wrap: a
// non-existent writable root is dropped from the protocol/bwrap argv.
func TestLinuxWrapDropsMissingRoots(t *testing.T) {
	missing := "/definitely/does/not/exist/for/this/test"
	w := newLinuxWrapper(linuxBackendProbes{bwrapUsable: func() bool { return true }})
	got, err := w.Wrap(bashCmd("/bin/bash", "-c", "true"), RootSet{WritableRoots: []string{missing}, NetworkEgress: true})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	for i := 0; i < len(got.Args)-1; i++ {
		if got.Args[i] == "--bind" && got.Args[i+1] == missing {
			t.Fatalf("missing root %q bound in %v", missing, got.Args)
		}
	}
}

// TestLinuxExecutableErrorFailsClosed locks the seam: when os.Executable
// errors on the landlock path, Wrap must error rather than fall back silently.
func TestLinuxExecutableErrorFailsClosed(t *testing.T) {
	w := newLinuxWrapper(linuxBackendProbes{
		landlockUsable: func() bool { return true },
		executable:     func() (string, error) { return "", errors.New("no exe") },
	})
	if _, err := w.Wrap(bashCmd("/bin/bash", "-c", "true"), RootSet{WritableRoots: []string{"/tmp"}, NetworkEgress: true}); err == nil {
		t.Fatal("executable resolution failure must error")
	}
}
