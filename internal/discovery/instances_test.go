package discovery

import (
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

func TestAssignChatPortDeterministicBySortedID(t *testing.T) {
	ids := []string{"local/bonsai-8b-1bit", "local/aardvark-1b", "local/zeta-70b"}
	// Sorted: aardvark-1b(0), bonsai-8b-1bit(1), zeta-70b(2) -> ports 11458,11459,11460
	port, err := AssignChatPort("local/bonsai-8b-1bit", ids)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != chatPortRangeStart+1 {
		t.Fatalf("port = %d, want %d", port, chatPortRangeStart+1)
	}
}

func TestAssignChatPortUnknownModelErrors(t *testing.T) {
	ids := []string{"local/aardvark-1b"}
	if _, err := AssignChatPort("local/not-registered", ids); err == nil {
		t.Fatal("expected error for a modelID not present in registeredIDs")
	}
}

func TestAssignChatPortRejectsMoreThanRangeSize(t *testing.T) {
	ids := make([]string, chatPortRangeSize+1)
	for i := range ids {
		ids[i] = string(rune('a' + i))
	}
	if _, err := AssignChatPort(ids[chatPortRangeSize], ids); err == nil {
		t.Fatal("expected 'no free local-model port' error beyond the reserved range")
	}
}

func TestStartModelInstanceUnknownManifestErrors(t *testing.T) {
	spawn := func(string) error { return nil }
	err := StartModelInstance(spawn, "local/does-not-exist", 19999, 1, t.TempDir(), "")
	if err == nil {
		t.Fatal("expected error for a model id with no chat manifest")
	}
}

func TestGetModelInstanceUnknownReturnsFalse(t *testing.T) {
	if _, ok := GetModelInstance("local/never-started"); ok {
		t.Fatal("expected ok=false for a model that was never started")
	}
}

func TestStopModelInstanceNotRunningIsNoop(t *testing.T) {
	procs := tool.NewProcessRegistry()
	if err := StopModelInstance(procs, "local/never-started"); err != nil {
		t.Fatalf("stopping a never-started instance should be a no-op, got: %v", err)
	}
}

func TestChatBreakerBlocksAfterThreshold(t *testing.T) {
	modelID := "local/test-breaker-threshold"
	t.Cleanup(func() { chatBreakerReset(modelID) })

	if blocked, _, _ := chatBreakerBlocked(modelID); blocked {
		t.Fatal("fresh model should not be blocked")
	}
	chatBreakerRecordFailure(modelID)
	if blocked, _, _ := chatBreakerBlocked(modelID); blocked {
		t.Fatal("single failure should not trip the breaker (threshold=2)")
	}
	if !chatBreakerRecentFailure(modelID) {
		t.Fatal("a single recent failure should still shorten the retry poll budget")
	}
	chatBreakerRecordFailure(modelID)
	blocked, failures, _ := chatBreakerBlocked(modelID)
	if !blocked {
		t.Fatal("two consecutive failures should trip the breaker")
	}
	if failures != 2 {
		t.Fatalf("failures = %d, want 2", failures)
	}
}

func TestChatBreakerResetClearsFailures(t *testing.T) {
	modelID := "local/test-breaker-reset"
	t.Cleanup(func() { chatBreakerReset(modelID) })

	chatBreakerRecordFailure(modelID)
	chatBreakerRecordFailure(modelID)
	if blocked, _, _ := chatBreakerBlocked(modelID); !blocked {
		t.Fatal("expected breaker to be tripped before reset")
	}
	chatBreakerReset(modelID)
	if blocked, _, _ := chatBreakerBlocked(modelID); blocked {
		t.Fatal("expected breaker to clear after a successful health check")
	}
	if chatBreakerRecentFailure(modelID) {
		t.Fatal("expected no recent failure after reset")
	}
}

func TestChatBreakerCooldownExpires(t *testing.T) {
	modelID := "local/test-breaker-cooldown"
	t.Cleanup(func() { chatBreakerReset(modelID) })

	chatFailuresMu.Lock()
	chatFailures[modelID] = &chatFailureState{
		consecutiveFailures: chatBreakerThreshold,
		lastFailureAt:       time.Now().Add(-chatBreakerCooldown - time.Second),
	}
	chatFailuresMu.Unlock()

	if blocked, _, _ := chatBreakerBlocked(modelID); blocked {
		t.Fatal("expected breaker to no longer block once the cooldown window has passed")
	}
}

func TestFilterMLXArgsDropsUnsupportedFlag(t *testing.T) {
	argv := []string{"python3", "-m", "mlx_lm.server",
		"--model", "{repo}",
		"--host", "127.0.0.1",
		"--port", "{port}",
		"--decode-concurrency", "{parallel}"}
	// mlx_lm 0.30.5 (the PrismML fork pairing) lacks --decode-concurrency.
	supported := map[string]bool{"--model": true, "--host": true, "--port": true}
	out := filterMLXArgs(argv, "prism-ml/Bonsai-8B-mlx-1bit", 11459, 2, supported)
	for _, a := range out {
		if a == "--decode-concurrency" {
			t.Fatalf("unsupported flag --decode-concurrency was not dropped: %v", out)
		}
	}
	// The placeholder should have been expanded and the flag's value dropped
	// alongside it: python3 -m mlx_lm.server --model <repo> --host 127.0.0.1
	// --port <port> = 9 args.
	if got := len(out); got != 9 {
		t.Fatalf("filtered argv has %d args, want 9: %v", got, out)
	}
}

func TestFilterMLXArgsKeepsSupportedFlag(t *testing.T) {
	argv := []string{"python3", "-m", "mlx_lm.server",
		"--model", "{repo}",
		"--decode-concurrency", "{parallel}"}
	supported := map[string]bool{"--model": true, "--decode-concurrency": true}
	out := filterMLXArgs(argv, "prism-ml/Bonsai-8B-mlx-1bit", 11459, 2, supported)
	joined := strings.Join(out, " ")
	if !strings.Contains(joined, "--decode-concurrency") {
		t.Fatalf("supported flag was dropped: %v", out)
	}
	if !strings.Contains(joined, "2") {
		t.Fatalf("{parallel} not expanded to 2: %v", out)
	}
}

// TestFilterMLXArgsNilSupportedKeepsEverything guards the fail-open fix: a
// nil `supported` map (the flag probe failed — python3 missing, mlx_lm not
// installed, --help timed out) must keep every flag, including required ones
// like --model/--host/--port, rather than treating "probe failed" the same as
// "probed successfully and found zero supported flags" (which would silently
// produce a broken spawn command with no required arguments at all).
func TestFilterMLXArgsNilSupportedKeepsEverything(t *testing.T) {
	argv := []string{"python3", "-m", "mlx_lm.server",
		"--model", "{repo}",
		"--host", "127.0.0.1",
		"--port", "{port}",
		"--decode-concurrency", "{parallel}"}
	out := filterMLXArgs(argv, "prism-ml/Bonsai-8B-mlx-1bit", 11459, 2, nil)
	if len(out) != len(argv) {
		t.Fatalf("nil supported map dropped flags: got %v, want all %d args kept: %v", out, len(argv), argv)
	}
	joined := strings.Join(out, " ")
	for _, want := range []string{"--model", "--host", "--port", "--decode-concurrency"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("nil supported map should keep every flag; missing %s in %v", want, out)
		}
	}
}
