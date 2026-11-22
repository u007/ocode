package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// Cross-process tests. A "peer" is another ocode process: it rewrites the
// shared file under the same lock and never touches this process's cache, so
// the stale-cache window these tests exercise is the real one. No test prints
// a credential value; failures name providers and profiles only.

func apiKeyCred(key string) Credential {
	return Credential{Kind: KindAPIKey, Key: key}
}

// resetCrossProcessStores points both stores at a fresh home and drops their
// process-global state, before and after the test.
func resetCrossProcessStores(t *testing.T) {
	t.Helper()
	setHomeTree(t, t.TempDir())
	resetAuthCacheForTest()
	resetProfileCacheForTest()
	t.Cleanup(func() {
		resetAuthCacheForTest()
		resetProfileCacheForTest()
	})
}

func resetAuthCacheForTest() {
	storeMu.Lock()
	defer storeMu.Unlock()
	cache = nil
	cacheLoaded = false
	cacheInfo = nil
}

func resetProfileCacheForTest() {
	profileStoreMu.Lock()
	defer profileStoreMu.Unlock()
	profileCache = nil
	profileLoaded = false
	profileCacheInfo = nil
}

func mustAuthPath(t *testing.T) string {
	t.Helper()
	p, err := AuthPath()
	if err != nil {
		t.Fatalf("AuthPath: %v", err)
	}
	return p
}

func mustProfilePath(t *testing.T) string {
	t.Helper()
	p, err := ProfileAuthPath()
	if err != nil {
		t.Fatalf("ProfileAuthPath: %v", err)
	}
	return p
}

func mustSet(t *testing.T, provider, key string) {
	t.Helper()
	if err := Set(provider, apiKeyCred(key)); err != nil {
		t.Fatalf("Set(%s): %v", provider, err)
	}
}

func mustRemove(t *testing.T, provider string) {
	t.Helper()
	if err := Remove(provider); err != nil {
		t.Fatalf("Remove(%s): %v", provider, err)
	}
}

// onDiskAuth returns auth.json as it is right now, bypassing the cache.
func onDiskAuth(t *testing.T) map[string]Credential {
	t.Helper()
	m, _, err := readAuthFile(mustAuthPath(t))
	if err != nil {
		t.Fatalf("read auth.json: %v", err)
	}
	return m
}

// onDiskProfiles returns auth.profiles.json as it is right now.
func onDiskProfiles(t *testing.T) map[string]map[string]Credential {
	t.Helper()
	m, _, err := readProfileFile(mustProfilePath(t))
	if err != nil {
		t.Fatalf("read auth.profiles.json: %v", err)
	}
	return m
}

// peerEditAuth plays another process: under the shared lock it applies edit to
// the file's current contents and writes them back atomically. It returns an
// error rather than failing the test, so goroutines can call it.
func peerEditAuth(edit func(map[string]Credential)) error {
	path, err := AuthPath()
	if err != nil {
		return err
	}
	return withAuthFileLock(path, func() error {
		m, _, err := readAuthFile(path)
		if err != nil {
			return err
		}
		edit(m)
		data, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return err
		}
		return writeFileAtomic(path, data)
	})
}

func mustPeerEditAuth(t *testing.T, edit func(map[string]Credential)) {
	t.Helper()
	if err := peerEditAuth(edit); err != nil {
		t.Fatalf("peer edit of auth.json: %v", err)
	}
}

// peerEditProfiles is peerEditAuth for auth.profiles.json.
func peerEditProfiles(edit func(map[string]map[string]Credential)) error {
	path, err := ProfileAuthPath()
	if err != nil {
		return err
	}
	return withAuthFileLock(path, func() error {
		m, _, err := readProfileFile(path)
		if err != nil {
			return err
		}
		edit(m)
		data, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return err
		}
		return writeFileAtomic(path, append(data, '\n'))
	})
}

func mustPeerEditProfiles(t *testing.T, edit func(map[string]map[string]Credential)) {
	t.Helper()
	if err := peerEditProfiles(edit); err != nil {
		t.Fatalf("peer edit of auth.profiles.json: %v", err)
	}
}

