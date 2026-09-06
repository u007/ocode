package remote

import (
	"testing"
)

func TestWSLTransportDescribe(t *testing.T) {
	if got := NewWSLTransport("Ubuntu", nil).Describe(); got != "wsl Ubuntu" {
		t.Errorf("got %q, want %q", got, "wsl Ubuntu")
	}
	if got := NewWSLTransport("", nil).Describe(); got != "wsl (default distro)" {
		t.Errorf("got %q, want %q", got, "wsl (default distro)")
	}
}

func TestWSLTransportExecCommandConstruction(t *testing.T) {
	// This test only verifies the *exec.Cmd shape (Path/Args), never
	// actually running wsl.exe — the package has no Windows CI (see
	// 04-phase3-wsl.md's Testing section), so exec.Command's argv is
	// asserted directly via wslExecArgs, the pure helper factored out for
	// exactly this reason.
	got := wslExecArgs("Ubuntu", "echo hi")
	want := []string{"-d", "Ubuntu", "--", "sh", "-c", "echo hi"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	gotDefault := wslExecArgs("", "echo hi")
	wantDefault := []string{"--", "sh", "-c", "echo hi"}
	if !equalStrings(gotDefault, wantDefault) {
		t.Errorf("got %v, want %v", gotDefault, wantDefault)
	}
}

func TestWSLTransportInteractiveArgs(t *testing.T) {
	got := wslInteractiveArgs("Ubuntu", "ocode ~")
	want := []string{"-d", "Ubuntu", "--", "ocode", "~"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWSLTransportCopyStreamsViaStdin(t *testing.T) {
	// Copy shells out for real (there's no fake exec.Cmd runner in this
	// package), so this test only runs where a "cat"-like receiver is
	// available to observe stdin — skip on Windows where wsl.exe itself
	// would need to exist. Use the transport's run() indirection isn't
	// exposed for interception, so instead assert the constructed command
	// via the same pure-args pattern as Exec/ExecInteractive.
	got := wslCopyArgs("Ubuntu", "~/.ocode/bin/0.1.0/.ocode.partial")
	want := []string{"-d", "Ubuntu", "--", "sh", "-c", "cat > " + shellQuotePath("~/.ocode/bin/0.1.0/.ocode.partial")}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
