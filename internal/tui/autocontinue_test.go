package tui

import (
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestAutoContinueCommandRegistered(t *testing.T) {
	found := false
	for _, spec := range commandSpecs {
		if spec.name == "/autocontinue" {
			found = true
			if spec.handler == nil {
				t.Error("/autocontinue has no handler")
			}
			break
		}
	}
	if !found {
		t.Error("/autocontinue not found in commandSpecs")
	}
}

// TestAutoContinueChainCap simulates streamDoneMsg's guard-then-fire sequence
// across repeated step-limit cutoffs, and asserts the chain stops at
// autoContinueMaxChain rather than looping forever.
func TestAutoContinueChainCap(t *testing.T) {
	count := 0
	lastWasAuto := false
	fires := 0

	// Returns whether this turn fired an auto-continue (i.e. whether a next
	// turn happens at all — without a fire, nothing calls askAgent() again,
	// so streamDoneMsg never re-enters this logic on its own).
	simulateTurn := func(stepLimitHit bool) bool {
		// Mirrors the streamDoneMsg handler: reset unless the PRIOR turn was
		// itself an auto-continue fire.
		if !lastWasAuto {
			count = 0
		}
		lastWasAuto = false
		if shouldAutoContinue(true /* enabled */, stepLimitHit, count) {
			count++
			lastWasAuto = true
			fires++
		}
		return lastWasAuto
	}

	// A runaway model keeps hitting the step limit turn after turn. Each
	// fired auto-continue triggers exactly one more turn; once the cap
	// declines to fire, nothing drives a further turn on its own — so drive
	// the loop only as long as it keeps firing, and confirm it stops instead
	// of running unbounded.
	for i := 0; i < 20 && simulateTurn(true); i++ {
	}
	if fires != autoContinueMaxChain {
		t.Errorf("consecutive step-limit turns: got %d fires, want %d (the cap)", fires, autoContinueMaxChain)
	}
	if count != autoContinueMaxChain {
		t.Errorf("count = %d, want %d after hitting the cap", count, autoContinueMaxChain)
	}

	// A turn that does NOT hit the step limit (human sent a normal message,
	// or the model just finished) must reset the chain.
	simulateTurn(false)
	if count != 0 {
		t.Errorf("count = %d after a non-step-limit turn, want 0 (reset)", count)
	}

	// And the chain can fire again from zero afterwards.
	fires = 0
	for i := 0; i < 2; i++ {
		simulateTurn(true)
	}
	if fires != 2 {
		t.Errorf("got %d fires after reset, want 2", fires)
	}
}

func TestShouldAutoContinueGuards(t *testing.T) {
	cases := []struct {
		name         string
		enabled      bool
		stepLimitHit bool
		count        int
		want         bool
	}{
		{"disabled", false, true, 0, false},
		{"no step-limit signal", true, false, 0, false},
		{"at cap", true, true, autoContinueMaxChain, false},
		{"eligible", true, true, 0, true},
	}
	for _, c := range cases {
		got := shouldAutoContinue(c.enabled, c.stepLimitHit, c.count)
		if got != c.want {
			t.Errorf("%s: shouldAutoContinue(%v,%v,%d) = %v, want %v",
				c.name, c.enabled, c.stepLimitHit, c.count, got, c.want)
		}
	}
}

func TestResolveMaskModelArg(t *testing.T) {
	m := &model{}
	m.config = &config.Config{
		Ocode: config.OcodeConfig{
			LocalModels: map[string]config.LocalModelConfig{
				"local/bonsai-8b-mlx-1bit": {Enabled: true, MaxParallel: 1},
			},
		},
	}

	cases := []struct {
		name string
		want string
	}{
		{"bonsai-8b-mlx-1bit", "local/bonsai-8b-mlx-1bit"},       // bare registered local id → prefixed
		{"local/bonsai-8b-mlx-1bit", "local/bonsai-8b-mlx-1bit"}, // already prefixed → unchanged
		{"lmstudio/some-model", "lmstudio/some-model"},           // has "/" already → unchanged
		{"unregistered-name", "unregistered-name"},               // not a registered local id → unchanged
	}
	for _, c := range cases {
		got := m.resolveMaskModelArg(c.name)
		if got != c.want {
			t.Errorf("resolveMaskModelArg(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}
