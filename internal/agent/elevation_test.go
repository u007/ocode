package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// TestElevationFailureCapCountsAndResets pins the 3-strike re-ask cap state
// machine: three consecutive failed elevation executions exhaust the chain;
// a non-elevation command or a fresh human turn resets it; an ask sentinel is
// not an execution and must not move the counter.
func TestElevationFailureCapCountsAndResets(t *testing.T) {
	a := newTestAgent(nil, []tool.Tool{tool.BashTool{}}, nil, nil)
	elevArgs := json.RawMessage(`{"command":"sudo whoami"}`)

	for i := 1; i <= 3; i++ {
		a.trackElevationOutcome("bash", elevArgs, "Command failed (exit code 1). Output: wrong password")
		if got := a.elevationFailureCount(); got != i {
			t.Fatalf("after %d failures count = %d, want %d", i, got, i)
		}
		if i < 3 && a.ElevationAttemptsExhausted() {
			t.Fatalf("cap reached early at %d failures", i)
		}
	}
	if !a.ElevationAttemptsExhausted() {
		t.Fatal("3 consecutive failures must exhaust the cap")
	}

	// An ask sentinel is not an execution: it must not consume or reset a strike.
	a.ResetElevationFailures()
	a.trackElevationOutcome("bash", elevArgs, tool.SentinelPermissionAsk+`{}`)
	if got := a.elevationFailureCount(); got != 0 {
		t.Fatalf("ask sentinel changed the counter: %d", got)
	}

	// A non-elevation command resets the chain.
	a.NoteElevationFailure()
	a.NoteElevationFailure()
	a.trackElevationOutcome("bash", json.RawMessage(`{"command":"echo hello"}`), "hello")
	if a.ElevationAttemptsExhausted() || a.elevationFailureCount() != 0 {
		t.Fatalf("non-elevation command did not reset: count=%d", a.elevationFailureCount())
	}
}

// TestElevationCapResetsOnNewUserTurn pins the other reset trigger: a fresh
// human message clears the chain, while a synthetic [ocode:...] resume does not.
func TestElevationCapResetsOnNewUserTurn(t *testing.T) {
	a := newTestAgent(nil, []tool.Tool{tool.BashTool{}}, nil, nil)
	for i := 0; i < 3; i++ {
		a.NoteElevationFailure()
	}
	if !a.ElevationAttemptsExhausted() {
		t.Fatal("setup: cap not exhausted")
	}
	_, _ = a.Step([]Message{{Role: "user", Content: "a fresh request"}})
	if a.ElevationAttemptsExhausted() {
		t.Fatal("new human turn must reset the elevation failure chain")
	}
	for i := 0; i < 3; i++ {
		a.NoteElevationFailure()
	}
	_, _ = a.Step([]Message{{Role: "user", Content: "[ocode:goal] keep going"}})
	if !a.ElevationAttemptsExhausted() {
		t.Fatal("synthetic [ocode:] resume must NOT reset the chain")
	}
}

// TestElevationCapDeniesAskAfterThreeFailures proves fail-closed behaviour at
// the prompt boundary: after three failures, no permission ask is raised at all
// and the caller gets a clear denial instead.
func TestElevationCapDeniesAskAfterThreeFailures(t *testing.T) {
	a := newTestAgent(&elevationE2EClient{}, []tool.Tool{tool.BashTool{}}, nil, nil)
	for i := 0; i < 3; i++ {
		a.NoteElevationFailure()
	}
	res, err := a.handleToolCall("bash", json.RawMessage(`{"command":"sudo whoami"}`), nil, "call-cap")
	if err != nil {
		t.Fatalf("handleToolCall: %v", err)
	}
	if strings.HasPrefix(res, tool.SentinelPermissionAsk) {
		t.Fatalf("elevation ask must not be raised after 3 failures: %q", res)
	}
	if !strings.Contains(res, "3 consecutive") {
		t.Fatalf("denial must explain the cap: %q", res)
	}
}

