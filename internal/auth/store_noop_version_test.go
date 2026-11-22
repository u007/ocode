package auth

import "testing"

// staleBaseCache replaces this process's base cache with stale content while the
// recorded stamp still matches auth.json. That is the state a peer write leaves
// behind when it lands between this process's last load and a locked re-read: the
// stat check sees nothing new, so only the re-read reveals the difference.
func staleBaseCache(stale map[string]Credential) {
	storeMu.Lock()
	defer storeMu.Unlock()
	cache = stale
}

// staleProfileCache is staleBaseCache for the profile store.
func staleProfileCache(stale map[string]map[string]Credential) {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	profileCache = stale
}

// A Set that turns out to be a no-op, but whose re-read differs from what this
// process had cached, must still bump the credential version. Otherwise a cached
// LLM client keeps authenticating with the key it built from the stale cache.
func TestNoOpSetAdoptingPeerStateBumpsVersion(t *testing.T) {
	resetCrossProcessStores(t)
	mustSet(t, "openai", "sk-peer")
	staleBaseCache(map[string]Credential{"openai": apiKeyCred("sk-old")})

	before := CredentialVersion()
	if err := Set("openai", apiKeyCred("sk-peer")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if after := CredentialVersion(); after <= before {
		t.Errorf("version = %d after the no-op Set, want more than %d", after, before)
	}
	if got, _ := Get("openai"); got.Key != "sk-peer" {
		t.Error("the cache did not adopt the key on disk")
	}
}

// Remove of a provider this process had cached but disk no longer holds is a
// no-op write that still changes the state, so it bumps the version too.
func TestNoOpRemoveAdoptingPeerStateBumpsVersion(t *testing.T) {
	resetCrossProcessStores(t)
	mustSet(t, "openai", "sk-peer")
	staleBaseCache(map[string]Credential{
		"openai": apiKeyCred("sk-peer"),
		"gamma":  apiKeyCred("sk-gamma"),
	})

	before := CredentialVersion()
	if err := Remove("gamma"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if after := CredentialVersion(); after <= before {
		t.Errorf("version = %d after the no-op Remove, want more than %d", after, before)
	}
	if _, ok := Get("gamma"); ok {
		t.Error("the cache still holds a provider the disk does not")
	}
}

// The profile mutators take the same path: a no-op SetProfileCredential over a
// stale profile cache bumps the version.
func TestNoOpProfileSetAdoptingPeerStateBumpsVersion(t *testing.T) {
	resetCrossProcessStores(t)
	if err := SetProfileCredential("work", "openai", apiKeyCred("sk-peer")); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	staleProfileCache(map[string]map[string]Credential{
		"work": {"openai": apiKeyCred("sk-old")},
	})

	before := CredentialVersion()
	if err := SetProfileCredential("work", "openai", apiKeyCred("sk-peer")); err != nil {
		t.Fatalf("SetProfileCredential: %v", err)
	}
	if after := CredentialVersion(); after <= before {
		t.Errorf("version = %d after the no-op profile Set, want more than %d", after, before)
	}
	if got, _ := GetProfileCredential("work", "openai"); got.Key != "sk-peer" {
		t.Error("the profile cache did not adopt the key on disk")
	}
}