// TestSetKeepsKeyAddedByPeerSinceLoad is the core clobber case: this process
// loads, a peer saves a key, and this process then saves a different key. The
// peer's key must survive, on disk and in this process's view.
func TestSetKeepsKeyAddedByPeerSinceLoad(t *testing.T) {
	resetCrossProcessStores(t)
	mustSet(t, "alpha", "sk-alpha")

	mustPeerEditAuth(t, func(m map[string]Credential) {
		m["peer-x"] = apiKeyCred("sk-peer")
	})
	mustSet(t, "beta", "sk-beta")

	disk := onDiskAuth(t)
	view := List()
	for _, p := range []string{"alpha", "beta", "peer-x"} {
		if _, ok := disk[p]; !ok {
			t.Errorf("auth.json lost provider %q", p)
		}
		if _, ok := view[p]; !ok {
			t.Errorf("this process no longer sees provider %q", p)
		}
	}
}

// TestSetKeepsKeyRemovedByPeer: a peer removes a key after this process
// loaded. A later Set from this process must not write that key back.
func TestSetKeepsKeyRemovedByPeer(t *testing.T) {
	resetCrossProcessStores(t)
	mustSet(t, "alpha", "sk-alpha")
	mustSet(t, "beta", "sk-beta")

	mustPeerEditAuth(t, func(m map[string]Credential) {
		delete(m, "alpha")
	})
	mustSet(t, "gamma", "sk-gamma")

	disk := onDiskAuth(t)
	if _, ok := disk["alpha"]; ok {
		t.Errorf("provider %q removed by a peer was written back", "alpha")
	}
	for _, p := range []string{"beta", "gamma"} {
		if _, ok := disk[p]; !ok {
			t.Errorf("auth.json lost provider %q", p)
		}
	}
	if _, ok := List()["alpha"]; ok {
		t.Errorf("this process serves provider %q that a peer removed", "alpha")
	}
}

// TestGetSeesPeerKeyAndBumpsVersionOnlyOnChange: Get reloads when a peer
// changed the file, bumping the version because the credentials changed. A
// reload with identical content must not bump it.
func TestGetSeesPeerKeyAndBumpsVersionOnlyOnChange(t *testing.T) {
	resetCrossProcessStores(t)
	mustSet(t, "alpha", "sk-alpha")

	before := CredentialVersion()
	mustPeerEditAuth(t, func(m map[string]Credential) {
		m["peer-x"] = apiKeyCred("sk-peer")
	})
	if _, ok := Get("peer-x"); !ok {
		t.Fatal("Get did not see a provider a peer added")
	}
	afterAdd := CredentialVersion()
	if afterAdd <= before {
		t.Errorf("CredentialVersion did not move when a peer added a key (%d)", afterAdd)
	}

	// A rewrite with the same content is a new file, so the stamp changes and
	// a reload runs, but nothing changed and the version must stay put.
	mustPeerEditAuth(t, func(map[string]Credential) {})
	if _, ok := Get("alpha"); !ok {
		t.Fatal("Get lost a provider after an identical rewrite")
	}
	if got := CredentialVersion(); got != afterAdd {
		t.Errorf("identical reload moved CredentialVersion %d -> %d", afterAdd, got)
	}
}

// TestRemoveSurvivesReload: a removed provider stays removed after a reload
// triggered by a peer's unrelated write, and after a later Set.
func TestRemoveSurvivesReload(t *testing.T) {
	resetCrossProcessStores(t)
	mustSet(t, "alpha", "sk-alpha")
	mustSet(t, "beta", "sk-beta")
	mustRemove(t, "beta")

	mustPeerEditAuth(t, func(m map[string]Credential) {
		m["peer-x"] = apiKeyCred("sk-peer")
	})
	if _, ok := Get("beta"); ok {
		t.Error("removed provider came back after a reload")
	}
	mustSet(t, "gamma", "sk-gamma")
	if _, ok := onDiskAuth(t)["beta"]; ok {
		t.Error("removed provider was written back by a later Set")
	}
}

// TestCorruptAuthFileFailsSetAndIsUntouched: a file that exists but does not
// parse must fail the write and keep its bytes, whether this process had
// loaded it or not.
func TestCorruptAuthFileFailsSetAndIsUntouched(t *testing.T) {
	resetCrossProcessStores(t)
	mustSet(t, "alpha", "sk-alpha")

	path := mustAuthPath(t)
	garbage := []byte(`{"alpha": {"type": "api", "key": `)
	if err := writeFileAtomic(path, garbage); err != nil {
		t.Fatalf("write corrupt auth.json: %v", err)
	}

	if err := Set("beta", apiKeyCred("sk-beta")); err == nil {
		t.Error("Set over a corrupt auth.json succeeded")
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, garbage) {
		t.Error("Set rewrote a corrupt auth.json")
	}

	resetAuthCacheForTest()
	if err := Set("gamma", apiKeyCred("sk-gamma")); err == nil {
		t.Error("Set in a fresh view over a corrupt auth.json succeeded")
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, garbage) {
		t.Error("fresh-view Set rewrote a corrupt auth.json")
	}
}

