package remote

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The config-preservation refusal is the one sync failure a user can fix, and
// `Connect` treats ANY sync error as fatal, so the message must name the host
// and the remedy instead of leaving a permanent 502 with no explanation.
func TestSyncFailureHintNamesTheRemedy(t *testing.T) {
	base := errors.New(`refusing to overwrite /home/x/.config/opencode/ocodeconfig.json: read destination /home/x/.config/opencode/ocodeconfig.json: permission denied`)

	got := syncFailureHint("dev@box", base)
	if got == nil {
		t.Fatal("expected a non-nil error")
	}
	// The original error must survive so callers can still inspect the cause.
	if !errors.Is(got, base) {
		t.Fatalf("hint dropped the original error: %v", got)
	}
	for _, want := range []string{"dev@box", "chmod", "delete", "extra_allowed_paths"} {
		if !strings.Contains(got.Error(), want) {
			t.Fatalf("hint should mention %q:\n%s", want, got)
		}
	}
}

func TestSyncFailureHintLeavesOtherErrorsAlone(t *testing.T) {
	base := errors.New("exit 1")
	if got := syncFailureHint("dev@box", base); got != base {
		t.Fatalf("an unrelated failure must pass through unchanged, got %v", got)
	}
	if got := syncFailureHint("dev@box", nil); got != nil {
		t.Fatalf("nil error must stay nil, got %v", got)
	}
}

// runSyncStage used to return a bare "exit N" and discard the remote's stderr,
// which is where the actionable reason lives — so the hint could never fire.
func TestRunSyncStageSurfacesRemoteStderr(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "ocodeconfig.json"), []byte(`{"models":{"a":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	origCfgDir := globalConfigDirFn
	globalConfigDirFn = func() (string, error) { return cfgDir, nil }
	t.Cleanup(func() { globalConfigDirFn = origCfgDir })

	tr := newFakeTransport()
	tr.execDefault = ExecResult{
		ExitCode: 1,
		Stderr:   "refusing to overwrite /home/x/.config/opencode/ocodeconfig.json: permission denied\n",
	}

	err := runSyncStage(NewProgress(io.Discard, "sync"), tr, "dev@box", "0.0.0-test", false)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("remote stderr was discarded from the error: %v", err)
	}
	if !strings.Contains(err.Error(), "exit 1") {
		t.Fatalf("should still report the exit code: %v", err)
	}
	// And the hint must then reach the user through Connect's wrapper.
	hinted := syncFailureHint("dev@box", err)
	if !strings.Contains(hinted.Error(), "chmod") {
		t.Fatalf("the refusal stderr should trigger the actionable hint: %v", hinted)
	}
}
