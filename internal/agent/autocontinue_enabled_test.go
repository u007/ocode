package agent

import "testing"

func TestAutoContinueEnabledRuntimeToggle(t *testing.T) {
	a := &Agent{}
	a.autoContinueEnabled.Store(false)
	if a.AutoContinueEnabled() {
		t.Fatal("expected off")
	}
	a.SetAutoContinueEnabled(true)
	if !a.AutoContinueEnabled() {
		t.Fatal("expected on after SetAutoContinueEnabled")
	}
}
