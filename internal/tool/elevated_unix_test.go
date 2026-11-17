//go:build !windows

package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeElevateScript writes a fake sudo/su named name into dir. It reads exactly
// one line from its tty (stdin on the pty), compares it to want, and on success
// prints a marker plus each argv verbatim so tests can assert the command string
// was not rewritten. On mismatch it prints a marker and exits 1.
func fakeElevateScript(t *testing.T, dir, name, want string) {
	t.Helper()
	body := fmt.Sprintf(`#!/bin/sh
IFS= read -r pw
if [ "$pw" = '%s' ]; then
  printf 'ELEV_OK\n'
  for a in "$@"; do printf 'ARG=[%%s]\n' "$a"; done
  exit 0
fi
printf 'ELEV_BAD\n'
exit 1
`, want)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}

func elevatePath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func elevateCmd(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	setProcGroup(cmd)
	return cmd
}

// (a) The secret is delivered once, drives the password check, and never shows
// up in captured stdout/stderr (echo is disabled AND output is scrubbed).
func TestElevatedPTYSecretNeverEchoed(t *testing.T) {
	const secret = "s3cr3t-passw0rd!"
	wantSecret := string(secret)
	dir := t.TempDir()
	fakeElevateScript(t, dir, "sudo", secret)
	elevatePath(t, dir)

	secretBuf := []byte(secret)
	out, err := executeElevated(context.Background(), elevateCmd(context.Background(), "sudo whoami"), secretBuf)
	if err != nil {
		t.Fatalf("executeElevated: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "ELEV_OK") {
		t.Fatalf("password check did not pass; out=%q", out)
	}
	if strings.Contains(out, wantSecret) {
		t.Fatalf("secret leaked into output: %q", out)
	}
	if strings.Count(out, "ELEV_OK") != 1 {
		t.Fatalf("password delivered more than once; out=%q", out)
	}
}

// (b) An incorrect secret surfaces an error, leaks nothing, and the transient
// secret buffer is zeroed.
func TestElevatedPTYWrongSecretClears(t *testing.T) {
	dir := t.TempDir()
	fakeElevateScript(t, dir, "sudo", "the-correct-password")
	elevatePath(t, dir)

	secret := []byte("definitely-wrong")
	wantSecret := string(secret)
	out, err := executeElevated(context.Background(), elevateCmd(context.Background(), "sudo whoami"), secret)
	if err == nil {
		t.Fatalf("expected an error for a wrong secret; out=%q", out)
	}
	if strings.Contains(out, wantSecret) {
		t.Fatalf("secret leaked into output: %q", out)
	}
	if strings.Contains(err.Error(), wantSecret) {
		t.Fatalf("secret leaked into error: %q", err)
	}
	for i, b := range secret {
		if b != 0 {
			t.Fatalf("secret buffer not zeroed at byte %d: %q", i, secret)
		}
	}
}

// (c) A timeout kills the whole process group (including the sleeping
// grandchild) and returns promptly; the transient secret buffer is zeroed.
func TestElevatedPTYTimeoutKillsGroupAndZeroes(t *testing.T) {
	const wantSecret = "timeout-secret"
	dir := t.TempDir()
	// Fake sudo reads the secret, then spawns a long-lived grandchild and waits.
	body := `#!/bin/sh
IFS= read -r pw
sleep 300 &
echo "CHILD_PID=$!"
echo "$pw" > /dev/null
sleep 300
`
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(body), 0o755); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}
	elevatePath(t, dir)

	secret := []byte(wantSecret)
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	start := time.Now()
	out, err := executeElevated(ctx, elevateCmd(ctx, "sudo whoami"), secret)
	if time.Since(start) > 5*time.Second {
		t.Fatalf("timeout did not return promptly: %v", time.Since(start))
	}
	if err == nil {
		t.Fatalf("expected a timeout error; out=%q", out)
	}
	for i, b := range secret {
		if b != 0 {
			t.Fatalf("secret buffer not zeroed at byte %d: %q", i, secret)
		}
	}
	// The grandchild must be dead: kill(pid, 0) fails with ESRCH.
	var childPID int
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "CHILD_PID=") {
			childPID, _ = strconv.Atoi(strings.TrimPrefix(line, "CHILD_PID="))
		}
	}
	if childPID == 0 {
		t.Fatalf("no CHILD_PID in output: %q", out)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(childPID, 0)
		if err == syscall.ESRCH {
			break
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(-childPID, syscall.SIGKILL)
			t.Fatalf("grandchild pid %d survived the group kill (kill check: %v)", childPID, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// (d) The original command string reaches the shell unchanged: no wrapper is
// prepended and argv is preserved verbatim.
func TestElevatedPTYCommandUnchanged(t *testing.T) {
	const wantSecret = "cmd-secret"
	dir := t.TempDir()
	fakeElevateScript(t, dir, "sudo", wantSecret)
	elevatePath(t, dir)

	secret := []byte(wantSecret)
	out, err := executeElevated(context.Background(), elevateCmd(context.Background(), "sudo -n -- echo hello"), secret)
	if err != nil {
		t.Fatalf("executeElevated: %v (out=%q)", err, out)
	}
	for _, want := range []string{"ARG=[-n]", "ARG=[--]", "ARG=[echo]", "ARG=[hello]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("command was rewritten; missing %q in %q", want, out)
		}
	}
}

// An empty/missing secret on Unix must fail closed — the command is never run
// unprivileged.
func TestElevatedPTYFailClosedEmptySecret(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "RAN")
	body := fmt.Sprintf("#!/bin/sh\nIFS= read -r pw\nprintf ran > %s\nprintf 'ELEV_OK\\n'\n", marker)
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(body), 0o755); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}
	elevatePath(t, dir)

	out, err := executeElevated(context.Background(), elevateCmd(context.Background(), "sudo whoami"), nil)
	if err == nil {
		t.Fatalf("expected fail-closed error; out=%q", out)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("command ran despite an empty elevation secret")
	}
}

