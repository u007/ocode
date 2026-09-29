package remote

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// withShrunkServeStatePoll bounds StartFreshServer's wait for the remote state
// file. With a fakeTransport it always times out, and the production default
// (40 x 250ms) would add 10s to the test for nothing.
func withShrunkServeStatePoll(t *testing.T) {
	t.Helper()
	origAttempts, origInterval := serveStatePollAttempts, serveStatePollInterval
	serveStatePollAttempts, serveStatePollInterval = 2, time.Millisecond
	t.Cleanup(func() {
		serveStatePollAttempts, serveStatePollInterval = origAttempts, origInterval
	})
}

// withFakeTransportTarget points newTransportForTarget at a fakeTransport for
// the duration of the test, so runPrepareStages runs with no real ssh process.
func withFakeTransportTarget(t *testing.T, f *fakeTransport) {
	t.Helper()
	prev := newTransportForTarget
	newTransportForTarget = func(Target, *tool.ProcessSupervisor) (Transport, error) {
		return f, nil
	}
	t.Cleanup(func() { newTransportForTarget = prev })
}

// OnEstablished is the boundary remotecli uses to decide whether a finished
// connect may leave a project entry behind. If it fired before the host was
// actually proven usable, a typo'd or offline host would be persisted anyway —
// the exact regression that put a dead `nosuchhost.invalid` entry in a real
// developer's project list. These tests pin the "never fire" direction, which
// is the one that causes damage.
func TestConnectDoesNotFireOnEstablishedWhenHostUnreachable(t *testing.T) {
	f := newFakeTransport()
	f.execErrs["true"] = errors.New("ssh: could not resolve hostname")
	withFakeTransportTarget(t, f)

	for _, tc := range []struct {
		name string
		call func(ConnectOptions) error
	}{
		{"Connect", Connect},
		{"ConnectWeb", ConnectWeb},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fired := false
			err := tc.call(ConnectOptions{
				Target:        Target{Kind: KindSSH, Host: "nosuchhost.invalid"},
				Path:          "~",
				Out:           &bytes.Buffer{},
				OnEstablished: func() { fired = true },
			})
			if err == nil {
				t.Fatal("expected the unreachable-host error")
			}
			if fired {
				t.Error("OnEstablished fired for a host that was never reachable")
			}
		})
	}
}

// The hook must sit at the END of the prepare chain, not just after the
// "reachable" probe. Here the host answers, but the very next stage fails, so
// ocode was never ensured on it and there is still nothing worth persisting.
func TestConnectDoesNotFireOnEstablishedWhenPlatformDetectFails(t *testing.T) {
	f := newFakeTransport()
	// "true" (the reachable probe) succeeds with the fake's default result;
	// uname -sm is what DetectPlatform runs, and it fails.
	f.execErrs["uname -sm"] = errors.New("remote command timed out after 30s")
	withFakeTransportTarget(t, f)

	for _, tc := range []struct {
		name string
		call func(ConnectOptions) error
	}{
		{"Connect", Connect},
		{"ConnectWeb", ConnectWeb},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fired := false
			err := tc.call(ConnectOptions{
				Target:        Target{Kind: KindSSH, Host: "h"},
				Path:          "~",
				Out:           &bytes.Buffer{},
				OnEstablished: func() { fired = true },
			})
			if err == nil {
				t.Fatal("expected the platform-detect error")
			}
			if fired {
				t.Error("OnEstablished fired before the prepare chain completed")
			}
		})
	}
}

// The positive direction. Without this, deleting BOTH OnEstablished call sites
// would leave every other test in this file green — they only assert the hook
// does NOT fire, which a hook that never fires trivially satisfies. A connect
// that never reports establishment means remotecli silently stops persisting
// every remote project, so the fire path needs its own pin.
//
// The prepare chain is satisfied purely through the fakeTransport: the
// "reachable" probe and the binary-presence test take the fake's default
// (exit 0), `uname -sm` returns a real platform, and NoSync skips credential
// sync. No ssh process is ever spawned.
func TestConnectFiresOnEstablishedAfterPrepareChainSucceeds(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(ConnectOptions) error
	}{
		{"Connect", Connect},
		{"ConnectWeb", ConnectWeb},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeTransport()
			f.execResults["uname -sm"] = ExecResult{Stdout: "Linux x86_64", ExitCode: 0}
			withFakeTransportTarget(t, f)
			withShrunkServeStatePoll(t)

			fired := 0
			// Whatever happens AFTER prepare (multiplex detect, remote
			// server start) is irrelevant here: the assertion is that the
			// hook fired at the prepare boundary.
			_ = tc.call(ConnectOptions{
				Target:        Target{Kind: KindSSH, Host: "h"},
				Path:          "~",
				NoSync:        true,
				Out:           &bytes.Buffer{},
				OnEstablished: func() { fired++ },
			})
			if fired != 1 {
				t.Errorf("OnEstablished fired %d times, want exactly 1", fired)
			}
		})
	}
}
