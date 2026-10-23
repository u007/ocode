package remote

import (
	"os"
	"path/filepath"
	"testing"
)

func credPayload(auth, mcp, anti string) SyncPayload {
	return SyncPayload{Files: []SyncFile{
		{Dir: SyncDirConfig, Name: "ocodeconfig.json", Content: []byte("{}")},
		{Dir: SyncDirOpencodeData, Name: "auth.json", Content: []byte(auth)},
		{Dir: SyncDirOpencodeData, Name: "mcp-auth.json", Content: []byte(mcp)},
		{Dir: SyncDirHome, Name: ".config/opencode/antigravity-accounts.json", Content: []byte(anti)},
	}}
}

func namesOf(p SyncPayload) map[string]bool {
	m := map[string]bool{}
	for _, f := range p.Files {
		m[credentialKey(f)] = true
	}
	return m
}

func TestGateCredentialFiles_FirstPushSendsEverything(t *testing.T) {
	gated, sent := gateCredentialFiles(credPayload("a", "m", "g"), nil, false)
	if len(gated.Files) != 4 || len(sent) != 3 || len(gated.KeepHome) != 0 {
		t.Fatalf("first push: files=%d sent=%d keep=%v", len(gated.Files), len(sent), gated.KeepHome)
	}
}

func TestGateCredentialFiles_UnchangedCredentialsAreHeldBack(t *testing.T) {
	_, sent := gateCredentialFiles(credPayload("a", "m", "g"), nil, false)
	// auth.json changed locally; mcp-auth.json and antigravity did not.
	gated, sent2 := gateCredentialFiles(credPayload("a2", "m", "g"), sent, false)
	got := namesOf(gated)
	if !got["opencode-data/auth.json"] || got["opencode-data/mcp-auth.json"] || got["home/.config/opencode/antigravity-accounts.json"] {
		t.Fatalf("wrong files sent: %v", got)
	}
	if !got["config/ocodeconfig.json"] {
		t.Fatal("non-credential file must always ride along")
	}
	if len(sent2) != 1 {
		t.Fatalf("sent hashes = %v; want only auth.json", sent2)
	}
	if len(gated.KeepHome) != 1 || gated.KeepHome[0] != ".config/opencode/antigravity-accounts.json" {
		t.Fatalf("KeepHome = %v", gated.KeepHome)
	}
}

func TestGateCredentialFiles_ForceSendsAll(t *testing.T) {
	_, sent := gateCredentialFiles(credPayload("a", "m", "g"), nil, false)
	gated, _ := gateCredentialFiles(credPayload("a", "m", "g"), sent, true)
	if len(gated.Files) != 4 {
		t.Fatalf("force sent %d files; want 4", len(gated.Files))
	}
}

func TestWritePayload_KeptCredentialIsNotSweptAsStale(t *testing.T) {
	dest := t.TempDir()
	prev := resolveSyncDirFn
	resolveSyncDirFn = func(SyncFileDir) (string, error) { return dest, nil }
	t.Cleanup(func() { resolveSyncDirFn = prev })

	full := credPayload("a", "m", "g")
	if err := WritePayload(full); err != nil {
		t.Fatal(err)
	}
	gated, _ := gateCredentialFiles(full, map[string]string{
		"home/.config/opencode/antigravity-accounts.json": contentHash([]byte("g")),
	}, false)
	if err := WritePayload(gated); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".config", "opencode", "antigravity-accounts.json")); err != nil {
		t.Fatalf("held-back credential file was deleted by the sweep: %v", err)
	}
}

func TestHomeSync_CollectsCommandsAndPromptFiles(t *testing.T) {
	home := t.TempDir()
	writeHomeFile(t, home, ".config/opencode/commands/deploy.md", "cmd", 0o644)
	writeHomeFile(t, home, ".config/opencode/auto-permission-prompt.md", "p", 0o644)
	writeHomeFile(t, home, ".config/opencode/auto-permission-prompt.local.md", "l", 0o644)
	writeHomeFile(t, home, ".config/opencode/antigravity-accounts.json", "{}", 0o600)
	writeHomeFile(t, home, ".config/opencode/tui.json", "{}", 0o644)
	writeHomeFile(t, home, ".config/opencode/plugins/x/index.js", "x", 0o644)
	got := homeFilesByName(t, home)
	for _, want := range []string{".config/opencode/commands/deploy.md", ".config/opencode/auto-permission-prompt.md", ".config/opencode/auto-permission-prompt.local.md", ".config/opencode/antigravity-accounts.json"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing %s", want)
		}
	}
	for _, bad := range []string{".config/opencode/tui.json", ".config/opencode/plugins/x/index.js"} {
		if _, ok := got[bad]; ok {
			t.Errorf("collected non-allowlisted %s", bad)
		}
	}
}
