package auth

import "testing"

// TestFailedReloadKeepsLastGoodCache: a reload that fails (here, a corrupt file
// written after a good load) must keep serving the last good credential. Fixing
// the file with a changed key must return the new key and bump the version.
func TestFailedReloadKeepsLastGoodCache(t *testing.T) {
	resetCrossProcessStores(t)
	mustSet(t, "alpha", "sk-old")
	if c, ok := Get("alpha"); !ok || c.Key != "sk-old" {
		t.Fatal("setup: good key not readable")
	}

	path := mustAuthPath(t)
	if err := writeFileAtomic(path, []byte(`{"alpha": {"type": "api", "key": `)); err != nil {
		t.Fatalf("write corrupt auth.json: %v", err)
	}
	if c, ok := Get("alpha"); !ok || c.Key != "sk-old" {
		t.Fatalf("corrupt file: Get ok=%v; want the last good key to keep serving", ok)
	}
	before := CredentialVersion()

	if err := writeFileAtomic(path, []byte(`{"alpha": {"type": "api", "key": "sk-new-value"}}`)); err != nil {
		t.Fatalf("fix auth.json: %v", err)
	}
	if c, ok := Get("alpha"); !ok || c.Key != "sk-new-value" {
		t.Fatalf("recovered file: Get ok=%v; want the new key", ok)
	}
	if got := CredentialVersion(); got <= before {
		t.Errorf("recovery did not bump CredentialVersion (%d -> %d)", before, got)
	}
}

// TestFailedProfileReloadKeepsLastGoodCache is the same contract for the profile
// store.
func TestFailedProfileReloadKeepsLastGoodCache(t *testing.T) {
	resetCrossProcessStores(t)
	if err := SetProfileCredential("work", "alpha", apiKeyCred("sk-old")); err != nil {
		t.Fatalf("SetProfileCredential: %v", err)
	}

	path := mustProfilePath(t)
	if err := writeFileAtomic(path, []byte(`{"work": {"alpha": `)); err != nil {
		t.Fatalf("write corrupt profile file: %v", err)
	}
	if c, ok := GetProfileCredential("work", "alpha"); !ok || c.Key != "sk-old" {
		t.Fatalf("corrupt profile file: Get ok=%v; want the last good key to keep serving", ok)
	}
	before := ProfileCredentialVersion()

	if err := writeFileAtomic(path, []byte(`{"work": {"alpha": {"type": "api", "key": "sk-new-value"}}}`)); err != nil {
		t.Fatalf("fix profile file: %v", err)
	}
	if c, ok := GetProfileCredential("work", "alpha"); !ok || c.Key != "sk-new-value" {
		t.Fatalf("recovered profile file: Get ok=%v; want the new key", ok)
	}
	if got := ProfileCredentialVersion(); got <= before {
		t.Errorf("profile recovery did not bump the version (%d -> %d)", before, got)
	}
}

// TestCredentialVersionSeesPeerChangeWithoutAGetter: CredentialVersion alone, with
// no Get in between, must notice another process's change to auth.json.
func TestCredentialVersionSeesPeerChangeWithoutAGetter(t *testing.T) {
	resetCrossProcessStores(t)
	mustSet(t, "alpha", "sk-alpha")
	before := CredentialVersion()

	mustPeerEditAuth(t, func(m map[string]Credential) { m["peer"] = apiKeyCred("sk-peer") })

	if got := CredentialVersion(); got <= before {
		t.Errorf("CredentialVersion did not see the peer change (%d -> %d)", before, got)
	}
}

// TestProfileCredentialVersionSeesPeerChangeWithoutAGetter is the same check for
// the profile store.
func TestProfileCredentialVersionSeesPeerChangeWithoutAGetter(t *testing.T) {
	resetCrossProcessStores(t)
	if err := SetProfileCredential("work", "alpha", apiKeyCred("sk-alpha")); err != nil {
		t.Fatalf("SetProfileCredential: %v", err)
	}
	before := CredentialVersion()

	mustPeerEditProfiles(t, func(m map[string]map[string]Credential) {
		m["work"]["peer"] = apiKeyCred("sk-peer")
	})

	if got := CredentialVersion(); got <= before {
		t.Errorf("CredentialVersion did not see the peer profile change (%d -> %d)", before, got)
	}
}