func TestElevationManagerPerRequestIsolation(t *testing.T) {
	m := NewElevationManager()
	m.Store("req-a", []byte("secret-a"))
	m.Store("req-b", []byte("secret-b"))

	gotA := m.Take("req-a")
	if !bytes.Equal(gotA, []byte("secret-a")) {
		t.Fatalf("Take(req-a) = %q, want %q", gotA, "secret-a")
	}
	// Taking one request must not disturb the other.
	gotB := m.Take("req-b")
	if !bytes.Equal(gotB, []byte("secret-b")) {
		t.Fatalf("Take(req-b) after taking req-a = %q, want %q", gotB, "secret-b")
	}
}

func TestElevationManagerTakeZeroes(t *testing.T) {
	m := NewElevationManager()
	m.Store("req", []byte("hunter2"))
	// Reach the stored backing array directly: Take must return a copy to the
	// caller and zero the stored bytes so no lingering reference keeps them.
	stored := m.secrets["req"]

	got := m.Take("req")
	if !bytes.Equal(got, []byte("hunter2")) {
		t.Fatalf("Take returned %q, want %q", got, "hunter2")
	}
	for i, b := range stored {
		if b != 0 {
			t.Fatalf("stored backing array not zeroed at index %d: %d", i, b)
		}
	}
	if _, ok := m.secrets["req"]; ok {
		t.Fatal("entry still present after Take")
	}
	if again := m.Take("req"); again != nil {
		t.Fatalf("second Take = %q, want nil", again)
	}
}

func TestElevationManagerStoreCopiesInput(t *testing.T) {
	m := NewElevationManager()
	input := []byte("original")
	m.Store("req", input)
	input[0] = 'X' // mutate the caller's slice after Store
	got := m.Take("req")
	if !bytes.Equal(got, []byte("original")) {
		t.Fatalf("stored secret tracked caller mutation: got %q", got)
	}
}

func TestElevationManagerClearZeroes(t *testing.T) {
	m := NewElevationManager()
	m.Store("req-a", []byte("aaa"))
	m.Store("req-b", []byte("bbb"))
	storedA := m.secrets["req-a"]

	m.Clear("req-a")
	for i, b := range storedA {
		if b != 0 {
			t.Fatalf("Clear did not zero entry at index %d: %d", i, b)
		}
	}
	if m.Take("req-a") != nil {
		t.Fatal("cleared entry still returned by Take")
	}
	if got := m.Take("req-b"); !bytes.Equal(got, []byte("bbb")) {
		t.Fatalf("Clear(req-a) disturbed req-b: got %q", got)
	}
	// Clearing an absent id is a no-op.
	m.Clear("does-not-exist")
}

func TestElevationManagerClearAllZeroesEveryEntry(t *testing.T) {
	m := NewElevationManager()
	m.Store("req-a", []byte("aaa"))
	m.Store("req-b", []byte("bbb"))
	storedA := m.secrets["req-a"]
	storedB := m.secrets["req-b"]

	m.ClearAll()
	for name, stored := range map[string][]byte{"req-a": storedA, "req-b": storedB} {
		for i, b := range stored {
			if b != 0 {
				t.Fatalf("ClearAll did not zero %s at index %d: %d", name, i, b)
			}
		}
	}
	if m.Take("req-a") != nil || m.Take("req-b") != nil {
		t.Fatal("entries still returned after ClearAll")
	}
	// ClearAll on an empty store is a no-op.
	m.ClearAll()
}

func TestSubagentHasDistinctElevationManager(t *testing.T) {
	parent := NewAgent(nil, nil, nil, nil)
	child := NewAgent(nil, nil, nil, nil)
	if parent.elevation == nil || child.elevation == nil {
		t.Fatal("agent elevation manager not initialized in constructor")
	}
	if parent.elevation == child.elevation {
		t.Fatal("subagent shares the parent's elevation manager")
	}
}

func TestClearElevationSecretsIsNilSafe(t *testing.T) {
	var a *Agent
	a.ClearElevationSecrets() // must not panic
	bare := &Agent{}          // directly-constructed agent has no store
	bare.ClearElevationSecrets()
	if bare.elevation != nil {
		t.Fatal("ClearElevationSecrets must not allocate a store")
	}

	a = NewAgent(nil, nil, nil, nil)
	a.elevation.Store("req", []byte("secret"))
	a.ClearElevationSecrets()
	if a.elevation.Take("req") != nil {
		t.Fatal("ClearElevationSecrets did not clear the store")
	}
}

