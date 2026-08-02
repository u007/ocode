package agent

import "testing"

// TestRearmMaintenanceAllowsSecondShutdown proves RearmMaintenance actually
// replaces the closed docMaintCh/memoryMaintCh (and their sync.Once guards)
// with fresh ones. Without a real re-init, a second shutdownTransient() would
// panic with "close of closed channel" — Go's testing package fails the test
// on any panic, so no explicit assertion is needed for that half.
func TestRearmMaintenanceAllowsSecondShutdown(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)

	a.shutdownTransient() // mirrors what happens when a dispatch goes terminal
	a.RearmMaintenance()
	a.shutdownTransient() // must not panic
}

func TestRearmMaintenanceReopensStopChannel(t *testing.T) {
	a := NewAgent(&MockClient{}, nil, nil, nil)

	// Use shutdownTransient (not a bare Cancel) to mirror the terminal-dispatch
	// path: Cancel() alone leaves the maintenance workers alive, so
	// RearmMaintenance's channel-field writes would race their `range` loops.
	// shutdownTransient internally calls Cancel (stopCh closes, as asserted
	// below) and waits for both workers to exit via their done channels.
	a.shutdownTransient()
	select {
	case <-a.StopCh():
	default:
		t.Fatal("setup: stopCh should be closed after shutdownTransient()")
	}

	a.RearmMaintenance()
	select {
	case <-a.StopCh():
		t.Fatal("stopCh still closed after RearmMaintenance")
	default:
	}
}
