package redact

import (
	"sync"
	"testing"
)

func TestRegistryGetOrAssign(t *testing.T) {
	r := NewRegistry("a3f9c2")

	// First assignment returns index 1
	idx1 := r.GetOrAssign("hunter2", "password", "test")
	if idx1 != 1 {
		t.Errorf("first GetOrAssign = %d, want 1", idx1)
	}

	// Same value returns same index
	idx2 := r.GetOrAssign("hunter2", "password", "test")
	if idx2 != idx1 {
		t.Errorf("second GetOrAssign = %d, want %d (same as first)", idx2, idx1)
	}

	// Different value returns next index
	idx3 := r.GetOrAssign("secret-key", "api_key", "test")
	if idx3 != 2 {
		t.Errorf("third GetOrAssign = %d, want 2", idx3)
	}
}

func TestRegistryNormalizeValue(t *testing.T) {
	r := NewRegistry("a3f9c2")

	// Values with symmetric quotes should normalize to same
	idx1 := r.GetOrAssign("\"hunter2\"", "password", "test")
	idx2 := r.GetOrAssign("'hunter2'", "password", "test")
	idx3 := r.GetOrAssign("hunter2", "password", "test")

	if idx1 != idx2 || idx2 != idx3 {
		t.Errorf("normalized values got different indexes: %d, %d, %d", idx1, idx2, idx3)
	}

	// Whitespace trimming
	idx4 := r.GetOrAssign("  hunter2  ", "password", "test")
	if idx4 != idx1 {
		t.Errorf("trimmed value got different index: %d, want %d", idx4, idx1)
	}
}

func TestRegistryLookup(t *testing.T) {
	r := NewRegistry("a3f9c2")
	r.GetOrAssign("my-secret", "token", "test")

	entry, ok := r.Lookup(1)
	if !ok {
		t.Fatal("Lookup(1) returned not found")
	}
	if entry.Value != "my-secret" {
		t.Errorf("entry.Value = %q, want %q", entry.Value, "my-secret")
	}
	if entry.Kind != "token" {
		t.Errorf("entry.Kind = %q, want %q", entry.Kind, "token")
	}
	if entry.Source != "test" {
		t.Errorf("entry.Source = %q, want %q", entry.Source, "test")
	}

	// Non-existent index
	_, ok = r.Lookup(999)
	if ok {
		t.Error("Lookup(999) should return not found")
	}
}

func TestRegistryAll(t *testing.T) {
	r := NewRegistry("a3f9c2")
	r.GetOrAssign("secret-a", "kind-a", "src-a")
	r.GetOrAssign("secret-b", "kind-b", "src-b")
	r.GetOrAssign("secret-c", "kind-c", "src-c")

	entries := r.All()
	if len(entries) != 3 {
		t.Errorf("All() returned %d entries, want 3", len(entries))
	}

	// Should be sorted by index ascending
	if len(entries) >= 2 && entries[0].Value != "secret-a" {
		t.Errorf("first entry value = %q, want %q", entries[0].Value, "secret-a")
	}
}

func TestRegistryConcurrentSafety(t *testing.T) {
	r := NewRegistry("a3f9c2")
	var wg sync.WaitGroup

	// N goroutines all trying to register the same value
	const N = 100
	idxs := make([]int, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			idxs[i] = r.GetOrAssign("concurrent-secret", "test", "goroutine")
		}(i)
	}
	wg.Wait()

	// All should get the same index
	first := idxs[0]
	for i, idx := range idxs {
		if idx != first {
			t.Errorf("goroutine %d got index %d, want %d", i, idx, first)
		}
	}
}

func TestRegistryConcurrentUnique(t *testing.T) {
	r := NewRegistry("a3f9c2")
	var wg sync.WaitGroup

	// N goroutines each registering a unique value - no duplicate indexes
	const N = 50
	idxs := make([]int, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			val := "secret-" + string(rune('a'+i))
			idxs[i] = r.GetOrAssign(val, "test", "goroutine")
		}(i)
	}
	wg.Wait()

	// All indexes should be unique
	seen := make(map[int]bool)
	for _, idx := range idxs {
		if seen[idx] {
			t.Errorf("duplicate index %d", idx)
		}
		seen[idx] = true
	}
}

func TestRegistrySubstitute(t *testing.T) {
	r := NewRegistry("a3f9c2")
	r.GetOrAssign("hunter2", "password", "test")
	r.GetOrAssign("hunter2-prod", "password", "test")

	text := "my password is hunter2 and the prod one is hunter2-prod"
	substituted := r.Substitute(text)

	t.Logf("Substituted: %s", substituted)

	// Both should be replaced
	if substituted == text {
		t.Error("Substitute did not change text")
	}

	// The longer value (hunter2-prod) should have its own token
	// and hunter2 should have a different token
	if !containsToken(substituted, "a3f9c2") {
		t.Errorf("Substituted text does not contain any tokens: %s", substituted)
	}

	// Resolve back
	resolved := r.Resolve(substituted)
	if resolved != text {
		t.Errorf("Resolve mismatch:\n  original:  %q\n  after r-t: %q", text, resolved)
	}
}

func TestRegistryResolveForeignNonce(t *testing.T) {
	r := NewRegistry("a3f9c2")
	r.GetOrAssign("my-secret", "password", "test")

	text := "here is [[OCSEC:a3f9c2:1]] and [[OCSEC:deadbe:2]]"
	resolved := r.Resolve(text)

	// Our nonce token should be resolved, foreign left alone
	if !stringsContains(resolved, "my-secret") {
		t.Errorf("Resolve should replace our token: %s", resolved)
	}
	if !stringsContains(resolved, "[[OCSEC:deadbe:2]]") {
		t.Errorf("Resolve should leave foreign token: %s", resolved)
	}
}

func containsToken(s, nonce string) bool {
	// Check if string has at least one OCSEC token
	for i := 0; i < len(s)-len("[[OCSEC::]]"); i++ {
		if s[i] == '[' && i+8 < len(s) && s[i:i+8] == "[[OCSEC:" {
			return true
		}
	}
	return false
}

func stringsContains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