// TestConcurrentWritersKeepEveryKey runs in-process writers and peer writers at
// once. Every key from both sides must survive.
func TestConcurrentWritersKeepEveryKey(t *testing.T) {
	resetCrossProcessStores(t)
	mustSet(t, "seed", "sk-seed")

	const groups, perWriter = 2, 8
	var wg sync.WaitGroup
	errs := make(chan error, 2*groups*perWriter)
	for g := 0; g < groups; g++ {
		wg.Add(2)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				name := fmt.Sprintf("local-%d-%d", g, i)
				if err := Set(name, apiKeyCred("sk-"+name)); err != nil {
					errs <- fmt.Errorf("Set(%s): %w", name, err)
				}
			}
		}(g)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				name := fmt.Sprintf("peer-%d-%d", g, i)
				if err := peerEditAuth(func(m map[string]Credential) {
					m[name] = apiKeyCred("sk-" + name)
				}); err != nil {
					errs <- fmt.Errorf("peer write %s: %w", name, err)
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	disk := onDiskAuth(t)
	if _, ok := disk["seed"]; !ok {
		t.Error("auth.json lost the seed provider")
	}
	for g := 0; g < groups; g++ {
		for i := 0; i < perWriter; i++ {
			for _, name := range []string{fmt.Sprintf("local-%d-%d", g, i), fmt.Sprintf("peer-%d-%d", g, i)} {
				if _, ok := disk[name]; !ok {
					t.Errorf("auth.json lost provider %q", name)
				}
			}
		}
	}
}

// TestAuthFileModeAndNoTempFileAfterWrite: the first load seeds the file at
// 0600, later writes keep 0600, and no temp file is left behind.
func TestAuthFileModeAndNoTempFileAfterWrite(t *testing.T) {
	resetCrossProcessStores(t)
	path := mustAuthPath(t)

	if _, ok := Get("alpha"); ok {
		t.Fatal("empty store returned a provider")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("first load did not seed auth.json: %v", err)
	}
	mustSet(t, "alpha", "sk-alpha")
	mustPeerEditAuth(t, func(m map[string]Credential) { m["peer-x"] = apiKeyCred("sk-peer") })
	mustSet(t, "beta", "sk-beta")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat auth.json: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("auth.json mode = %o, want 0600", info.Mode().Perm())
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("auth.json.tmp left behind (stat err: %v)", err)
	}
}

// TestLegacyCredentialsSurviveLoadAndRemovalSticks covers the opencode legacy
// file when it is a different file from auth.json. Its credentials stay
// readable and are folded in on the first write, and a Remove sticks even
// though the legacy file still holds the provider.
func TestLegacyCredentialsSurviveLoadAndRemovalSticks(t *testing.T) {
	const legacyProvider = "legacy-p"
	legacyFile := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		p := filepath.Join(dir, "auth.json")
		if err := os.WriteFile(p, []byte(`{"`+legacyProvider+`": {"type": "api", "key": "sk-legacy"}}`), 0o600); err != nil {
			t.Fatalf("write legacy file: %v", err)
		}
		return p
	}
	useLegacy := func(t *testing.T, p string) {
		t.Helper()
		legacyAuthPath = func() string { return p }
		t.Cleanup(func() { legacyAuthPath = opencodeLegacyAuthPath })
	}

	t.Run("readable then folded on first write", func(t *testing.T) {
		resetCrossProcessStores(t)
		useLegacy(t, legacyFile(t))
		if err := writeFileAtomic(mustAuthPath(t), []byte(`{"alpha": {"type": "api", "key": "sk-alpha"}}`)); err != nil {
			t.Fatalf("seed auth.json: %v", err)
		}

		if _, ok := Get(legacyProvider); !ok {
			t.Fatal("legacy credential is not readable")
		}
		mustSet(t, "beta", "sk-beta")
		disk := onDiskAuth(t)
		for _, p := range []string{"alpha", "beta", legacyProvider} {
			if _, ok := disk[p]; !ok {
				t.Errorf("auth.json lacks provider %q after the first write", p)
			}
		}

		mustRemove(t, legacyProvider)
		mustPeerEditAuth(t, func(m map[string]Credential) { m["peer-x"] = apiKeyCred("sk-peer") })
		if _, ok := Get(legacyProvider); ok {
			t.Error("removed legacy provider came back after a reload")
		}
		if _, ok := onDiskAuth(t)[legacyProvider]; ok {
			t.Error("removed legacy provider is on disk")
		}
	})

	t.Run("removed before any write stays removed", func(t *testing.T) {
		resetCrossProcessStores(t)
		useLegacy(t, legacyFile(t))
		if err := writeFileAtomic(mustAuthPath(t), []byte(`{"alpha": {"type": "api", "key": "sk-alpha"}}`)); err != nil {
			t.Fatalf("seed auth.json: %v", err)
		}

		if _, ok := Get(legacyProvider); !ok {
			t.Fatal("legacy credential is not readable")
		}
		mustRemove(t, legacyProvider)
		if _, ok := Get(legacyProvider); ok {
			t.Error("legacy provider still served after Remove")
		}
		mustSet(t, "beta", "sk-beta")
		if _, ok := onDiskAuth(t)[legacyProvider]; ok {
			t.Error("legacy provider written back by the next Set")
		}
	})
}

