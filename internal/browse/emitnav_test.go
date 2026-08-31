package browse

import (
	"testing"

	"github.com/u007/ocode/internal/browse/cdp"
)

// TestEmitNavAdapterStampsChromeMode drives the exact seam between
// internal/browse/cdp and the browse nav publisher. The cdp.Manager calls
// its ManagerOptions.EmitNav closure for every navigation it observes; the
// browse server installs an adapter that stamps Mode:"chrome". The manager
// itself never sets Mode, so this closure is solely responsible for the
// stamp — fire it directly and assert the full payload passthrough.
func TestEmitNavAdapterStampsChromeMode(t *testing.T) {
	s := New("apitoken", nil)
	var got []NavEvent
	s.SetNavPublisher(func(_ string, ev NavEvent) { got = append(got, ev) })

	s.initManager(Options{}) // wires the EmitNav adapter onto a real cdp.Manager
	mgr, ok := s.cdp.(*realManagerAdapter)
	if !ok || mgr == nil || mgr.Manager == nil {
		t.Fatalf("cdp manager not initialized as *realManagerAdapter")
	}

	// emitNavForTest fires opts.EmitNav exactly as Manager.emitNav would.
	want := cdp.NavEvent{StateKey: "tab:nav", URL: "https://example.com/x", Status: 0}
	mgr.Manager.EmitNavForTest(want)
	if len(got) != 1 {
		t.Fatalf("adapter emitted %d events, want 1", len(got))
	}
	wantNav := NavEvent{StateKey: "tab:nav", URL: "https://example.com/x", Status: 0, Mode: "chrome"}
	if got[0] != wantNav {
		t.Fatalf("adapter output = %+v, want %+v", got[0], wantNav)
	}
}

// TestConfigureZeroOptionsInstallsManager: Configure with zero-value Options
// must still install the manager (and thus the EmitNav adapter); a nil
// manager would silently drop all chrome nav events.
func TestConfigureZeroOptionsInstallsManager(t *testing.T) {
	s := New("apitoken", nil)
	s.Configure(Options{})
	if s.cdp == nil {
		t.Fatal("Configure(Options{}) did not install a cdp manager")
	}
	if _, ok := s.cdp.(*realManagerAdapter); !ok {
		t.Fatalf("cdp = %T, want *realManagerAdapter", s.cdp)
	}
}
