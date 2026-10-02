package cdp

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// The preload's relay dials HTR_SOCKET_PATH when it is set, and falls back to
// htrcli's own default (~/.htrcli/daemon.sock) when it is not. In shared mode
// ocode must NOT set it: the user's own browser extension cannot read a
// process environment, so it always uses htrcli's default, and pointing only
// ocode's embedded Chromium somewhere else is exactly the two-daemon split this
// branch removes.
//
// Every assertion below names the wrong input it catches, because a filter that
// is never called, or called with the wrong scope, passes a one-sided "the value
// is gone" check.

// TestFilterSharedLaunchEnvRemovesExactlyTwoNames pins the filter's SCOPE, not
// just its effect. The drops are asserted individually and the survivors are
// asserted individually, so neither of these wrong implementations passes:
// over-broad filtering on the "HTR_" prefix (which would also strip HTR_PORT /
// HTR_MANAGED_ID) and dropping the whole slice (which would strip everything).
func TestFilterSharedLaunchEnvRemovesExactlyTwoNames(t *testing.T) {
	in := []string{
		"HTR_SOCKET_PATH=/Users/me/.htrcli/daemon.sock",
		"HTR_NATIVE_HOST_NAME=com.ocode.htrcontrol",
		"HTR_PORT=3845",
		"HTR_MANAGED_ID=shared-token",
		"PATH=/usr/bin",
	}
	got := filterSharedLaunchEnv(in, "shared")

	joined := "\n" + strings.Join(got, "\n") + "\n"
	// Two exact lines must be gone. "HTR_SOCKET_PATH=" is asserted as a full
	// line (newline-delimited), not as a substring, so a value that merely
	// MENTIONS it cannot satisfy the assertion.
	for _, gone := range []string{"HTR_SOCKET_PATH=", "HTR_NATIVE_HOST_NAME="} {
		if strings.Contains(joined, "\n"+gone) || strings.HasPrefix(joined, gone) {
			t.Errorf("shared mode must not pass %s to Chrome; got:\n%s", gone, strings.Join(got, "\n"))
		}
	}
	// Everything else must survive. HTR_PORT and HTR_MANAGED_ID are the
	// canaries: they are HTR_* and carry real values, so an over-broad
	// "HTR_" prefix filter drops them.
	for _, keep := range []string{"HTR_PORT=3845", "HTR_MANAGED_ID=shared-token", "PATH=/usr/bin"} {
		if !strings.Contains(joined, keep) {
			t.Errorf("shared mode must pass unrelated vars through, including %s; got:\n%s", keep, strings.Join(got, "\n"))
		}
	}
	if len(got) != len(in)-2 {
		t.Errorf("shared mode must drop exactly 2 entries, dropped %d; got:\n%s", len(in)-len(got), strings.Join(got, "\n"))
	}
}

// TestFilterSharedLaunchEnvPreservesOrderAndDoesNotAlias guards two quieter
// wrong implementations: a filter that returns the input slice unchanged when
// nothing matches (aliasing the caller's slice, so a later append clobbers it)
// and one that reorders the environment.
func TestFilterSharedLaunchEnvPreservesOrderAndDoesNotAlias(t *testing.T) {
	in := []string{"A=1", "HTR_SOCKET_PATH=/x.sock", "B=2", "HTR_NATIVE_HOST_NAME=n", "C=3"}

	got := filterSharedLaunchEnv(in, "shared")
	if want := []string{"A=1", "B=2", "C=3"}; !slices.Equal(got, want) {
		t.Fatalf("shared mode = %v, want %v", got, want)
	}

	// Nothing matched: the result must be a fresh slice, not the caller's.
	noMatch := []string{"A=1", "B=2"}
	out := filterSharedLaunchEnv(noMatch, "shared")
	out[0] = "MUTATED"
	if noMatch[0] != "A=1" {
		t.Errorf("filter aliased its input slice; noMatch[0] = %q", noMatch[0])
	}
}