// Integration: ExecuteStreamCtx routes an approved elevated call through the
// PTY path when the context carries the elevation getter.
func TestBashElevationIntegration(t *testing.T) {
	const secret = "integration-secret"
	dir := t.TempDir()
	fakeElevateScript(t, dir, "sudo", secret)
	elevatePath(t, dir)

	stored := []byte(secret)
	ctx := WithElevationFlag(WithElevationSecret(context.Background(), func() []byte {
		got := stored
		stored = nil
		return got
	}), true)
	args, _ := json.Marshal(map[string]string{"command": "sudo whoami"})
	res, err := (BashTool{}).ExecuteStreamCtx(ctx, args, nil)
	if err != nil {
		t.Fatalf("ExecuteStreamCtx: %v", err)
	}
	if !strings.Contains(res, "ELEV_OK") {
		t.Fatalf("elevated command did not run; res=%q", res)
	}
	if strings.Contains(res, secret) {
		t.Fatalf("secret leaked in integration result: %q", res)
	}
	if stored != nil {
		t.Fatal("elevation secret getter was not consumed")
	}
}

// The web/desktop path uses a process registry. The parent-death monitor
// wrapper must be skipped for elevated commands (it backgrounds the command
// with stdin=/dev/null), so the secret still reaches sudo via the PTY.
func TestBashElevationWithProcessRegistry(t *testing.T) {
	const wantSecret = "wrapped-secret"
	dir := t.TempDir()
	fakeElevateScript(t, dir, "sudo", wantSecret)
	elevatePath(t, dir)

	stored := []byte(wantSecret)
	ctx := WithElevationFlag(WithElevationSecret(context.Background(), func() []byte {
		got := stored
		stored = nil
		return got
	}), true)
	args, _ := json.Marshal(map[string]string{"command": "sudo whoami"})
	res, err := (BashTool{Procs: NewProcessRegistry()}).ExecuteStreamCtx(ctx, args, nil)
	if err != nil {
		t.Fatalf("ExecuteStreamCtx: %v", err)
	}
	if !strings.Contains(res, "ELEV_OK") {
		t.Fatalf("elevated command did not run through registry path; res=%q", res)
	}
	if strings.Contains(res, wantSecret) {
		t.Fatalf("secret leaked in registry result: %q", res)
	}
}

// Scrubbing must remove every full occurrence, including overlapping ones that
// a single ReplaceAll pass would leave as a partial remnant.
func TestScrubElevationSecretOverlapping(t *testing.T) {
	secret := []byte("abab")
	got := scrubElevationSecret("xxababababyyababzz", secret)
	if strings.Contains(got, "abab") {
		t.Fatalf("scrub left a full secret occurrence: %q", got)
	}
	if strings.Contains(got, "ababab") {
		t.Fatalf("scrub left an overlapping remnant: %q", got)
	}
	// A secret appearing twice is fully removed.
	if got := scrubElevationSecret("k-secret-k-secret-", []byte("secret")); strings.Contains(got, "secret") {
		t.Fatalf("scrub left a repeated occurrence: %q", got)
	}
	// Empty secret is a no-op.
	if got := scrubElevationSecret("abc", nil); got != "abc" {
		t.Fatalf("empty scrub changed input: %q", got)
	}
}

