package server

import (
	"log"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/version"
)

// deadPID is larger than any real pid on every supported platform, so the
// liveness probe answers "gone" without spawning and reaping a child.
const deadPID = 1 << 30

func newServingAddr(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func writeTestInstance(t *testing.T, inst serveInstance) string {
	t.Helper()
	path, err := serveInstancePath(inst.PID)
	if err != nil {
		t.Fatalf("instance path: %v", err)
	}
	if err := writeServeInstance(path, inst); err != nil {
		t.Fatalf("write instance: %v", err)
	}
	return path
}

// The whole point of the registry: a server of another version is named, with
// the pid, so the user can stop it.
func TestServeSiblingWarningsNameDifferentVersion(t *testing.T) {
	lines := serveSiblingWarnings([]serveInstance{
		{PID: 2132, Version: "0.9.23", Addr: "127.0.0.1:4096", WorkDir: "/Users/james/www/ocode", Verified: true},
	}, version.Version)

	if len(lines) != 2 {
		t.Fatalf("got %d warning lines, want 2: %#v", len(lines), lines)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"v0.9.23", "pid 2132", "127.0.0.1:4096", "/Users/james/www/ocode", serveStopHint(runtime.GOOS, 2132)} {
		if !strings.Contains(joined, want) {
			t.Fatalf("warning %q missing %q", joined, want)
		}
	}
}

func TestServeRegistryStopHintPerOS(t *testing.T) {
	if got := serveStopHint("windows", 2132); got != "taskkill /PID 2132" {
		t.Fatalf("windows hint = %q, want taskkill /PID 2132", got)
	}
	if got := serveStopHint("darwin", 2132); got != "kill 2132" {
		t.Fatalf("darwin hint = %q, want kill 2132", got)
	}
}

// Same-version siblings are normal (desktop shell + a dev server) and must not
// produce noise on every start.
func TestServeSiblingWarningsSilentForSameVersion(t *testing.T) {
	lines := serveSiblingWarnings([]serveInstance{
		{PID: 2132, Version: version.Version, Addr: "127.0.0.1:4096"},
	}, version.Version)
	if len(lines) != 0 {
		t.Fatalf("same-version sibling warned: %#v", lines)
	}
}

func TestServeRegistryRoundTrip(t *testing.T) {
	setHomeTree(t, t.TempDir())
	addr, closeLn := newServingAddr(t)
	defer closeLn()

	cleanup := registerServeInstance(addr, "/tmp/project")
	instances := scanServeInstancesExcept(0)
	if len(instances) != 1 {
		t.Fatalf("scan found %d instances, want 1: %#v", len(instances), instances)
	}
	got := instances[0]
	if got.PID != os.Getpid() || got.Version != version.Version || got.Addr != addr {
		t.Fatalf("recorded %+v, want pid=%d version=%s addr=%s", got, os.Getpid(), version.Version, addr)
	}

	cleanup()
	if instances := scanServeInstancesExcept(0); len(instances) != 0 {
		t.Fatalf("cleanup left %#v", instances)
	}
}

// A server killed without running its cleanup leaves a record behind. The next
// start must drop it instead of warning about a ghost.
func TestServeRegistryDropsDeadPID(t *testing.T) {
	setHomeTree(t, t.TempDir())
	addr, closeLn := newServingAddr(t)
	defer closeLn()

	path := writeTestInstance(t, serveInstance{PID: deadPID, Version: "0.1.0", Addr: addr})
	if instances := scanServeInstancesExcept(0); len(instances) != 0 {
		t.Fatalf("dead pid reported as live: %#v", instances)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("dead record not removed (stat err = %v)", err)
	}
}

func TestServeRegistryDropsMalformedFile(t *testing.T) {
	setHomeTree(t, t.TempDir())
	path, err := serveInstancePath(deadPID)
	if err != nil {
		t.Fatalf("instance path: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write malformed: %v", err)
	}

	if instances := scanServeInstancesExcept(0); len(instances) != 0 {
		t.Fatalf("malformed record reported: %#v", instances)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("malformed record not removed (stat err = %v)", err)
	}
}

// A record this build cannot decode may still belong to a live server (a newer
// ocode). It must be kept, not deleted and not reported.
func TestServeRegistryKeepsUndecodableLiveRecord(t *testing.T) {
	setHomeTree(t, t.TempDir())
	path, err := serveInstancePath(os.Getpid())
	if err != nil {
		t.Fatalf("instance path: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"pid": "not-a-number"}`), 0o600); err != nil {
		t.Fatalf("write undecodable: %v", err)
	}

	if instances := scanServeInstancesExcept(0); len(instances) != 0 {
		t.Fatalf("undecodable record reported: %#v", instances)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("undecodable record of a live owner was deleted: %v", err)
	}
}

// A live pid with a listening address is a sibling and must be reported.
func TestServeRegistryReportsLiveRecord(t *testing.T) {
	setHomeTree(t, t.TempDir())
	addr, closeLn := newServingAddr(t)
	defer closeLn()

	writeTestInstance(t, serveInstance{PID: os.Getpid(), Version: "0.0.1", Addr: addr})
	instances := scanServeInstancesExcept(0)
	if len(instances) != 1 || instances[0].Addr != addr || instances[0].Version != "0.0.1" {
		t.Fatalf("live record not reported: %#v", instances)
	}
	// Written without the owner's lock, so it is reported but not verified: the
	// pid may have been recycled, and no stop command may be suggested.
	if instances[0].Verified {
		t.Fatalf("unlocked record reported as verified: %#v", instances[0])
	}
}

// PID reuse: a live pid whose recorded address is not listening proves nothing,
// so it must be ignored AND its file left alone (it may belong to another
// process's still-running server that has not bound yet).
func TestServeRegistryIgnoresRecycledPID(t *testing.T) {
	setHomeTree(t, t.TempDir())

	path := writeTestInstance(t, serveInstance{
		PID:     os.Getpid(),
		Version: "0.0.1",
		Addr:    "127.0.0.1:1", // nothing listens here
	})

	if instances := scanServeInstancesExcept(0); len(instances) != 0 {
		t.Fatalf("recycled pid reported: %#v", instances)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("recycled record was deleted: %v", err)
	}
}

func TestServeRegistrySkipsSelf(t *testing.T) {
	setHomeTree(t, t.TempDir())
	addr, closeLn := newServingAddr(t)
	defer closeLn()

	writeTestInstance(t, serveInstance{PID: os.Getpid(), Version: version.Version, Addr: addr, StartedAt: time.Now()})
	if instances := scanServeInstances(); len(instances) != 0 {
		t.Fatalf("self reported as a sibling: %#v", instances)
	}
}

// An unverified sibling is still reported (it may be a real pre-lock build) but
// must never suggest a stop command: its pid may belong to an unrelated process.
func TestServeSiblingWarningsOmitsStopHintWhenUnverified(t *testing.T) {
	lines := serveSiblingWarnings([]serveInstance{
		{PID: 2132, Version: "0.9.23", Addr: "127.0.0.1:4096"},
	}, version.Version)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "kill ") || strings.Contains(joined, "taskkill") {
		t.Fatalf("unverified sibling printed a stop command: %q", joined)
	}
	if !strings.Contains(joined, "pid 2132") {
		t.Fatalf("unverified sibling must still name the pid: %q", joined)
	}
}

// The owner's advisory lock is the identity proof. A record written by a live
// owner must be reported as verified even though nothing else distinguishes it.
func TestServeRegistryVerifiesLockedRecord(t *testing.T) {
	setHomeTree(t, t.TempDir())
	addr, closeLn := newServingAddr(t)
	defer closeLn()

	cleanup := registerServeInstance(addr, "/tmp/locked")
	defer cleanup()

	instances := scanServeInstancesExcept(0)
	if len(instances) != 1 {
		t.Fatalf("scan found %d instances, want 1: %#v", len(instances), instances)
	}
	if !instances[0].Verified {
		t.Fatalf("record whose owner holds the lock was not verified: %#v", instances[0])
	}
}

// A write killed between WriteFile and Rename leaves a .tmp the scan filter
// would otherwise never look at.
func TestServeRegistryPrunesStaleTmp(t *testing.T) {
	setHomeTree(t, t.TempDir())
	path, err := serveInstancePath(deadPID)
	if err != nil {
		t.Fatalf("instance path: %v", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}

	if instances := scanServeInstancesExcept(0); len(instances) != 0 {
		t.Fatalf("tmp reported as an instance: %#v", instances)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("stale tmp not pruned (stat err = %v)", err)
	}
}

// The wildcard branch is the one that runs in the default `ocode serve`
// configuration (the recorded address is 0.0.0.0:<port>), so it needs its own
// coverage: dialing that literal is not portable.
func TestServeAddrListeningTable(t *testing.T) {
	addr, closeLn := newServingAddr(t)
	defer closeLn()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}

	cases := []struct {
		name string
		addr string
		want bool
	}{
		{"loopback live", addr, true},
		{"wildcard v4 live", "0.0.0.0:" + port, true},
		{"wildcard v6 live", "[::]:" + port, true},
		{"portless", "127.0.0.1", false},
		{"zero port", "127.0.0.1:0", false},
		{"empty", "", false},
		{"dead port", "127.0.0.1:1", false},
	}
	for _, c := range cases {
		if got := serveAddrListening(c.addr); got != c.want {
			t.Errorf("%s: serveAddrListening(%q) = %v, want %v", c.name, c.addr, got, c.want)
		}
	}
}

// warnServeSiblings is the sole production entry point; prove the log path is
// wired, not just the pure renderer.
func TestWarnServeSiblingsLogsDifferentVersion(t *testing.T) {
	setHomeTree(t, t.TempDir())
	addr, closeLn := newServingAddr(t)
	defer closeLn()

	// A live process other than this one: the test binary's parent.
	siblingPID := os.Getppid()
	writeTestInstance(t, serveInstance{PID: siblingPID, Version: "0.0.1", Addr: addr, WorkDir: "/tmp/sibling"})

	var buf strings.Builder
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	})

	warnServeSiblings()

	got := buf.String()
	for _, want := range []string{"v0.0.1", "pid " + strconv.Itoa(siblingPID), addr, "/tmp/sibling"} {
		if !strings.Contains(got, want) {
			t.Fatalf("warning %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "kill ") {
		t.Fatalf("unverified sibling printed a stop command: %q", got)
	}
}