// TestProfileSetKeepsKeyAddedByPeerSinceLoad is TestSetKeepsKeyAddedByPeerSinceLoad
// for the profile sidecar.
func TestProfileSetKeepsKeyAddedByPeerSinceLoad(t *testing.T) {
	resetCrossProcessStores(t)
	if err := SetProfileCredential("work", "alpha", apiKeyCred("sk-alpha")); err != nil {
		t.Fatalf("SetProfileCredential: %v", err)
	}
	mustPeerEditProfiles(t, func(m map[string]map[string]Credential) {
		m["home"] = map[string]Credential{"beta": apiKeyCred("sk-beta")}
	})
	if err := SetProfileCredential("work", "gamma", apiKeyCred("sk-gamma")); err != nil {
		t.Fatalf("SetProfileCredential: %v", err)
	}

	disk := onDiskProfiles(t)
	if _, ok := disk["home"]["beta"]; !ok {
		t.Error("profile sidecar lost the peer's profile")
	}
	if _, ok := disk["work"]["alpha"]; !ok {
		t.Error("profile sidecar lost work/alpha")
	}
	if _, ok := disk["work"]["gamma"]; !ok {
		t.Error("profile sidecar lost work/gamma")
	}
	names := ListProfileNames()
	if len(names) != 2 || names[0] != "home" || names[1] != "work" {
		t.Errorf("ListProfileNames = %v, want [home work]", names)
	}
}

// TestProfileSetKeepsKeyRemovedByPeer is TestSetKeepsKeyRemovedByPeer for profiles.
func TestProfileSetKeepsKeyRemovedByPeer(t *testing.T) {
	resetCrossProcessStores(t)
	if err := SetProfileCredential("work", "alpha", apiKeyCred("sk-alpha")); err != nil {
		t.Fatalf("SetProfileCredential: %v", err)
	}
	if err := SetProfileCredential("work", "beta", apiKeyCred("sk-beta")); err != nil {
		t.Fatalf("SetProfileCredential: %v", err)
	}
	mustPeerEditProfiles(t, func(m map[string]map[string]Credential) {
		delete(m["work"], "alpha")
	})
	if err := SetProfileCredential("work", "gamma", apiKeyCred("sk-gamma")); err != nil {
		t.Fatalf("SetProfileCredential: %v", err)
	}

	disk := onDiskProfiles(t)
	if _, ok := disk["work"]["alpha"]; ok {
		t.Error("profile provider removed by a peer was written back")
	}
	if _, ok := disk["work"]["beta"]; !ok {
		t.Error("profile sidecar lost work/beta")
	}
}

// TestProfileGetSeesPeerKeyAndBumpsVersionOnlyOnChange is the Get and version
// case for profiles.
func TestProfileGetSeesPeerKeyAndBumpsVersionOnlyOnChange(t *testing.T) {
	resetCrossProcessStores(t)
	if err := SetProfileCredential("work", "alpha", apiKeyCred("sk-alpha")); err != nil {
		t.Fatalf("SetProfileCredential: %v", err)
	}

	before := CredentialVersion()
	mustPeerEditProfiles(t, func(m map[string]map[string]Credential) {
		m["home"] = map[string]Credential{"beta": apiKeyCred("sk-beta")}
	})
	if _, ok := GetProfileCredential("home", "beta"); !ok {
		t.Fatal("GetProfileCredential did not see a profile a peer added")
	}
	afterAdd := CredentialVersion()
	if afterAdd <= before {
		t.Errorf("CredentialVersion did not move when a peer added a profile key (%d)", afterAdd)
	}

	mustPeerEditProfiles(t, func(map[string]map[string]Credential) {})
	if _, ok := GetProfileCredential("work", "alpha"); !ok {
		t.Fatal("GetProfileCredential lost a key after an identical rewrite")
	}
	if got := CredentialVersion(); got != afterAdd {
		t.Errorf("identical profile reload moved CredentialVersion %d -> %d", afterAdd, got)
	}
}

