package notebus

import (
	"strings"
	"testing"
)

// TestRedaction_NoteBodyScrubbedBeforeAppend: a note body
// that contains a known-format secret is scrubbed BEFORE
// the entry reaches the bus log. The bus stores the
// redacted form, not the raw secret. This is the
// design's "secret redaction must also cover the sidecar"
// requirement.
func TestRedaction_NoteBodyScrubbedBeforeAppend(t *testing.T) {
	bus := NewBus("grp")
	// We use the canonical adapter so the test does not
	// depend on the production tier-1 detector
	// configuration.
	bus.SetRedactor(RedactBody)
	bus.Start(t.Context())
	defer func() { bus.Stop(); <-bus.Done() }()
	if _, err := bus.Append(Note(0, "a1", "x.go",
		"see the github token ghp_1234567890abcdefghijklmnopqrstuvwxyz", 0)); err != nil {
		t.Fatal(err)
	}
	snap := bus.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}
	body := snap[0].Body
	if strings.Contains(body, "ghp_1234567890abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("raw secret present in stored body:\n%s", body)
	}
	// The redacted form must be a non-empty replacement
	// (typically [REDACTED:...] or similar) so the LLM
	// still sees "there was a secret here" without seeing
	// the secret itself.
	if !strings.Contains(body, "REDACTED") && !strings.Contains(body, "[redacted") {
		t.Errorf("body not marked as redacted:\n%s", body)
	}
}

// TestRedaction_NoRedactorLeavesBodyAlone: when no
// redactor is set, the body is stored verbatim. This is
// the default for tests and for the case where the
// caller has not wired redaction (backwards-compat).
func TestRedaction_NoRedactorLeavesBodyAlone(t *testing.T) {
	bus := NewBus("grp")
	bus.Start(t.Context())
	defer func() { bus.Stop(); <-bus.Done() }()
	body := "raw secret: ghp_1234567890abcdefghijklmnopqrstuvwxyz"
	if _, err := bus.Append(Note(0, "a1", "x.go", body, 0)); err != nil {
		t.Fatal(err)
	}
	snap := bus.Snapshot()
	if snap[0].Body != body {
		t.Errorf("body altered without redactor:\n%s", snap[0].Body)
	}
}

// TestRedaction_NoteAtAndByUnaffected: redaction only
// touches the body. The At (anchor) and By (author) are
// not user-data secrets — they are protocol metadata.
func TestRedaction_NoteAtAndByUnaffected(t *testing.T) {
	bus := NewBus("grp")
	bus.SetRedactor(RedactBody)
	bus.Start(t.Context())
	defer func() { bus.Stop(); <-bus.Done() }()
	if _, err := bus.Append(Note(0, "a1", "x.go:foo",
		"token ghp_1234567890abcdefghijklmnopqrstuvwxyz", 0)); err != nil {
		t.Fatal(err)
	}
	snap := bus.Snapshot()
	if snap[0].At != "x.go:foo" {
		t.Errorf("At altered by redactor: %q", snap[0].At)
	}
	if snap[0].By != "a1" {
		t.Errorf("By altered by redactor: %q", snap[0].By)
	}
}

// TestRedaction_TouchesAndResolvesPassThrough: touches
// and resolves have no body, so redaction is a no-op for
// them. The bus must not error on those kinds.
func TestRedaction_TouchesAndResolvesPassThrough(t *testing.T) {
	bus := NewBus("grp")
	bus.SetRedactor(RedactBody)
	bus.Start(t.Context())
	defer func() { bus.Stop(); <-bus.Done() }()
	if _, err := bus.Append(Touch(0, "a1", "x.go", "edit", 0)); err != nil {
		t.Errorf("touch: %v", err)
	}
	if _, err := bus.Append(Resolve(0, "a2", 1, 0)); err != nil {
		t.Errorf("resolve: %v", err)
	}
	snap := bus.Snapshot()
	if len(snap) != 2 {
		t.Errorf("snapshot len = %d, want 2", len(snap))
	}
}

// TestRedaction_SidecarHoldsRedactedForm: the sidecar
// (the on-disk log) contains the REDACTED form, not the
// raw secret. This is the design's hard requirement: a
// crash-recovered sidecar must never expose a secret.
func TestRedaction_SidecarHoldsRedactedForm(t *testing.T) {
	dir := t.TempDir()
	bus := NewBus("grp")
	sc, err := NewSidecar(dir, "grp")
	if err != nil {
		t.Fatal(err)
	}
	bus.SetPersist(sc)
	bus.SetRedactor(RedactBody)
	bus.Start(t.Context())
	defer func() { bus.Stop(); <-bus.Done(); sc.Close() }()

	raw := "ghp_1234567890abcdefghijklmnopqrstuvwxyz"
	if _, err := bus.Append(Note(0, "a1", "x.go", raw, 0)); err != nil {
		t.Fatal(err)
	}
	bus.Stop()
	<-bus.Done()
	sc.Close()

	// Reload and inspect the on-disk log.
	entries, _, _, _, err := LoadSnapshot(dir, "grp")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if strings.Contains(entries[0].Body, raw) {
		t.Errorf("raw secret present in sidecar:\n%s", entries[0].Body)
	}
}
