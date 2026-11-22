package auth

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// countCredentialSaves counts OnCredentialsSaved fires for the rest of the test.
func countCredentialSaves(t *testing.T) *int {
	t.Helper()
	n := new(int)
	remove := OnCredentialsSaved.Add(func() { *n++ })
	t.Cleanup(remove)
	return n
}

// rawAuthBytes returns auth.json exactly as it is on disk.
func rawAuthBytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(mustAuthPath(t))
	if err != nil {
		t.Fatalf("read auth.json: %v", err)
	}
	return data
}

// rawTopLevelKeys returns the top-level keys of auth.json as written on disk.
func rawTopLevelKeys(t *testing.T) []string {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(rawAuthBytes(t), &top); err != nil {
		t.Fatalf("auth.json is not a JSON object: %v", err)
	}
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	return keys
}

// TestLegacyWrapperFileKeepsEveryProviderOnSet: the {"credentials": {…}} wrapper
// holds real providers. They must read, and a Set must rewrite the file flat
// with all of them kept, not replace them with one bogus "credentials" entry.
func TestLegacyWrapperFileKeepsEveryProviderOnSet(t *testing.T) {
	resetCrossProcessStores(t)
	wrapper := `{"credentials": {"alpha": {"type": "api", "key": "sk-alpha"}, "beta": {"type": "api", "key": "sk-beta"}}}`
	if err := writeFileAtomic(mustAuthPath(t), []byte(wrapper)); err != nil {
		t.Fatalf("write wrapper auth.json: %v", err)
	}

	for _, p := range []string{"alpha", "beta"} {
		if _, ok := Get(p); !ok {
			t.Errorf("wrapped provider %q is not readable", p)
		}
	}
	mustSet(t, "gamma", "sk-gamma")

	disk := onDiskAuth(t)
	for _, p := range []string{"alpha", "beta", "gamma"} {
		if _, ok := disk[p]; !ok {
			t.Errorf("Set dropped wrapped provider %q", p)
		}
	}
	if keys := rawTopLevelKeys(t); slices.Contains(keys, "credentials") {
		t.Error("auth.json still wraps credentials after a Set; it must be rewritten flat")
	}
}

// TestFlatProviderNamedCredentialsParsesFlat: a flat file whose real provider is
// literally named "credentials" is not the wrapper, because its value holds
// fields, not objects.
func TestFlatProviderNamedCredentialsParsesFlat(t *testing.T) {
	resetCrossProcessStores(t)
	if err := writeFileAtomic(mustAuthPath(t), []byte(`{"credentials": {"type": "api", "key": "sk-named"}}`)); err != nil {
		t.Fatalf("write flat auth.json: %v", err)
	}
	cred, ok := Get("credentials")
	if !ok {
		t.Fatal("flat provider named credentials was not read as a provider")
	}
	if cred.Kind != KindAPIKey {
		t.Errorf("flat provider named credentials has kind %q, want %q", cred.Kind, KindAPIKey)
	}
}

// TestZeroByteAuthFileIsEmptyStore: a zero-byte or whitespace-only auth.json is an
// empty store, not an unknown format, and a Set over it works.
func TestZeroByteAuthFileIsEmptyStore(t *testing.T) {
	cases := map[string][]byte{
		"zero-byte":  {},
		"whitespace": []byte(" \n\t "),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			resetCrossProcessStores(t)
			if err := os.WriteFile(mustAuthPath(t), content, 0o600); err != nil {
				t.Fatalf("write %s auth.json: %v", name, err)
			}
			if _, _, err := readAuthFile(mustAuthPath(t)); err != nil {
				t.Fatalf("%s auth.json is not an empty store: %v", name, err)
			}
			if got := List(); len(got) != 0 {
				t.Errorf("%s auth.json read as %d providers, want 0", name, len(got))
			}
			mustSet(t, "alpha", "sk-alpha")
			if _, ok := onDiskAuth(t)["alpha"]; !ok {
				t.Error("Set over an empty auth.json did not persist")
			}
		})
	}
}

