package agent

import "testing"

func TestShouldOptimizeDiscovery_Gate(t *testing.T) {
	tests := []struct {
		name string
		sig  discoveryOptSignal
		want bool
	}{
		{"trivial no write", discoveryOptSignal{toolCalls: 2, writeCalls: 0}, false},
		{"trivial but discover_more still gated", discoveryOptSignal{toolCalls: 2, discoverMore: 1}, false},
		{"nontrivial but no signal", discoveryOptSignal{toolCalls: 5}, false},
		{"nontrivial + discover_more", discoveryOptSignal{toolCalls: 5, discoverMore: 1}, true},
		{"nontrivial + high explore", discoveryOptSignal{toolCalls: 6, grepReadCalls: 6}, true},
		{"nontrivial + low precision", discoveryOptSignal{toolCalls: 5, attachedTotal: 4, attachedUsed: 1}, true},
		{"nontrivial + low rank no attach", discoveryOptSignal{toolCalls: 5, attachedTotal: 0, queryHadLowRank: true}, true},
		{"writeCall single but high explore", discoveryOptSignal{toolCalls: 1, writeCalls: 1, grepReadCalls: 6}, true},
		{"attached 2 used 1 borderline 0.5 not less", discoveryOptSignal{toolCalls: 5, attachedTotal: 2, attachedUsed: 1}, false},
		{"attached 2 used 0", discoveryOptSignal{toolCalls: 5, attachedTotal: 2, attachedUsed: 0}, true},
	}
	for _, tt := range tests {
		if got := shouldOptimizeDiscovery(tt.sig); got != tt.want {
			t.Errorf("%s: got %v want %v sig=%+v", tt.name, got, tt.want, tt.sig)
		}
	}
}

func TestMaybePostTaskDiscoveryOptimization_DisabledIsNoop(t *testing.T) {
	// Discovery off => no panic, no debug needed (just ensure no panic).
	a := &Agent{config: nil}
	sig := discoveryOptSignal{toolCalls: 10, discoverMore: 1}
	// Should be no-op because discoveryConfigEnabled is false.
	a.maybePostTaskDiscoveryOptimization(sig, "do something")
}
