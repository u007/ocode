package tool

import (
	"context"
	"encoding/json"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Supervisor‑backed ProcessRegistry behaviour
// ---------------------------------------------------------------------------

func TestProcessRegistry_WithSupervisor_StartBackgroundOutput(t *testing.T) {
	sup := NewProcessSupervisor(ProcessSupervisorOptions{})
	reg := NewProcessRegistry()
	reg.SetSupervisor(sup)

	p := reg.StartBackground("echo sup-out")
	if p.ID == "" {
		t.Fatal("expected non-empty id")
	}
	for i := 0; i < 200; i++ {
		if st, _ := p.snapshotStatus(); st != ProcRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	out, st, code, err := reg.Output(p.ID)
	if err != nil {
		t.Fatalf("Output error: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if st != ProcExited {
		t.Fatalf("status = %s, want %s", st, ProcExited)
	}
	if !strings.Contains(out, "sup-out") {
		t.Fatalf("output missing echo: %q", out)
	}
}

func TestProcessRegistry_WithSupervisor_Snapshot(t *testing.T) {
	sup := NewProcessSupervisor(ProcessSupervisorOptions{})
	reg := NewProcessRegistry()
	reg.SetSupervisor(sup)

	p := reg.StartBackground("echo snap-me")
	for i := 0; i < 200; i++ {
		if st, _ := p.snapshotStatus(); st != ProcRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	snap := reg.Snapshot()
	if len(snap) < 1 {
		t.Fatal("expected at least 1 process in snapshot")
	}
	found := false
	for _, pi := range snap {
		if pi.ID == p.ID && pi.Command == "echo snap-me" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("process %q not found in snapshot %v", p.ID, snap)
	}
}

func TestProcessRegistry_WithSupervisor_Kill(t *testing.T) {
	sup := NewProcessSupervisor(ProcessSupervisorOptions{})
	reg := NewProcessRegistry()
	reg.SetSupervisor(sup)

	p := reg.StartBackground("sleep 30")
	out, err := reg.Kill(p.ID)
	if err != nil {
		t.Fatalf("Kill error: %v", err)
	}
	if !strings.Contains(out, "killed") {
		t.Fatalf("expected kill confirmation, got %q", out)
	}

	_, st, _, _ := reg.Dump(p.ID)
	if st != ProcKilled {
		t.Fatalf("status = %s, want %s", st, ProcKilled)
	}

	rec, ok := sup.Lookup(p.ID)
	if !ok {
		t.Fatal("supervisor should have record")
	}
	if rec.Status != ProcKilled {
		t.Fatalf("supervisor status = %s, want %s", rec.Status, ProcKilled)
	}
}

func TestProcessRegistry_WithSupervisor_KillAllDelegates(t *testing.T) {
	sup := NewProcessSupervisor(ProcessSupervisorOptions{})
	reg := NewProcessRegistry()
	reg.SetSupervisor(sup)

	_ = reg.StartBackground("sleep 30")
	_ = reg.StartBackground("sleep 30")

	reg.KillAll()

	for _, rec := range sup.Snapshot() {
		if rec.Status == ProcRunning {
			t.Fatalf("process %s still running after KillAll", rec.ID)
		}
	}
}

func TestProcessRegistry_WithSupervisor_Dump(t *testing.T) {
	sup := NewProcessSupervisor(ProcessSupervisorOptions{})
	reg := NewProcessRegistry()
	reg.SetSupervisor(sup)

	p := reg.StartBackground("echo dump-me")
	for i := 0; i < 200; i++ {
		if st, _ := p.snapshotStatus(); st != ProcRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	text, st, code, err := reg.Dump(p.ID)
	if err != nil {
		t.Fatalf("Dump error: %v", err)
	}
	if !strings.Contains(text, "dump-me") {
		t.Fatalf("dump missing text: %q", text)
	}
	if st != ProcExited || code != 0 {
		t.Fatalf("status=%s code=%d", st, code)
	}
}

func TestProcessRegistry_WithSupervisor_FailedToStartRetained(t *testing.T) {
	sup := NewProcessSupervisor(ProcessSupervisorOptions{})
	reg := NewProcessRegistry()
	reg.SetSupervisor(sup)

	p := reg.StartBackground("exit 42")
	time.Sleep(100 * time.Millisecond) // let it finish

	_, st, code, err := reg.Dump(p.ID)
	if err != nil {
		t.Fatalf("Dump error: %v", err)
	}
	if st == ProcRunning {
		t.Fatal("finished process should not be running")
	}
	if code != 42 {
		t.Fatalf("exit code = %d, want 42", code)
	}

	rec, ok := sup.Lookup(p.ID)
	if !ok {
		t.Fatal("terminal process must be retained in supervisor")
	}
	if rec.Status != ProcExited {
		t.Fatalf("supervisor status = %s, want %s", rec.Status, ProcExited)
	}
	if rec.ExitCode != 42 {
		t.Fatalf("supervisor exit code = %d, want 42", rec.ExitCode)
	}

	snap := reg.Snapshot()
	found := false
	for _, pi := range snap {
		if pi.ID == p.ID {
			found = true
			if pi.Status == ProcRunning {
				t.Fatal("snapshot shows finished process as running")
			}
			break
		}
	}
	if !found {
		t.Fatal("finished process missing from snapshot")
	}
}

func TestProcessRegistry_WithSupervisor_RunningCount(t *testing.T) {
	sup := NewProcessSupervisor(ProcessSupervisorOptions{})
	reg := NewProcessRegistry()
	reg.SetSupervisor(sup)

	_ = reg.StartBackground("sleep 30")
	_ = reg.StartBackground("echo done")
	time.Sleep(100 * time.Millisecond) // let echo finish

	n := reg.RunningCount()
	// sleep is running, echo finished → 1 or 2 depending on timing
	if n < 1 {
		t.Fatalf("running count = %d, want >= 1", n)
	}
}

func TestProcessRegistry_WithSupervisor_SetOnDoneFires(t *testing.T) {
	sup := NewProcessSupervisor(ProcessSupervisorOptions{})
	reg := NewProcessRegistry()
	reg.SetSupervisor(sup)

	done := make(chan string, 1)
	reg.SetOnDone(func(p *Process) {
		done <- p.ID
	})

	p := reg.StartBackground("echo done-cb")
	for i := 0; i < 200; i++ {
		if st, _ := p.snapshotStatus(); st != ProcRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case id := <-done:
		if id != p.ID {
			t.Fatalf("onDone id = %s, want %s", id, p.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("onDone never fired")
	}
}

func TestProcessRegistry_WithSupervisor_OutputAndDumpUseSupervisorState(t *testing.T) {
	sup := NewProcessSupervisor(ProcessSupervisorOptions{})
	reg := NewProcessRegistry()
	reg.SetSupervisor(sup)

	p := &Process{ID: "proc-1", Command: "synthetic", Status: ProcRunning}
	p.appendOutput([]byte("hello from buffer\n"))

	reg.mu.Lock()
	reg.procs[p.ID] = p
	reg.order = append(reg.order, p.ID)
	reg.mu.Unlock()

	if _, err := sup.Register(ProcessRegistration{
		ID:        p.ID,
		Command:   p.Command,
		Kind:      ProcessKindBackgroundBash,
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Register error: %v", err)
	}
	if !sup.MarkExited(p.ID, 23) {
		t.Fatal("expected supervisor record to transition to exited")
	}

	out, st, code, err := reg.Output(p.ID)
	if err != nil {
		t.Fatalf("Output error: %v", err)
	}
	if !strings.Contains(out, "hello from buffer") {
		t.Fatalf("output missing buffered text: %q", out)
	}
	if st != ProcExited {
		t.Fatalf("Output status = %s, want %s", st, ProcExited)
	}
	if code != 23 {
		t.Fatalf("Output exit code = %d, want 23", code)
	}

	full, dumpStatus, dumpCode, err := reg.Dump(p.ID)
	if err != nil {
		t.Fatalf("Dump error: %v", err)
	}
	if !strings.Contains(full, "hello from buffer") {
		t.Fatalf("dump missing buffered text: %q", full)
	}
	if dumpStatus != ProcExited {
		t.Fatalf("Dump status = %s, want %s", dumpStatus, ProcExited)
	}
	if dumpCode != 23 {
		t.Fatalf("Dump exit code = %d, want 23", dumpCode)
	}
}

func TestProcessRegistry_WithSupervisor_ClosedSupervisorRetainsFailedStartRecord(t *testing.T) {
	sup := NewProcessSupervisor(ProcessSupervisorOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sup.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown error: %v", err)
	}

	reg := NewProcessRegistry()
	reg.SetSupervisor(sup)

	p := reg.StartBackground("echo should-not-start")

	text, st, code, err := reg.Dump(p.ID)
	if err != nil {
		t.Fatalf("Dump error: %v", err)
	}
	if !strings.Contains(text, "failed to start") {
		t.Fatalf("dump missing startup failure text: %q", text)
	}
	if st != ProcExited {
		t.Fatalf("status = %s, want %s", st, ProcExited)
	}
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}

	snap := reg.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}
	if snap[0].ID != p.ID {
		t.Fatalf("snapshot id = %q, want %q", snap[0].ID, p.ID)
	}
	if snap[0].Status != ProcExited {
		t.Fatalf("snapshot status = %s, want %s", snap[0].Status, ProcExited)
	}
	if reg.RunningCount() != 0 {
		t.Fatalf("running count = %d, want 0", reg.RunningCount())
	}
}

func TestBashToolForegroundRegistersWithSupervisor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sleep")
	}
	sup := NewProcessSupervisor(ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	reg := NewProcessRegistry()
	reg.SetSupervisor(sup)
	done := make(chan struct{})

	go func() {
		_, _ = (BashTool{Procs: reg}).Execute(json.RawMessage(`{"command":"sleep 30"}`))
		close(done)
	}()

	for i := 0; i < 100; i++ {
		if len(sup.Snapshot()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(sup.Snapshot()) == 0 {
		t.Fatal("expected foreground bash command to register with supervisor")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sup.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("foreground bash command did not stop after supervisor shutdown")
	}
}

func TestBashToolForegroundCanBeMovedToBackground(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sleep")
	}
	reg := NewProcessRegistry()
	completed := make(chan *Process, 1)
	reg.SetOnDone(func(p *Process) { completed <- p })
	resultCh := make(chan string, 1)

	go func() {
		out, _ := (BashTool{Procs: reg}).Execute(json.RawMessage(`{"command":"sleep 0.2; echo promoted"}`))
		resultCh <- out
	}()

	var id string
	for i := 0; i < 100; i++ {
		var ok bool
		id, _, ok = reg.RequestBackgroundLatest()
		if ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("expected running foreground bash to become backgroundable")
	}

	out := <-resultCh
	if !strings.Contains(out, "Moved running bash command to background as "+id) {
		t.Fatalf("unexpected promotion result: %q", out)
	}

	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("expected promoted process completion callback")
	}

	text, st, code, err := reg.Dump(id)
	if err != nil {
		t.Fatalf("Dump error: %v", err)
	}
	if st != ProcExited {
		t.Fatalf("status = %s, want %s", st, ProcExited)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(text, "promoted") {
		t.Fatalf("dump missing promoted output: %q", text)
	}
}

func TestBashToolForegroundDoesNotEmitCompletionWithoutPromotion(t *testing.T) {
	reg := NewProcessRegistry()
	completed := make(chan struct{}, 1)
	reg.SetOnDone(func(*Process) { completed <- struct{}{} })

	out, err := (BashTool{Procs: reg}).Execute(json.RawMessage(`{"command":"echo hi"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if strings.TrimSpace(out) != "hi" {
		t.Fatalf("output = %q, want hi", out)
	}

	select {
	case <-completed:
		t.Fatal("unexpected completion callback for non-promoted foreground bash")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestProcessRingBufferTruncates(t *testing.T) {
	p := &Process{ID: "proc-1", Command: "x", Status: ProcRunning}
	// Write more than the cap.
	big := strings.Repeat("a", procBufferCap+5000)
	p.appendOutput([]byte(big))
	text, _ := p.readSince()
	if len(text) > procBufferCap+64 {
		t.Fatalf("buffer not capped: got %d bytes", len(text))
	}
	if !strings.Contains(text, "truncated") {
		t.Fatalf("expected truncation marker, got prefix %q", text[:40])
	}
}

func TestProcessReadSinceIsIncremental(t *testing.T) {
	p := &Process{ID: "proc-1", Command: "x", Status: ProcRunning}
	p.appendOutput([]byte("hello "))
	first, _ := p.readSince()
	if first != "hello " {
		t.Fatalf("first read = %q", first)
	}
	p.appendOutput([]byte("world"))
	second, _ := p.readSince()
	if second != "world" {
		t.Fatalf("second read = %q, want incremental %q", second, "world")
	}
}

func TestProcessRegistryRunAndCapture(t *testing.T) {
	r := NewProcessRegistry()
	p := r.StartBackground("echo hello-bg")
	if p.ID == "" {
		t.Fatal("expected non-empty process id")
	}
	// Poll until the process exits (cap the wait).
	for i := 0; i < 200; i++ {
		if st, _ := p.snapshotStatus(); st != ProcRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	out, _, code, err := r.Output(p.ID)
	if err != nil {
		t.Fatalf("Output err: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "hello-bg") {
		t.Fatalf("output missing echo: %q", out)
	}
}

func TestProcessRegistryUnknownID(t *testing.T) {
	r := NewProcessRegistry()
	if _, _, _, err := r.Output("proc-999"); err == nil {
		t.Fatal("expected error for unknown id")
	}
	if _, err := r.Kill("proc-999"); err == nil {
		t.Fatal("expected error killing unknown id")
	}
}

func TestBashToolBackgroundReturnsID(t *testing.T) {
	r := NewProcessRegistry()
	bt := &BashTool{Procs: r}
	out, err := bt.Execute(json.RawMessage(`{"command":"echo hi","run_in_background":true}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(out, "proc-1") {
		t.Fatalf("expected process id in output, got %q", out)
	}
	if len(r.Snapshot()) != 1 {
		t.Fatalf("expected 1 registered process, got %d", len(r.Snapshot()))
	}
}

func TestBashToolForegroundUnchanged(t *testing.T) {
	bt := &BashTool{}
	out, err := bt.Execute(json.RawMessage(`{"command":"echo sync-ok"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(out, "sync-ok") {
		t.Fatalf("foreground output wrong: %q", out)
	}
}

func TestBashOutputTool(t *testing.T) {
	r := NewProcessRegistry()
	p := r.StartBackground("echo poll-me")
	for i := 0; i < 200; i++ {
		if st, _ := p.snapshotStatus(); st != ProcRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	bo := BashOutputTool{Procs: r}
	out, err := bo.Execute(json.RawMessage(`{"id":"` + p.ID + `"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(out, "poll-me") {
		t.Fatalf("bash_output missing process text: %q", out)
	}
}

// TestProcessRegistry_SupervisorIDPrefix_PreventsCollision verifies that two
// ProcessRegistry instances sharing one ProcessSupervisor can both spawn
// processes whose local IDs collide ("proc-1") without colliding in the
// supervisor's records map, when each registry is given a distinct supID
// prefix via SetSupervisorIDPrefix.
func TestProcessRegistry_SupervisorIDPrefix_PreventsCollision(t *testing.T) {
	sup := NewProcessSupervisor(ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	regA := NewProcessRegistry()
	regA.SetSupervisor(sup)
	regA.SetSupervisorIDPrefix("a-")
	regB := NewProcessRegistry()
	regB.SetSupervisor(sup)
	regB.SetSupervisorIDPrefix("b-")

	pA := regA.StartBackground("true")
	pB := regB.StartBackground("true")

	if pA.ID != "proc-1" || pB.ID != "proc-1" {
		t.Fatalf("expected both local IDs to be proc-1 (got %q and %q)", pA.ID, pB.ID)
	}

	// Both registrations should appear distinctly in the supervisor.
	if _, ok := sup.Lookup("a-proc-1"); !ok {
		t.Fatalf("supervisor missing a-proc-1 record")
	}
	if _, ok := sup.Lookup("b-proc-1"); !ok {
		t.Fatalf("supervisor missing b-proc-1 record")
	}

	// Drain to exit so test goroutines finish.
	for i := 0; i < 200; i++ {
		stA, _ := pA.snapshotStatus()
		stB, _ := pB.snapshotStatus()
		if stA != ProcRunning && stB != ProcRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestProcessRegistry_NoPrefix_CollidesInSupervisor confirms the negative
// case: without SetSupervisorIDPrefix, two registries sharing a supervisor
// collide on the second registration. This documents the failure mode that
// SetSupervisorIDPrefix exists to prevent.
func TestProcessRegistry_NoPrefix_CollidesInSupervisor(t *testing.T) {
	sup := NewProcessSupervisor(ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	regA := NewProcessRegistry()
	regA.SetSupervisor(sup)
	regB := NewProcessRegistry()
	regB.SetSupervisor(sup)

	pA := regA.StartBackground("true")
	pB := regB.StartBackground("true")

	// First registration succeeds; second collides (supID "proc-1" already exists).
	for i := 0; i < 200; i++ {
		stA, _ := pA.snapshotStatus()
		stB, _ := pB.snapshotStatus()
		if stA != ProcRunning && stB != ProcRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	outB, _, _, err := regB.Output(pB.ID)
	if err != nil {
		t.Fatalf("regB Output: %v", err)
	}
	if !strings.Contains(outB, "already registered") {
		t.Fatalf("expected regB to fail with 'already registered'; got %q", outB)
	}
}

// TestProcessRegistry_SeedCounter_AvoidsReuseOnSharedSupervisor reproduces the
// /model-switch collision: a new registry attaches to a still-live session
// supervisor that retains the prior registry's proc-N records. Seeding the new
// registry's counter from the old high-water mark continues the sequence
// (proc-2) instead of reusing proc-1 and colliding.
func TestProcessRegistry_SeedCounter_AvoidsReuseOnSharedSupervisor(t *testing.T) {
	sup := NewProcessSupervisor(ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})
	regA := NewProcessRegistry()
	regA.SetSupervisor(sup)
	pA := regA.StartBackground("true")
	waitProcDone(t, pA)

	// Terminated children remain in the supervisor's records map.
	_ = sup.TerminateAll(context.Background())

	// New agent's registry on the same supervisor, seeded from the old counter.
	regB := NewProcessRegistry()
	regB.SetSupervisor(sup)
	regB.SeedCounter(regA.Counter())

	pB := regB.StartBackground("true")
	if pB.ID != "proc-2" {
		t.Fatalf("expected continued ID proc-2, got %q", pB.ID)
	}
	waitProcDone(t, pB)

	outB, _, _, err := regB.Output(pB.ID)
	if err != nil {
		t.Fatalf("regB Output: %v", err)
	}
	if strings.Contains(outB, "already registered") {
		t.Fatalf("seeded registry should not collide; got %q", outB)
	}
}

func waitProcDone(t *testing.T, p *Process) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if st, _ := p.snapshotStatus(); st != ProcRunning {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %s did not finish", p.ID)
}

func TestKillShellTool(t *testing.T) {
	r := NewProcessRegistry()
	p := r.StartBackground("sleep 30")
	ks := KillShellTool{Procs: r}
	out, err := ks.Execute(json.RawMessage(`{"id":"` + p.ID + `"}`))
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !strings.Contains(out, "killed") {
		t.Fatalf("expected kill confirmation, got %q", out)
	}
}

func TestProcessRegistry_ForegroundWithPrefix_FinalizeMarksSupervisorRecord(t *testing.T) {
	// Reproduces the foreground-bash + SupervisorIDPrefix bug: registering
	// under "<prefix>proc-N" but calling MarkExited(proc.ID) would leave the
	// supervisor record stuck in Running. The fix caches proc.SupKey at
	// registration so finalizeManagedProcess can address the correct record.
	sup := NewProcessSupervisor(ProcessSupervisorOptions{})
	reg := NewProcessRegistry()
	reg.SetSupervisor(sup)
	reg.SetSupervisorIDPrefix("sub-7-")

	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd start: %v", err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	proc, err := reg.RegisterForeground("true", cmd, time.Now(), func() error { return <-waitDone })
	if err != nil {
		t.Fatalf("RegisterForeground: %v", err)
	}

	wantKey := "sub-7-" + proc.ID
	if proc.SupKey() != wantKey {
		t.Fatalf("proc.SupKey() = %q, want %q", proc.SupKey(), wantKey)
	}

	if _, ok := sup.Lookup(wantKey); !ok {
		t.Fatalf("supervisor record %q not found after registration", wantKey)
	}

	// Drive the command to completion (matches the exec.go path that calls
	// finalizeManagedProcess after the wait returns).
	finalizeManagedProcess(proc, sup, nil, <-waitDone)

	rec, ok := sup.Lookup(wantKey)
	if !ok {
		t.Fatalf("supervisor record %q vanished after finalize", wantKey)
	}
	if rec.Status == ProcRunning {
		t.Fatalf("supervisor record stuck in Running after finalize (status=%s) — supKey mismatch?", rec.Status)
	}
}