// TestSetIdenticalCredentialWritesNothing: re-storing the stored credential is a
// no-op: no write (same file identity and bytes), no hook, no version bump.
func TestSetIdenticalCredentialWritesNothing(t *testing.T) {
	resetCrossProcessStores(t)
	mustSet(t, "alpha", "sk-same")

	path := mustAuthPath(t)
	infoBefore, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat auth.json: %v", err)
	}
	bytesBefore := rawAuthBytes(t)
	saves := countCredentialSaves(t)
	versionBefore := CredentialVersion()

	if err := Set("alpha", apiKeyCred("sk-same")); err != nil {
		t.Fatalf("Set identical: %v", err)
	}

	if *saves != 0 {
		t.Errorf("identical Set fired OnCredentialsSaved %d times, want 0", *saves)
	}
	if got := CredentialVersion(); got != versionBefore {
		t.Errorf("identical Set moved CredentialVersion %d -> %d", versionBefore, got)
	}
	infoAfter, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat auth.json after: %v", err)
	}
	if !os.SameFile(infoBefore, infoAfter) || !infoBefore.ModTime().Equal(infoAfter.ModTime()) {
		t.Error("identical Set rewrote auth.json")
	}
	if !bytes.Equal(rawAuthBytes(t), bytesBefore) {
		t.Error("identical Set changed auth.json bytes")
	}
}

// TestRemoveAbsentProviderWritesNothing: removing a provider that is not stored
// writes nothing, fires nothing and bumps nothing.
func TestRemoveAbsentProviderWritesNothing(t *testing.T) {
	resetCrossProcessStores(t)
	mustSet(t, "alpha", "sk-alpha")

	path := mustAuthPath(t)
	infoBefore, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat auth.json: %v", err)
	}
	saves := countCredentialSaves(t)
	versionBefore := CredentialVersion()

	if err := Remove("absent-provider"); err != nil {
		t.Fatalf("Remove absent: %v", err)
	}

	if *saves != 0 {
		t.Errorf("Remove of an absent provider fired OnCredentialsSaved %d times, want 0", *saves)
	}
	if got := CredentialVersion(); got != versionBefore {
		t.Errorf("Remove of an absent provider moved CredentialVersion %d -> %d", versionBefore, got)
	}
	infoAfter, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat auth.json after: %v", err)
	}
	if !os.SameFile(infoBefore, infoAfter) || !infoBefore.ModTime().Equal(infoAfter.ModTime()) {
		t.Error("Remove of an absent provider rewrote auth.json")
	}
}

// TestProfileNoOpWritesNothing: the profile store follows the same rule for an
// identical Set and for removing what is not stored.
func TestProfileNoOpWritesNothing(t *testing.T) {
	resetCrossProcessStores(t)
	if err := SetProfileCredential("work", "alpha", apiKeyCred("sk-a")); err != nil {
		t.Fatalf("SetProfileCredential: %v", err)
	}

	path := mustProfilePath(t)
	infoBefore, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat profile file: %v", err)
	}
	saves := countCredentialSaves(t)
	versionBefore := CredentialVersion()

	steps := map[string]func() error{
		"identical SetProfileCredential":          func() error { return SetProfileCredential("work", "alpha", apiKeyCred("sk-a")) },
		"RemoveProfileCredential absent provider": func() error { return RemoveProfileCredential("work", "absent") },
		"RemoveProfileCredential absent profile":  func() error { return RemoveProfileCredential("nobody", "alpha") },
		"DeleteProfileCredentials absent profile": func() error { return DeleteProfileCredentials("ghost") },
	}
	for name, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}

	if *saves != 0 {
		t.Errorf("no-op profile writes fired OnCredentialsSaved %d times, want 0", *saves)
	}
	if got := CredentialVersion(); got != versionBefore {
		t.Errorf("no-op profile writes moved CredentialVersion %d -> %d", versionBefore, got)
	}
	infoAfter, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat profile file after: %v", err)
	}
	if !os.SameFile(infoBefore, infoAfter) || !infoBefore.ModTime().Equal(infoAfter.ModTime()) {
		t.Error("a no-op profile write rewrote the profile file")
	}
}
