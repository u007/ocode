package remote

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodeDecodeFrameRoundTrip(t *testing.T) {
	p := SyncPayload{Files: []SyncFile{
		{Dir: SyncDirOcodeData, Name: "auth.profiles.json", Content: []byte(`{"default":{"anthropic":{"type":"api_key","key":"sk-test"}}}`)},
	}}
	frame, err := EncodeFrame("1.2.3", p)
	if err != nil {
		t.Fatalf("EncodeFrame: %v", err)
	}
	ver, got, err := DecodeAndVerifyFrame(frame)
	if err != nil {
		t.Fatalf("DecodeAndVerifyFrame: %v", err)
	}
	if ver != "1.2.3" {
		t.Errorf("version = %q, want 1.2.3", ver)
	}
	if len(got.Files) != 1 || got.Files[0].Name != "auth.profiles.json" {
		t.Errorf("payload round-trip mismatch: %+v", got)
	}
}

// --- Security (mandatory, one each) ---

func TestSecurityFramingRejection(t *testing.T) {
	cases := [][]byte{
		nil,
		[]byte(""),
		[]byte("not json"),
		[]byte(`{"version":"1.0.0"}`),   // missing sha256
		[]byte(`{"sha256":"deadbeef"}`), // missing version
		[]byte(`{"version":"1","sha256":"x","payload":123}`), // payload not bytes/string
	}
	for _, c := range cases {
		if _, _, err := DecodeAndVerifyFrame(c); err == nil {
			t.Errorf("DecodeAndVerifyFrame(%q): expected framing rejection, got nil error", c)
		}
	}
}

func TestSecurityOversizedPayloadRejected(t *testing.T) {
	huge := make([]byte, maxSyncPayloadBytes+1)
	if _, _, err := DecodeAndVerifyFrame(huge); err == nil {
		t.Fatal("expected oversized payload to be rejected")
	}
}

func TestSecurityChecksumRejection(t *testing.T) {
	p := SyncPayload{Files: []SyncFile{{Dir: SyncDirOcodeData, Name: "auth.profiles.json", Content: []byte("secret")}}}
	frame, err := EncodeFrame("1.0.0", p)
	if err != nil {
		t.Fatalf("EncodeFrame: %v", err)
	}

	// Tamper with the frame's payload while leaving its declared sha256
	// untouched — the classic checksum-bypass attempt.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(frame, &raw); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	evilPayload := SyncPayload{Files: []SyncFile{{Dir: SyncDirOcodeData, Name: "auth.profiles.json", Content: []byte("evil-payload")}}}
	evilPayloadJSON, err := json.Marshal(evilPayload)
	if err != nil {
		t.Fatal(err)
	}
	evilPayloadField, err := json.Marshal(evilPayloadJSON) // syncFrame.Payload is []byte -> json string
	if err != nil {
		t.Fatal(err)
	}
	raw["payload"] = evilPayloadField
	tampered, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := DecodeAndVerifyFrame(tampered); err == nil {
		t.Fatal("expected checksum mismatch to be rejected")
	}
}

func TestSecurityWritePayload0600AndTempRename(t *testing.T) {
	dataDir := t.TempDir()
	cfgDir := t.TempDir()
	restore := overrideSyncDirsForTest(t, dataDir, cfgDir)
	defer restore()

	p := SyncPayload{Files: []SyncFile{
		{Dir: SyncDirOcodeData, Name: "auth.profiles.json", Content: []byte(`{"k":"v"}`)},
		{Dir: SyncDirConfig, Name: "opencode.json", Content: []byte(`{"model":"x"}`)},
	}}
	if err := WritePayload(p); err != nil {
		t.Fatalf("WritePayload: %v", err)
	}

	authPath := filepath.Join(dataDir, "auth.profiles.json")
	info, err := os.Stat(authPath)
	if err != nil {
		t.Fatalf("stat %s: %v", authPath, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("auth.profiles.json mode = %o, want 0600", perm)
	}

	// No leftover .tmp-* files (temp+rename must not litter the dest dir).
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != "" && e.Name() != "auth.profiles.json" {
			t.Errorf("unexpected leftover file in dest dir: %s", e.Name())
		}
	}

	cfgPath := filepath.Join(cfgDir, "opencode.json")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("stat %s: %v", cfgPath, err)
	}
}

func TestSecurityPartialWriteLeavesNoDestFile(t *testing.T) {
	// Point the data dir at a path that cannot be created (a file where a
	// directory is expected), forcing writeFile0600 to fail before rename —
	// the destination path must not exist afterward.
	parent := t.TempDir()
	blocker := filepath.Join(parent, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(blocker, "nested") // blocker is a file, not a dir
	cfgDir := t.TempDir()
	restore := overrideSyncDirsForTest(t, dataDir, cfgDir)
	defer restore()

	p := SyncPayload{Files: []SyncFile{{Dir: SyncDirOcodeData, Name: "auth.profiles.json", Content: []byte("x")}}}
	if err := WritePayload(p); err == nil {
		t.Fatal("expected WritePayload to fail when the dest dir cannot be created")
	}
	// dataDir itself can't exist (its parent "blocker" is a file), so any
	// stat error under it — not just ENOENT — proves no destination file
	// was left behind.
	if _, err := os.Stat(filepath.Join(dataDir, "auth.profiles.json")); err == nil {
		t.Error("expected no destination file after a failed write, but stat succeeded")
	}
}

func TestSecuritySecretsAbsentFromExecArgv(t *testing.T) {
	// The credential sync payload must travel over Transport.ExecStdin's
	// stdin argument, never interpolated into the command string handed to
	// Exec/ExecStdin — that string is what ends up in this (and the
	// remote's) process argv/`ps` listing.
	ft := newFakeTransport()
	secret := "sk-super-secret-token-should-never-be-in-argv"

	if _, err := ft.ExecStdin("ocode remote-receive-config", strings.NewReader(secret)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ft.execStdinCalls) != 1 {
		t.Fatalf("expected 1 ExecStdin call, got %d", len(ft.execStdinCalls))
	}
	cmd := ft.execStdinCalls[0]
	if strings.Contains(cmd, secret) {
		t.Fatalf("secret leaked into command string: %q", cmd)
	}
	if !strings.Contains(string(ft.copyContent), secret) {
		t.Fatal("expected the secret to travel via stdin content, but it didn't arrive there")
	}
}

// overrideSyncDirsForTest redirects resolveSyncDir at fixed temp
// directories for the duration of a test, returning a restore func.
func overrideSyncDirsForTest(t *testing.T, dataDir, cfgDir string) func() {
	t.Helper()
	orig := resolveSyncDirFn
	resolveSyncDirFn = func(d SyncFileDir) (string, error) {
		switch d {
		case SyncDirOcodeData:
			return dataDir, nil
		case SyncDirConfig:
			return cfgDir, nil
		default:
			return "", errors.New("unknown sync dir")
		}
	}
	return func() { resolveSyncDirFn = orig }
}
