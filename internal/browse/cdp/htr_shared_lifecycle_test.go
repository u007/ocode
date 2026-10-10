package cdp

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// isolateHTROwnerState points ocode's global data dir at a temp directory so a
// test neither reads the developer's real owner marker nor writes lease files
// into it. paths.OcodeGlobalDataDir resolves the HOME-derived location on
// darwin/windows and XDG_DATA_HOME on linux, so both are set: setting only one
// leaves the other machine's real marker reachable and the assertions below
// silently test the developer's machine instead of the fixture.
func isolateHTROwnerState(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", home)
	return home
}

// withStubbedProbes replaces both liveness probes. A real port is neither
// available nor wanted here: the point of every test below is the branch the
// probe's answer selects, not the probe itself (htrHealthyForInstance has its
// own coverage against a real httptest server).
func withStubbedProbes(t *testing.T, own, foreign bool) {
	t.Helper()
	origOwn, origForeign := htrHealthyForInstanceFn, htrHealthyForeignFn
	htrHealthyForInstanceFn = func(port int, socket, identity string) bool { return own }
	htrHealthyForeignFn = func(port int, socket, token string) bool { return foreign }
	t.Cleanup(func() { htrHealthyForInstanceFn, htrHealthyForeignFn = origOwn, origForeign })
}

func TestOwnAliveReusesWithoutSpawning(t *testing.T) {
	isolateHTROwnerState(t)
	withStubbedProbes(t, true, false)

	st, err := EnsureHTRServe(nil, HTROptions{
		Enabled: true, Port: 3845,
		Shared: SharedDaemon{Mode: "shared", Port: 3845, Socket: "/tmp/x.sock", Token: "tok"},
	}, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !st.Running {
		t.Fatal("expected Running")
	}
	if st.StartedByOcode {
		t.Error("an adopted/alive daemon must not be reported as started by ocode")
	}
}

func TestForeignAliveIsAdoptedWithoutSpawnAndWithoutMarker(t *testing.T) {
	isolateHTROwnerState(t)
	withStubbedProbes(t, false, true)

	st, err := EnsureHTRServe(nil, HTROptions{
		Enabled: true, Port: 3845,
		Shared: SharedDaemon{Mode: "shared", Port: 3845, Socket: "/tmp/x.sock", Token: "tok"},
	}, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !st.Running || st.StartedByOcode {
		t.Fatalf("running=%v startedByOcode=%v, want true/false", st.Running, st.StartedByOcode)
	}
	// The owner marker is the authorisation terminateManagedHTR needs before it
	// kills a daemon. Asserting "no PID" instead would pass on a stale marker
	// left by a previous run while adoption went on to write one, so assert the
	// marker itself is absent.
	if _, err := readHTROwner(); err == nil {
		t.Error("adopting a foreign daemon must not write an ocode owner marker")
	}
}

func TestAdoptOnlyNeverSpawns(t *testing.T) {
	isolateHTROwnerState(t)
	withStubbedProbes(t, false, false)

	_, err := EnsureHTRServe(nil, HTROptions{
		Enabled: true, Port: 3845,
		Shared: SharedDaemon{Mode: "shared", AdoptOnly: true, Port: 3845, Socket: "/tmp/x.sock"},
	}, discardLogger())
	if err == nil {
		t.Fatal("AdoptOnly with no daemon must return an error rather than pretending")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "adopt") && !strings.Contains(lower, "htrcli serve") {
		t.Errorf("error %q must tell the user to start `htrcli serve` themselves", err)
	}
}

func TestSharedSpawnEnvOmitsBearerToken(t *testing.T) {
	env := sharedServeEnv("tok", 3845, "/tmp/x.sock", "com.ocode.htrcontrol")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "HTR_BEARER_TOKEN=") {
		t.Errorf("shared spawn must not pass HTR_BEARER_TOKEN; htrcli resolves its own:\n%s", joined)
	}
	if !strings.Contains(joined, "HTR_MANAGED_ID=tok") {
		t.Errorf("shared spawn must set HTR_MANAGED_ID to the shared token:\n%s", joined)
	}
	if !strings.Contains(joined, "HTR_PORT=3845") || !strings.Contains(joined, "HTR_SOCKET_PATH=/tmp/x.sock") {
		t.Errorf("shared spawn missing port/socket:\n%s", joined)
	}
	if strings.Contains(joined, "HTR_NATIVE_HOST_NAME=com.htrcontrol.host") {
		t.Error("must never point at the user's standalone host name")
	}
}

// TestPrivateSpawnEnvKeepsBearerToken guards the other side of the split above.
// sharedServeEnv and privateServeEnv share a construction, so dropping the
// private bearer line would leave the shared test green while the managed
// daemon came up with no token and every authenticated probe 401s.
func TestPrivateSpawnEnvKeepsBearerToken(t *testing.T) {
	env := privateServeEnv("priv-id", 3846, "/tmp/priv.sock", "com.ocode.htrcontrol")
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "HTR_BEARER_TOKEN=priv-id") {
		t.Errorf("the ocode-managed daemon must still receive its bearer token:\n%s", joined)
	}
	if !strings.Contains(joined, "HTR_MANAGED_ID=priv-id") {
		t.Errorf("private spawn must set HTR_MANAGED_ID to the identity:\n%s", joined)
	}
}

