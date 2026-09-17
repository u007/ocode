package sandbox

import (
	"errors"
	"os/exec"
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
		// The confiner carries the original shell argv tail verbatim:
		// [exe, sandbox-confine, <shell>, [-l], -c, <command>].
		if len(got.Args) < 4 || got.Args[1] != confinerSubcommand || got.Args[len(got.Args)-2] != "-c" || got.Args[len(got.Args)-1] != "echo hi" {
			t.Fatalf("Args = %v, want [exe sandbox-confine <shell> -c echo hi]", got.Args)
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

	// The desktop login-shell invocation (SetLoginShell: <shell> -l -c) must
	// survive the Landlock re-exec with its argv tail intact — the confiner
	// extracts shell + flags + command positionally, so a 4-element argv that
	// used to be read as Args[2] would have handed "-c" to bash.
	t.Run("landlock-login-shell", func(t *testing.T) {
		w := newLinuxWrapper(linuxBackendProbes{
			landlockUsable: func() bool { return true },
			bwrapUsable:    func() bool { return true },
			executable:     func() (string, error) { return fakeExe, nil },
		})
		base := bashCmd("/bin/zsh", "-l", "-c", "echo hi")
		got, err := w.Wrap(base, RootSet{WritableRoots: []string{"/tmp"}, NetworkEgress: true})
		if err != nil {
			t.Fatalf("wrap error: %v", err)
		}
		want := []string{fakeExe, confinerSubcommand, "/bin/zsh", "-l", "-c", "echo hi"}
		if len(got.Args) != len(want) {
			t.Fatalf("Args = %v, want %v", got.Args, want)
		}
		for i := range want {
			if got.Args[i] != want[i] {
				t.Fatalf("Args = %v, want %v (mismatch at %d)", got.Args, want, i)
			}
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

// TestLinuxLandlockReexecResolvesBareShellPath is the regression for the Linux
// "total lockout": the production bash invocation is `exec.Command("bash",
// "-c", cmd)`, whose Args[0] is the bare name while Path is the PATH-resolved
// absolute path. The Landlock confiner execve's the shell directly (no PATH
// lookup), so forwarding Args[0] verbatim made every sandboxed command fail
// with `sandbox-confine: no such file or directory`. Wrap must hand the
// confiner an absolute shell path.
func TestLinuxLandlockReexecResolvesBareShellPath(t *testing.T) {
	w := newLinuxWrapper(linuxBackendProbes{
		landlockUsable: func() bool { return true },
		executable:     func() (string, error) { return "/fake/ocode", nil },
	})
	// Model exactly what exec.Command("bash", "-c", ...) produces on Linux:
	// resolved absolute Path, bare Args[0].
	base := &exec.Cmd{Path: "/usr/bin/bash", Args: []string{"bash", "-c", "echo hi"}}
	got, err := w.Wrap(base, RootSet{WritableRoots: []string{"/tmp"}, NetworkEgress: true})
	if err != nil {
		t.Fatalf("wrap error: %v", err)
	}
	if len(got.Args) < 5 || got.Args[1] != confinerSubcommand {
		t.Fatalf("Args = %v, want [exe sandbox-confine <shell> -c echo hi]", got.Args)
	}
	if shell := got.Args[2]; shell != "/usr/bin/bash" {
		t.Fatalf("confiner shell = %q, want absolute /usr/bin/bash (bare name would ENOENT)", shell)
	}
}

// TestLinuxConfineEntrypointResolvesBareShellPath locks the confiner's own
// defense-in-depth PATH resolution: even if a caller hands it a bare shell
// name, the confiner must not execve a CWD-relative "bash".
func TestLinuxConfineEntrypointResolvesBareShellPath(t *testing.T) {
	if got := resolveConfineShell("bash"); got == "" || got == "bash" {
		t.Fatalf("resolveConfineShell(%q) = %q, want a PATH-resolved path", "bash", got)
	}
	if got := resolveConfineShell("/usr/bin/bash"); got != "/usr/bin/bash" {
		t.Fatalf("resolveConfineShell absolute = %q, want unchanged", got)
	}
}
