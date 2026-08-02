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

	a.Cancel()
	select {
	case <-a.StopCh():
	default:
		t.Fatal("setup: stopCh should be closed after Cancel()")
	}

	a.RearmMaintenance()
	select {
	case <-a.StopCh():
		t.Fatal("stopCh still closed after RearmMaintenance")
	default:
	}
}