// An ordinary approval attaches the same getter, but it yields no secret. Such
// a call must take the unchanged normal path (no PTY, no fail-closed error).
func TestBashOrdinaryApprovalIgnoresEmptyGetterResult(t *testing.T) {
	ctx := WithElevationSecret(context.Background(), func() []byte { return nil })
	args, _ := json.Marshal(map[string]string{"command": "echo ordinary-ok"})
	res, err := (BashTool{}).ExecuteStreamCtx(ctx, args, nil)
	if err != nil {
		t.Fatalf("ExecuteStreamCtx: %v", err)
	}
	if !strings.Contains(res, "ordinary-ok") {
		t.Fatalf("ordinary command did not run on the normal path: %q", res)
	}
}

// A present getter that yields an empty (but non-nil) secret marks an
// elevation: on Unix it must fail closed through the integration path too.
func TestBashElevationEmptySecretFailsClosed(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "RAN")
	body := fmt.Sprintf("#!/bin/sh\nIFS= read -r pw\nprintf ran > %s\nprintf 'ELEV_OK\\n'\n", marker)
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(body), 0o755); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}
	elevatePath(t, dir)

	ctx := WithElevationFlag(WithElevationSecret(context.Background(), func() []byte { return []byte{} }), true)
	args, _ := json.Marshal(map[string]string{"command": "sudo whoami"})
	res, _ := (BashTool{}).ExecuteStreamCtx(ctx, args, nil)
	if !strings.Contains(res, "refusing to run the command unprivileged") {
		t.Fatalf("expected fail-closed error; res=%q", res)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("command ran despite an empty elevation secret")
	}
}

// BLOCKER regression: an approved leading sudo/su whose secret is missing (store
// reset, denied ask, wrong tool-call id, double Take) must fail closed. The flag
// is the elevation marker; a nil-yielding getter must NOT fall through to the
// ordinary path and run the command unprivileged.
func TestBashElevationMissingSecretFailsClosed(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "RAN")
	// Writes the marker as soon as it starts, regardless of stdin, so the old
	// fail-open behaviour is unmistakable.
	body := fmt.Sprintf("#!/bin/sh\nprintf ran > %s\nprintf 'ELEV_OK\\n'\n", marker)
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(body), 0o755); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}
	elevatePath(t, dir)

	ctx := WithElevationFlag(WithElevationSecret(context.Background(), func() []byte { return nil }), true)
	args, _ := json.Marshal(map[string]string{"command": "sudo whoami"})
	res, err := (BashTool{}).ExecuteStreamCtx(ctx, args, nil)
	if err != nil {
		t.Fatalf("ExecuteStreamCtx: %v", err)
	}
	if !strings.Contains(res, "refusing to run the command unprivileged") {
		t.Fatalf("expected a fail-closed error; res=%q", res)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("command ran unprivileged despite a missing elevation secret")
	}
}

// A wrong exit code from an elevated command must survive scrubbing: flattening
// the error to a plain fmt.Errorf makes commandExitCode report 1 for every
// elevated nonzero exit.
func TestBashElevationPreservesExitCode(t *testing.T) {
	const secret = "exitcode-secret"
	dir := t.TempDir()
	body := fmt.Sprintf(`#!/bin/sh
IFS= read -r pw
if [ "$pw" = '%s' ]; then
  printf 'ELEV_OK\n'
  exit 7
fi
printf 'ELEV_BAD\n'
exit 1
`, secret)
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(body), 0o755); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}
	elevatePath(t, dir)

	ctx := WithElevationFlag(WithElevationSecret(context.Background(), func() []byte { return []byte(secret) }), true)
	args, _ := json.Marshal(map[string]string{"command": "sudo whoami"})
	res, err := (BashTool{}).ExecuteStreamCtx(ctx, args, nil)
	if err != nil {
		t.Fatalf("ExecuteStreamCtx: %v", err)
	}
	if !strings.Contains(res, "exit code 7") {
		t.Fatalf("real exit code was lost (flattened to 1?): res=%q", res)
	}
	if strings.Contains(res, secret) {
		t.Fatalf("secret leaked in result: %q", res)
	}
}

// scrubElevationBuffer must remove the secret from the returned string and
// zero every byte of the caller's backing buffer (no residual unzeroed copies).
func TestScrubElevationBufferZeroesBytes(t *testing.T) {
	secret := []byte("s3cr3t")
	buf := []byte("before-s3cr3t-after")
	got := scrubElevationBuffer(buf, secret)
	if strings.Contains(got, "s3cr3t") {
		t.Fatalf("secret left in output: %q", got)
	}
	if got != "before--after" {
		t.Fatalf("unexpected scrubbed output: %q", got)
	}
	for i, b := range buf {
		if b != 0 {
			t.Fatalf("buffer byte %d not zeroed: %d", i, b)
		}
	}
}
