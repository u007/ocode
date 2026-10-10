package auth

import "testing"

// A base-store write must move the credential version, because live agents
// snapshot that version at build time and rebuild only when it changes
// (internal/server.reconcileProfileAgent). Before this, only the PROFILE store
// bumped it, so a key set through the base store — /connect in the TUI, or
// PUT /api/auth/connect/{provider} from the web/desktop Connectors settings —
// left every resident session on the stale key until an unrelated model or
// profile change happened to rebuild it.

func TestBaseCredentialWriteBumpsCredentialVersion(t *testing.T) {
	preserveProviderCredential(t, "opencode-go")

	before := CredentialVersion()
	if err := Set("opencode-go", Credential{Kind: KindAPIKey, Key: "sk-version-1"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := CredentialVersion(); got == before {
		t.Errorf("Set left CredentialVersion at %d; a resident agent would keep the old key", got)
	}

	after := CredentialVersion()
	if err := Remove("opencode-go"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := CredentialVersion(); got == after {
		t.Errorf("Remove left CredentialVersion at %d; removing a key must invalidate too", got)
	}
}

// The version is ONE counter covering both stores, so a base write and a
// profile write are each independently visible to a caller that snapshotted it.
func TestCredentialVersionCoversBothStores(t *testing.T) {
	preserveProviderCredential(t, "opencode-go")
	const profile = "versiontest"
	t.Cleanup(func() { _ = DeleteProfileCredentials(profile) })

	baseStart := CredentialVersion()
	if err := Set("opencode-go", Credential{Kind: KindAPIKey, Key: "sk-both"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	afterBase := CredentialVersion()
	if afterBase == baseStart {
		t.Fatalf("base write did not bump CredentialVersion")
	}

	if err := SetProfileCredential(profile, "opencode-go", Credential{Kind: KindAPIKey, Key: "sk-profile"}); err != nil {
		t.Fatalf("SetProfileCredential: %v", err)
	}
	if got := CredentialVersion(); got == afterBase {
		t.Errorf("profile write did not bump CredentialVersion (still %d)", got)
	}
}

// Two successive base writes must produce two distinct versions; a caller
// comparing snapshot-to-current would otherwise miss the second edit.
func TestCredentialVersionChangesOnEveryWrite(t *testing.T) {
	preserveProviderCredential(t, "opencode-go")

	var seen []int64
	for i := 0; i < 3; i++ {
		if err := Set("opencode-go", Credential{Kind: KindAPIKey, Key: "sk-iter"}); err != nil {
			t.Fatalf("Set %d: %v", i, err)
		}
		seen = append(seen, CredentialVersion())
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] == seen[i-1] {
			t.Fatalf("write %d did not change the version (still %d)", i, seen[i])
		}
	}
}
