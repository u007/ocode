package agent

import "testing"

// SetWorkDir rewrites a.tools while the async auto-continue judge reads it
// through advisorAvailable. Under -race an unguarded map access fails here.
func TestAdvisorAvailableIsSafeAgainstSetWorkDir(t *testing.T) {
	a := newTestAgent(nil, nil, nil, nil)
	dirs := []string{t.TempDir(), t.TempDir()}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			a.SetWorkDir(dirs[i%2])
		}
	}()
	for i := 0; i < 200; i++ {
		if !a.advisorAvailable() {
			t.Fatal("advisor tool reported unavailable while it is installed and enabled")
		}
	}
	<-done
}