// TestProfileRemoveSurvivesReload is TestRemoveSurvivesReload for profiles.
func TestProfileRemoveSurvivesReload(t *testing.T) {
	resetCrossProcessStores(t)
	for _, k := range []string{"alpha", "beta"} {
		if err := SetProfileCredential("work", k, apiKeyCred("sk-"+k)); err != nil {
			t.Fatalf("SetProfileCredential: %v", err)
		}
	}
	if err := RemoveProfileCredential("work", "beta"); err != nil {
		t.Fatalf("RemoveProfileCredential: %v", err)
	}
	mustPeerEditProfiles(t, func(m map[string]map[string]Credential) {
		m["home"] = map[string]Credential{"gamma": apiKeyCred("sk-gamma")}
	})
	if _, ok := GetProfileCredential("work", "beta"); ok {
		t.Error("removed profile provider came back after a reload")
	}
}

// TestProfileCorruptFileFailsWriteAndIsUntouched is the corrupt-file case for
// the profile sidecar.
func TestProfileCorruptFileFailsWriteAndIsUntouched(t *testing.T) {
	resetCrossProcessStores(t)
	if err := SetProfileCredential("work", "alpha", apiKeyCred("sk-alpha")); err != nil {
		t.Fatalf("SetProfileCredential: %v", err)
	}
	path := mustProfilePath(t)
	garbage := []byte(`{"work": {"alpha": `)
	if err := writeFileAtomic(path, garbage); err != nil {
		t.Fatalf("write corrupt profile file: %v", err)
	}

	if err := SetProfileCredential("work", "beta", apiKeyCred("sk-beta")); err == nil {
		t.Error("SetProfileCredential over a corrupt file succeeded")
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, garbage) {
		t.Error("SetProfileCredential rewrote a corrupt profile file")
	}
}

// TestProfileFileModeAndNoTempFileAfterWrite is the mode and temp-file case for profiles.
func TestProfileFileModeAndNoTempFileAfterWrite(t *testing.T) {
	resetCrossProcessStores(t)
	if err := SetProfileCredential("work", "alpha", apiKeyCred("sk-alpha")); err != nil {
		t.Fatalf("SetProfileCredential: %v", err)
	}
	path := mustProfilePath(t)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat profile file: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("profile file mode = %o, want 0600", info.Mode().Perm())
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("profile temp file left behind (stat err: %v)", err)
	}
}

// TestProfileConcurrentWritersKeepEveryKey is the two-writer hammer for profiles.
func TestProfileConcurrentWritersKeepEveryKey(t *testing.T) {
	resetCrossProcessStores(t)
	if err := SetProfileCredential("work", "seed", apiKeyCred("sk-seed")); err != nil {
		t.Fatalf("SetProfileCredential: %v", err)
	}

	const groups, perWriter = 2, 8
	var wg sync.WaitGroup
	errs := make(chan error, 2*groups*perWriter)
	for g := 0; g < groups; g++ {
		wg.Add(2)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				name := fmt.Sprintf("local-%d-%d", g, i)
				if err := SetProfileCredential("work", name, apiKeyCred("sk-"+name)); err != nil {
					errs <- fmt.Errorf("SetProfileCredential(%s): %w", name, err)
				}
			}
		}(g)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				name := fmt.Sprintf("peer-%d-%d", g, i)
				if err := peerEditProfiles(func(m map[string]map[string]Credential) {
					if m["work"] == nil {
						m["work"] = map[string]Credential{}
					}
					m["work"][name] = apiKeyCred("sk-" + name)
				}); err != nil {
					errs <- fmt.Errorf("peer profile write %s: %w", name, err)
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	work := onDiskProfiles(t)["work"]
	if _, ok := work["seed"]; !ok {
		t.Error("profile sidecar lost the seed provider")
	}
	for g := 0; g < groups; g++ {
		for i := 0; i < perWriter; i++ {
			for _, name := range []string{fmt.Sprintf("local-%d-%d", g, i), fmt.Sprintf("peer-%d-%d", g, i)} {
				if _, ok := work[name]; !ok {
					t.Errorf("profile sidecar lost provider %q", name)
				}
			}
		}
	}
}