func TestRekeyClearsElevationSecrets(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rekey func(a *Agent)
	}{
		{"RekeySession", func(a *Agent) { a.RekeySession("ses_old", "ses_new") }},
		{"RekeyOpenCodeSession", func(a *Agent) { a.RekeyOpenCodeSession("ses_old", "ses_new") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAgent(nil, nil, nil, nil)
			a.elevation.Store("req", []byte("secret"))
			tc.rekey(a)
			if a.elevation.Take("req") != nil {
				t.Fatalf("%s did not clear elevation secrets", tc.name)
			}
		})
	}
}

func TestShutdownClearsElevationSecrets(t *testing.T) {
	a := NewAgent(nil, nil, nil, nil)
	a.elevation.Store("req", []byte("secret"))
	a.Shutdown()
	if a.elevation.Take("req") != nil {
		t.Fatal("Shutdown did not clear elevation secrets")
	}
}

func TestElevationSecretContextCarriesGetterOnly(t *testing.T) {
	called := false
	ctx := tool.WithElevationSecret(context.Background(), func() []byte {
		called = true
		return []byte("secret")
	})
	getter := tool.ElevationSecretFromContext(ctx)
	if getter == nil {
		t.Fatal("getter missing from context")
	}
	if called {
		t.Fatal("WithElevationSecret must not call the getter")
	}
	if got := getter(); !bytes.Equal(got, []byte("secret")) || !called {
		t.Fatalf("getter() = %q, called = %v", got, called)
	}
	if got := tool.ElevationSecretFromContext(context.Background()); got != nil {
		t.Fatal("no-getter context must return nil")
	}
}

// elevationCaptureTool records the secret a ContextualTool receives through its
// execution context, so the re-execution path can be checked end to end.
type elevationCaptureTool struct {
	got string
}

func (t *elevationCaptureTool) Name() string               { return "elev_probe" }
func (t *elevationCaptureTool) Description() string        { return "" }
func (t *elevationCaptureTool) Definition() map[string]any { return map[string]any{"name": t.Name()} }
func (t *elevationCaptureTool) Execute(json.RawMessage) (string, error) {
	return "", nil
}
func (t *elevationCaptureTool) Parallel() bool { return false }
func (t *elevationCaptureTool) ExecuteCtx(ctx context.Context, _ json.RawMessage) (string, error) {
	getter := tool.ElevationSecretFromContext(ctx)
	if getter == nil {
		t.got = "NO_GETTER"
		return t.got, nil
	}
	t.got = string(getter())
	return t.got, nil
}

func TestHandleApprovedToolCallCtxCarriesContext(t *testing.T) {
	a := NewAgent(nil, nil, nil, nil)
	probe := &elevationCaptureTool{}
	a.tools[probe.Name()] = probe

	ctx := tool.WithElevationSecret(context.Background(), func() []byte {
		return []byte("s3cr3t")
	})
	got, err := a.HandleApprovedToolCallCtx(ctx, probe.Name(), json.RawMessage(`{}`), "tc-1")
	if err != nil {
		t.Fatalf("HandleApprovedToolCallCtx: %v", err)
	}
	if got != "s3cr3t" || probe.got != "s3cr3t" {
		t.Fatalf("re-execution lost the caller context: result=%q tool=%q", got, probe.got)
	}
}

func TestHandleApprovedToolCallAttachesElevationGetter(t *testing.T) {
	a := NewAgent(nil, nil, nil, nil)
	probe := &elevationCaptureTool{}
	a.tools[probe.Name()] = probe
	a.elevation.Store("tc-1", []byte("s3cr3t"))

	got, err := a.HandleApprovedToolCall(probe.Name(), json.RawMessage(`{}`), "tc-1")
	if err != nil {
		t.Fatalf("HandleApprovedToolCall: %v", err)
	}
	if got != "s3cr3t" {
		t.Fatalf("re-execution did not receive the stored secret: got %q", got)
	}
	// The getter takes (consumes) the secret; a second re-execution has none.
	got2, err := a.HandleApprovedToolCall(probe.Name(), json.RawMessage(`{}`), "tc-1")
	if err != nil {
		t.Fatalf("second HandleApprovedToolCall: %v", err)
	}
	if got2 != "NO_GETTER" && got2 != "" {
		t.Fatalf("secret survived consumption: second result %q", got2)
	}
}