// TestFilterSharedLaunchEnvKeepsPrivateAndUnsetModes is the rollback guard.
// ManagerOptions.HTRSharedMode has three meaningful values: "shared",
// "private", and "" (a caller that never threaded it — today's behaviour).
// Both non-shared values must keep injecting, or `browser.htr_shared: false`
// silently becomes the shared path with an empty socket path.
func TestFilterSharedLaunchEnvKeepsPrivateAndUnsetModes(t *testing.T) {
	in := []string{"HTR_SOCKET_PATH=/x.sock", "HTR_NATIVE_HOST_NAME=com.ocode.htrcontrol"}
	for _, mode := range []string{"private", "", "  ", "Shared", "sharedx"} {
		got := filterSharedLaunchEnv(in, mode)
		if !slices.Equal(got, in) {
			t.Errorf("mode %q must keep the full env (private daemon + unset callers), got %v", mode, got)
		}
	}
}

// The two launch tests below are the ones that prove the filter is APPLIED.
// The unit tests above all pass if filterSharedLaunchEnv is defined and never
// called; these launch the test binary as a fake Chrome (see TestMain in
// launch_test.go) and read back the HTR_* environment it actually received.

const sharedSocket = "/Users/me/.htrcli/daemon.sock"

// TestSharedLaunchDoesNotInjectSocketIntoChrome is the behaviour the whole
// branch depends on. It catches: the filter never applied, applied with the
// mode hardcoded to "private", or applied to an env slice other than the one
// handed to the process.
func TestSharedLaunchDoesNotInjectSocketIntoChrome(t *testing.T) {
	got := launchAndReadChildHTREnv(t, htrLaunchConfig{
		ExtensionDir:   "",
		SocketPath:     sharedSocket,
		NativeHostName: "com.ocode.htrcontrol",
		SharedMode:     "shared",
	})
	joined := "\n" + strings.Join(got, "\n") + "\n"
	for _, gone := range []string{"HTR_SOCKET_PATH=", "HTR_NATIVE_HOST_NAME="} {
		if strings.Contains(joined, "\n"+gone) || strings.HasPrefix(joined, gone) {
			t.Errorf("shared launch must not put %s in Chrome's environment; got:\n%s", gone, strings.Join(got, "\n"))
		}
	}
}

// TestPrivateLaunchStillInjectsSocketIntoChrome is the other half. It catches a
// filter that over-fires (e.g. `mode == ""` treated as shared), which would
// break the documented rollback path `browser.htr_shared: false` — the daemon
// would come up on the ocode-managed socket while its preload relays to a
// different one.
func TestPrivateLaunchStillInjectsSocketIntoChrome(t *testing.T) {
	got := launchAndReadChildHTREnv(t, htrLaunchConfig{
		ExtensionDir:   "",
		SocketPath:     "/private/htr.sock",
		NativeHostName: "com.ocode.htrcontrol",
		SharedMode:     "private",
	})
	joined := "\n" + strings.Join(got, "\n") + "\n"
	if !strings.Contains(joined, "HTR_SOCKET_PATH=/private/htr.sock") {
		t.Errorf("private launch must still inject the ocode-managed socket; got:\n%s", strings.Join(got, "\n"))
	}
	if !strings.Contains(joined, "HTR_NATIVE_HOST_NAME=com.ocode.htrcontrol") {
		t.Errorf("private launch must still inject the native host name; got:\n%s", strings.Join(got, "\n"))
	}
}

// launchAndReadChildHTREnv launches the test binary as a fake Chrome with the
// given HTR launch config and returns the HTR_* entries from the environment
// the child process actually received.
func launchAndReadChildHTREnv(t *testing.T, htr htrLaunchConfig) []string {
	t.Helper()
	t.Setenv("OCODE_FAKE_CHROME", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	dump := filepath.Join(t.TempDir(), "child-env.txt")
	t.Setenv("OCODE_FAKE_CHROME_ENV_DUMP", dump)

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: time.Second})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sup.Shutdown(ctx)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, _, cleanup, err := launchChromeWithOptions(ctx, exe, sup, nil, htr, "", true)
	if err != nil {
		t.Fatalf("launchChromeWithOptions: %v", err)
	}
	t.Cleanup(cleanup)

	// The child writes the dump before its first CDP read, so it exists by the
	// time Wait has reaped the handshake; poll anyway so a slow machine cannot
	// read the file too early.
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, readErr := os.ReadFile(dump)
		if readErr == nil {
			return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		}
		if time.Now().After(deadline) {
			t.Fatalf("fake Chrome never wrote its environment dump to %s: %v", dump, readErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
