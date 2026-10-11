package cdp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveSharedDaemonRereadsConfigAfterTokenEdit is the honest form of
// Review Focus #1 from the shared-daemon plan ("user edits their token in
// ~/.htrcli/config.json while ocode is running").
//
// An earlier draft of this test asserted it against EnsureHTRServe and PASSED
// against unmodified code — it was vacuous. It hand-built the SharedDaemon, so it
// never exercised the thing the finding was about: whether a long-lived holder
// keeps serving a stale token. It is rewritten here to drive the actual
// resolution function twice across a token rotation, which is the only place a
// cache could hide.
//
// This test is the REGRESSION GUARD for the finding being retired, not a
// reproduction of a live bug. ResolveSharedDaemon re-reads the file on every
// call (loadHTRcliConfig -> os.ReadFile), and every production entry point —
// the TUI's ensureSharedHTRDaemon, the settings start button's startManagedHTR —
// resolves immediately before use, so no SharedDaemon is ever held across an
// edit. See the "HTR shared daemon" entry in TODO.md.
func TestResolveSharedDaemonRereadsConfigAfterTokenEdit(t *testing.T) {
	home := isolateHTROwnerState(t)
	cfgPath := filepath.Join(home, ".htrcli", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeHTRcliConfig(t, cfgPath, "token-before-edit")

	in := HTRSharedInput{Shared: true, ConfigPath: cfgPath, Home: home}

	first := ResolveSharedDaemon(in)
	if first.Token != "token-before-edit" {
		t.Fatalf("first resolve = %q, want token-before-edit", first.Token)
	}

	// The user edits the token while ocode keeps running.
	writeHTRcliConfig(t, cfgPath, "token-after-edit")

	second := ResolveSharedDaemon(in)
	if second.Token != "token-after-edit" {
		t.Fatalf("second resolve = %q, want token-after-edit; ResolveSharedDaemon is caching "+
			"and a mid-session token edit would be ignored by every caller", second.Token)
	}
}

// TestResolveSharedDaemonRereadsConfigAfterPortEdit covers the same hazard for
// the coordinates rather than the credential: an edited `server` URL must move
// the port ocode probes. A stale port would send every probe to a dead address.
func TestResolveSharedDaemonRereadsConfigAfterPortEdit(t *testing.T) {
	home := isolateHTROwnerState(t)
	cfgPath := filepath.Join(home, ".htrcli", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeHTRcliConfig(t, cfgPath, "tok")

	in := HTRSharedInput{Shared: true, ConfigPath: cfgPath, Home: home}
	if got := ResolveSharedDaemon(in).Port; got != 3845 {
		t.Fatalf("first port = %d, want 3845", got)
	}

	writeHTRcliConfigOn(t, cfgPath, "tok", "http://127.0.0.1:49152")

	if got := ResolveSharedDaemon(in).Port; got != 49152 {
		t.Fatalf("second port = %d, want 49152; an edited server URL must be re-read", got)
	}
}

// TestResolveSharedDaemonHonoursOcodeTokenOverride guards the precedence the
// refresh must never break: browser.htr_token beats htrcli's own token. If a
// future "always re-read" change dropped this, the user's pinned credential
// would be silently discarded.
func TestResolveSharedDaemonHonoursOcodeTokenOverride(t *testing.T) {
	home := isolateHTROwnerState(t)
	cfgPath := filepath.Join(home, ".htrcli", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeHTRcliConfig(t, cfgPath, "config-file-token")

	got := ResolveSharedDaemon(HTRSharedInput{
		Shared: true, ConfigPath: cfgPath, Home: home, Token: "ocode-override",
	})
	if got.Token != "ocode-override" {
		t.Fatalf("token = %q, want ocode-override", got.Token)
	}
	if got.TokenSource != "ocode-config" {
		t.Fatalf("token source = %q, want ocode-config", got.TokenSource)
	}
}

func writeHTRcliConfig(t *testing.T, path, token string) {
	t.Helper()
	writeHTRcliConfigOn(t, path, token, "http://127.0.0.1:3845")
}

func writeHTRcliConfigOn(t *testing.T, path, token, server string) {
	t.Helper()
	body, err := json.Marshal(htrcliConfig{Server: server, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}