// elevationFlagProbe records the elevation flag and secret a ContextualTool sees
// on its execution context, so HandleApprovedToolCall's flag wiring is testable.
type elevationFlagProbe struct {
	name      string
	gotFlag   bool
	gotSecret string
}

func (p *elevationFlagProbe) Name() string               { return p.name }
func (p *elevationFlagProbe) Description() string        { return "" }
func (p *elevationFlagProbe) Definition() map[string]any { return map[string]any{"name": p.Name()} }
func (p *elevationFlagProbe) Execute(json.RawMessage) (string, error) {
	return "", nil
}
func (p *elevationFlagProbe) Parallel() bool { return false }
func (p *elevationFlagProbe) ExecuteCtx(ctx context.Context, _ json.RawMessage) (string, error) {
	p.gotFlag = tool.ElevationFlagFromContext(ctx)
	if getter := tool.ElevationSecretFromContext(ctx); getter != nil {
		p.gotSecret = string(getter())
	}
	return "", nil
}

func newElevationFlagAgent(t *testing.T, name string) (*Agent, *elevationFlagProbe) {
	t.Helper()
	a := NewAgent(nil, nil, nil, nil)
	probe := &elevationFlagProbe{name: name}
	a.tools[name] = probe
	return a, probe
}

// A leading sudo/su approval carries the flag AND the secret. The flag is what
// makes the tool take the elevated path; the secret only supplies the password.
func TestHandleApprovedToolCallSetsElevationFlagForSudo(t *testing.T) {
	a, probe := newElevationFlagAgent(t, "bash")
	a.elevation.Store("tc-elev", []byte("s3cr3t"))
	args := json.RawMessage(`{"command":"sudo whoami"}`)
	if _, err := a.HandleApprovedToolCall("bash", args, "tc-elev"); err != nil {
		t.Fatalf("HandleApprovedToolCall: %v", err)
	}
	if !probe.gotFlag {
		t.Fatal("elevation flag not set for a leading sudo approval")
	}
	if probe.gotSecret != "s3cr3t" {
		t.Fatalf("secret not attached: got %q", probe.gotSecret)
	}
}

// The flag is independent of the secret: even a lost/never-stored secret must
// be marked as an elevation so the tool fails closed instead of running the
// command unprivileged on the ordinary path.
func TestHandleApprovedToolCallSetsElevationFlagWithoutSecret(t *testing.T) {
	a, probe := newElevationFlagAgent(t, "bash")
	args := json.RawMessage(`{"command":"su -c whoami"}`)
	if _, err := a.HandleApprovedToolCall("bash", args, "tc-lost"); err != nil {
		t.Fatalf("HandleApprovedToolCall: %v", err)
	}
	if !probe.gotFlag {
		t.Fatal("elevation flag not set for a leading su approval with no secret")
	}
	if probe.gotSecret != "" {
		t.Fatalf("unexpected secret: %q", probe.gotSecret)
	}
}

// An ordinary approval must NOT carry the flag, or it would be routed through
// the elevated executor.
func TestHandleApprovedToolCallNoElevationFlagForOrdinary(t *testing.T) {
	a, probe := newElevationFlagAgent(t, "bash")
	for _, args := range []json.RawMessage{
		json.RawMessage(`{"command":"echo ordinary-ok"}`),
		json.RawMessage(`{"command":"echo sudo is data"}`),
	} {
		probe.gotFlag = false
		if _, err := a.HandleApprovedToolCall("bash", args, "tc-ordinary"); err != nil {
			t.Fatalf("HandleApprovedToolCall: %v", err)
		}
		if probe.gotFlag {
			t.Fatalf("elevation flag set for ordinary command %s", args)
		}
	}
}

// A non-bash tool is never an elevation, whatever its args look like.
func TestHandleApprovedToolCallNoElevationFlagForNonBash(t *testing.T) {
	a, probe := newElevationFlagAgent(t, "other_tool")
	if _, err := a.HandleApprovedToolCall("other_tool", json.RawMessage(`{"command":"sudo whoami"}`), "tc-other"); err != nil {
		t.Fatalf("HandleApprovedToolCall: %v", err)
	}
	if probe.gotFlag {
		t.Fatal("elevation flag set for a non-bash tool")
	}
}