func TestDaemonOutputIsCapturedNotInherited(t *testing.T) {
	cmd := newSharedServeCmd("/nonexistent/htrcli", "tok", 3845, "/tmp/x.sock", "com.ocode.htrcontrol")
	if cmd.Stdout == nil || cmd.Stderr == nil {
		t.Fatal("daemon stdout/stderr must be captured, never inherited from the TUI")
	}
	for _, s := range []struct {
		name string
		io.Writer
	}{{"stdout", cmd.Stdout}, {"stderr", cmd.Stderr}} {
		if f, ok := s.Writer.(*os.File); ok {
			t.Fatalf("daemon %s must not be an *os.File (would paint the alt-screen); got %v", s.name, f)
		}
	}
}

// Catches: gating the foreign probe on !AdoptOnly. Adopt-only means "never
// spawn", not "never adopt": a config that cannot authorise a spawn (here, one
// resolved with a token but flagged adopt-only) must still attach to the daemon
// the user is already running, instead of telling them none exists.
func TestAdoptOnlyStillAdoptsForeignDaemon(t *testing.T) {
	isolateHTROwnerState(t)
	withStubbedProbes(t, false, true)

	st, err := EnsureHTRServe(nil, HTROptions{
		Enabled: true, Port: 3845,
		Shared: SharedDaemon{Mode: "shared", AdoptOnly: true, Port: 3845, Socket: "/tmp/x.sock", Token: "tok"},
	}, discardLogger())
	if err != nil {
		t.Fatalf("adopt-only must adopt a running foreign daemon, got: %v", err)
	}
	if !st.Running || st.StartedByOcode {
		t.Fatalf("running=%v startedByOcode=%v, want true/false", st.Running, st.StartedByOcode)
	}
	if _, err := readHTROwner(); err == nil {
		t.Error("adopting a foreign daemon must not write an ocode owner marker")
	}
}

// Catches: a marker-only status. Adoption writes no owner marker, so the
// settings snapshot has to find an adopted daemon through the shared token or
// it reports a daemon ocode is using as stopped.
func TestHTRDaemonStatusReportsAdoptedForeignDaemon(t *testing.T) {
	isolateHTROwnerState(t)
	withStubbedProbes(t, false, true)

	info := HTRDaemonStatus(3845, "/tmp/x.sock", "tok")
	if !info.Running {
		t.Fatalf("an adopted foreign daemon must report running: %+v", info)
	}
	if info.Managed {
		t.Errorf("a daemon with no owner marker must not be reported as managed: %+v", info)
	}
}

// Catches: a tabs query that refuses without a marker. The adopted daemon is
// authenticated with the shared token instead.
func TestListHTRTabsQueriesAdoptedDaemonWithSharedToken(t *testing.T) {
	isolateHTROwnerState(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tabs" || r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"data":[{"id":1,"url":"https://example.com","title":"Example"}]}`)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	tabs, err := ListHTRTabs(port, "tok")
	if err != nil {
		t.Fatalf("list tabs on an adopted daemon: %v", err)
	}
	if len(tabs) != 1 {
		t.Fatalf("tabs = %+v, want 1", tabs)
	}
}
