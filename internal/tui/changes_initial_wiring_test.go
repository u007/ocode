package tui

import "testing"

// Regression: newModel builds the initial agent from config but only
// installAgent wired the changes tab's registry accessor, so a session that
// never swapped agents (no /model switch, no /connect) showed "no changes in
// this session yet." forever even though the agent's snapshot store was
// recording every edit.
func TestNewModelWiresChangesRegistryToInitialAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENCODE_CONFIG_DIR", t.TempDir())
	t.Setenv("OPENCODE_MODEL", "lmstudio/test-model") // key-optional provider: NewClient builds offline

	m := newModel()
	if m.agent == nil {
		t.Fatal("expected newModel to build an initial agent from OPENCODE_MODEL")
	}
	if m.changes.getRegistry == nil {
		t.Fatal("changes tab registry accessor not wired to the initial agent")
	}
	if got := m.changes.getRegistry(); got != m.agent.Changes() {
		t.Fatalf("changes tab reads registry %p, initial agent owns %p", got, m.agent.Changes())
	}
}
