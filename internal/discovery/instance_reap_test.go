package discovery

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	gopsprocess "github.com/shirou/gopsutil/v4/process"
)

const strayHelperEnv = "OCODE_STRAY_HELPER_PORT"
const strayHelperHealthyEnv = "OCODE_STRAY_HELPER_HEALTHY"
const strayHelperToken = "prism-ml/Test-Stray-mlx-1bit"

// TestStrayHelperProcess is not a real test: it is the child process used by
// TestReapStrayChatServerReclaimsPort. Re-executing the test binary is the
// standard way to get a process with a command line we control — the reaper
// identifies servers by command line, so a real child is the only honest way
// to exercise it. It binds the port and then goes silent, exactly like the
// wedged server it stands in for.
func TestStrayHelperProcess(t *testing.T) {
	port := os.Getenv(strayHelperEnv)
	if port == "" {
		t.Skip("helper process only; run via TestReapStrayChatServerReclaimsPort")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stray helper could not bind port %s: %v\n", port, err)
		os.Exit(1)
	}
	defer ln.Close()

	// OCODE_STRAY_HELPER_HEALTHY makes the helper stand in for a WORKING
	// server instead of a wedged one, so the reaper can be shown leaving it
	// alone. Without it the helper accepts nothing and answers nothing.
	if os.Getenv(strayHelperHealthyEnv) == "1" {
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"object":"list","data":[{"id":%q,"object":"model"}]}`, strayHelperToken)
		})}
		go srv.Serve(ln)
	}

	// Never outlive the parent test by much: on the happy path the parent
	// kills this within a second, so this is only a runaway backstop.
	time.Sleep(20 * time.Second)
}

// freePort returns a port that was bindable a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func TestChatPortHeldTracksRealBinding(t *testing.T) {
	port := freePort(t)
	if chatPortHeld(port) {
		t.Fatalf("port %d should be free", port)
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("bind %d: %v", port, err)
	}
	if !chatPortHeld(port) {
		t.Fatalf("port %d should read as held while bound", port)
	}
	ln.Close()
	if chatPortHeld(port) {
		t.Fatalf("port %d should read as free after close", port)
	}
}

func TestReapStrayChatServerNoOpOnFreePort(t *testing.T) {
	man := ServerManifest{Backend: BackendMLX, MLXRepo: strayHelperToken}
	reaped, err := reapStrayChatServer("local/test-stray", freePort(t), man)
	if err != nil {
		t.Fatalf("free port must not error: %v", err)
	}
	if reaped {
		t.Fatal("nothing to reap on a free port")
	}
}

// A port held by something that is NOT this model's server must be reported,
// never killed — the reaper only ever terminates a process it can positively
// identify as the server for this model on this port.
func TestReapStrayChatServerRefusesUnknownHolder(t *testing.T) {
	port := freePort(t)
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("bind %d: %v", port, err)
	}
	defer ln.Close()

	man := ServerManifest{Backend: BackendMLX, MLXRepo: strayHelperToken}
	reaped, err := reapStrayChatServer("local/test-stray", port, man)
	if reaped {
		t.Fatal("must not claim to have reaped an unidentified holder")
	}
	if err == nil {
		t.Fatal("expected an error naming the occupied port")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("port %d", port)) {
		t.Fatalf("error should name the port, got %v", err)
	}
}

func TestReapStrayChatServerReclaimsPort(t *testing.T) {
	port := freePort(t)

	helper := startStrayHelper(t, port, false)

	man := ServerManifest{Backend: BackendMLX, MLXRepo: strayHelperToken}
	if pid, _, found := findStrayChatServer(man, port); !found {
		t.Fatalf("reaper could not identify the stray helper on port %d", port)
	} else if pid != helper.Process.Pid {
		t.Fatalf("identified pid %d, want the helper's %d", pid, helper.Process.Pid)
	}

	reaped, err := reapStrayChatServer("local/test-stray", port, man)
	if err != nil {
		t.Fatalf("reap failed: %v", err)
	}
	if !reaped {
		t.Fatal("expected the stray helper to be reaped")
	}
	if chatPortHeld(port) {
		t.Fatalf("port %d still held after reap", port)
	}
}

// startStrayHelper launches the child server stand-in on port and waits until
// it has the port bound. Its command line carries both signals the reaper
// matches on — the served model id and the exact --port flag — after the first
// positional argument because the test binary's own flag parser stops there
// and leaves the remaining arguments available for process inspection.
func startStrayHelper(t *testing.T, port int, healthy bool) *exec.Cmd {
	return startStrayHelperOn(t, port, port, healthy)
}

func startStrayHelperOn(t *testing.T, bindPort, advertisedPort int, healthy bool) *exec.Cmd {
	t.Helper()
	helper := exec.Command(os.Args[0],
		"-test.run=TestStrayHelperProcess",
		strayHelperToken, "--port", strconv.Itoa(advertisedPort))
	helper.Stderr = os.Stderr
	helper.Env = append(os.Environ(), fmt.Sprintf("%s=%d", strayHelperEnv, bindPort))
	if healthy {
		helper.Env = append(helper.Env, strayHelperHealthyEnv+"=1")
	}
	if err := helper.Start(); err != nil {
		t.Fatalf("start stray helper: %v", err)
	}
	t.Cleanup(func() {
		_ = helper.Process.Kill()
		_, _ = helper.Process.Wait()
	})

	deadline := time.Now().Add(30 * time.Second)
	for !chatPortHeld(bindPort) {
		if time.Now().After(deadline) {
			t.Fatalf("stray helper never bound port %d", bindPort)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return helper
}

func TestStrayServerArgsMatchRequiresExactPort(t *testing.T) {
	args := []string{"mlx_lm.server", "--model", strayHelperToken, "--port", "114580"}
	if strayServerArgsMatch(args, strayHelperToken, 11458) {
		t.Fatal("a longer port value must not match the requested port")
	}
	if !strayServerArgsMatch([]string{"mlx_lm.server", "--model", strayHelperToken, "--port", "11458"}, strayHelperToken, 11458) {
		t.Fatal("the exact model and port should match")
	}
}

func TestReapStrayChatServerRefusesMatchingNonOwner(t *testing.T) {
	targetPort := freePort(t)
	otherPort := freePort(t)
	// The target port is held, but the process whose command line looks like
	// the target server owns only another port. It must not be signalled.
	targetListener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", targetPort))
	if err != nil {
		t.Fatalf("bind target port %d: %v", targetPort, err)
	}
	defer targetListener.Close()
	helper := startStrayHelperOn(t, otherPort, targetPort, false)

	man := ServerManifest{Backend: BackendMLX, MLXRepo: strayHelperToken}
	reaped, err := reapStrayChatServer("local/test-stray", targetPort, man)
	if reaped {
		t.Fatal("must not reap a command-line match that does not own the target listener")
	}
	if err == nil {
		t.Fatal("expected an error naming the unidentified target-port holder")
	}
	if err := helper.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("non-owner candidate was signalled or exited: %v", err)
	}
}

// The reaper SIGKILLs whatever it identifies, so the case that matters most is
// the one where it must hold fire: a server that looks exactly like a stray by
// command line but is actually serving. Only the health re-probe separates them.
func TestReapStrayChatServerSparesHealthyServer(t *testing.T) {
	port := freePort(t)
	helper := startStrayHelper(t, port, true)
	man := ServerManifest{Backend: BackendMLX, MLXRepo: strayHelperToken, HealthPath: "/v1/models"}

	// Sanity: the command-line matcher does consider it a candidate, so a pass
	// here proves the re-probe is what spared it.
	if _, _, found := findStrayChatServer(man, port); !found {
		t.Fatal("healthy helper should still match the command-line identification")
	}

	reaped, err := reapStrayChatServer("local/test-stray", port, man)
	if err != nil {
		t.Fatalf("a healthy server must not produce an error: %v", err)
	}
	if reaped {
		t.Fatal("a healthy server must never be reaped")
	}
	if !chatPortHeld(port) {
		t.Fatal("healthy server was killed")
	}
	if alive, _ := gopsprocess.PidExists(int32(helper.Process.Pid)); !alive {
		t.Fatal("healthy server process was killed")
	}
}

// A start lock left behind by a process that died is what sends every later
// start down the wait-for-someone-else path instead of reaping the stray that
// same dead process orphaned. A dead owner must therefore lose the lock
// immediately, without waiting out chatStartLockStaleAfter.
func TestAcquireChatStartLockReclaimsFromDeadOwner(t *testing.T) {
	cacheDir := t.TempDir()
	modelID := "local/test-stray"

	// A pid that has already exited: start a child and reap it.
	dead := exec.Command(os.Args[0], "-test.run=TestStrayHelperProcess")
	dead.Env = append(os.Environ(), strayHelperEnv+"=") // no port set -> skips immediately
	if err := dead.Run(); err != nil {
		t.Fatalf("run throwaway process: %v", err)
	}
	deadPID := dead.Process.Pid

	lockPath := filepath.Join(cacheDir, "locks", strings.ReplaceAll(modelID, "/", "_")+".start.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatalf("mkdir locks: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte(fmt.Sprintf("%d", deadPID)), 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	acquired, release, err := acquireChatStartLock(cacheDir, modelID)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !acquired {
		t.Fatal("a lock whose owner is dead must be reclaimed immediately, not waited out")
	}
	defer release()

	// And the reclaimed lock records this process as the new owner.
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read reclaimed lock: %v", err)
	}
	if strings.TrimSpace(string(data)) != fmt.Sprintf("%d", os.Getpid()) {
		t.Fatalf("lock should record the new owner pid %d, got %q", os.Getpid(), data)
	}
}

// A live owner keeps the lock: the contender must wait rather than stomp a
// genuine in-flight spawn.
func TestAcquireChatStartLockRespectsLiveOwner(t *testing.T) {
	cacheDir := t.TempDir()
	modelID := "local/test-stray"

	lockPath := filepath.Join(cacheDir, "locks", strings.ReplaceAll(modelID, "/", "_")+".start.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatalf("mkdir locks: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	acquired, _, err := acquireChatStartLock(cacheDir, modelID)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if acquired {
		t.Fatal("a lock held by a live process must not be reclaimed")
	}
}
