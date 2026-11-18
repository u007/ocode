package cloudconnect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/encryption"
	"github.com/u007/ocode/internal/keychain"
)

// memorySecretStore is a deterministic stand-in for the legacy keychain blob
// so the migration test never depends on an OS secret-store binary. It records
// writes so the test can prove the old blob was not modified.
type memorySecretStore struct {
	data    map[string]string
	sets    int
	deletes int
}

func (s *memorySecretStore) key(service, account string) string { return service + "\x00" + account }

func (s *memorySecretStore) Get(service, account string) (string, error) {
	v, ok := s.data[s.key(service, account)]
	if !ok {
		return "", keychain.ErrNotFound
	}
	return v, nil
}
func (s *memorySecretStore) Set(service, account, secret string) error {
	s.data[s.key(service, account)] = secret
	s.sets++
	return nil
}
func (s *memorySecretStore) Delete(service, account string) error {
	delete(s.data, s.key(service, account))
	s.deletes++
	return nil
}

func fixedKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i + 1)
	}
	return k
}

// TestMigrateV1ToV2KeepsLegacyFields pins the non-destructive migration: v1
// metadata (root_path, max_cache_bytes) is preserved in the local-mode v2
// connections, refresh tokens land ONLY in the local credentials envelope,
// and both old secret sources are left byte-identical.
func TestMigrateV1ToV2KeepsLegacyFields(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()
	const password = "test-master-password"

	// Seed the legacy v1 metadata file.
	v1 := &config.StorageConnectorsFile{
		Version: "v1",
		Settings: map[string]config.ConnectorSettings{
			"gdrive-main": {
				Provider:      "google-drive",
				RootPath:      "/MyDrive",
				MaxCacheBytes: 12345,
				ProjectSlug:   "legacy-slug",
			},
		},
		MaxCacheBytes: 500 * 1024 * 1024,
	}
	if err := config.SaveStorageConnectors(root, v1); err != nil {
		t.Fatal(err)
	}

	// Seed the legacy keychain "_all" blob (the primary secret source).
	keychainSecrets := map[string]SecretBundle{
		"gdrive-main": {AccessKey: "access-1", RefreshToken: "refresh-1", ClientID: "cid-1"},
	}
	blob, err := json.Marshal(keychainSecrets)
	if err != nil {
		t.Fatal(err)
	}
	store := &memorySecretStore{data: map[string]string{}}
	if err := store.Set("ocode-cloud", "_all", string(blob)); err != nil {
		t.Fatal(err)
	}

	// Seed the legacy cloudconnect-secrets.enc file (the fallback secret source)
	// with a different connector, so the test proves both sources are merged.
	fileSecrets := map[string]SecretBundle{
		"dropbox-x": {AccessKey: "access-2", RefreshToken: "refresh-2"},
	}
	filePlain, _ := json.Marshal(fileSecrets)
	fileEnc, err := encryption.EncryptJSON(filePlain, password)
	if err != nil {
		t.Fatal(err)
	}
	secretsPath := filepath.Join(root, ".ocode", "cloudconnect-secrets.enc")
	if err := os.WriteFile(secretsPath, []byte(fileEnc), 0600); err != nil {
		t.Fatal(err)
	}

	key := fixedKey()
	oldDir, oldKey, oldStore, oldPW := cloudDataDir, resolveCloudKey, newLegacySecretStore, migrateMasterPassword
	cloudDataDir = func() (string, error) { return dataDir, nil }
	resolveCloudKey = func() ([]byte, bool, error) { return key, true, nil }
	newLegacySecretStore = func(string, string) keychain.Store { return store }
	migrateMasterPassword = func() string { return password }
	defer func() {
		cloudDataDir, resolveCloudKey, newLegacySecretStore, migrateMasterPassword = oldDir, oldKey, oldStore, oldPW
	}()

	beforeBlob := store.data[store.key("ocode-cloud", "_all")]
	beforeFile, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	store.sets, store.deletes = 0, 0

	if err := MigrateV1ToV2(root); err != nil {
		t.Fatalf("MigrateV1ToV2: %v", err)
	}

	// The v2 file is local mode with preserved metadata.
	f, err := config.LoadV2(root)
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != "v2" {
		t.Fatalf("Version = %q, want v2", f.Version)
	}
	if f.Central.ConnectorID != "kakiit" || f.Central.Mode != "local" {
		t.Fatalf("Central = %+v, want kakiit/local", f.Central)
	}
	conn, ok := f.Central.Connections["google-drive"]
	if !ok {
		t.Fatalf("google-drive connection missing: %+v", f.Central.Connections)
	}
	if conn.RootPath != "/MyDrive" || conn.MaxCacheBytes != 12345 {
		t.Fatalf("metadata not preserved: %+v", conn)
	}

	// Refresh tokens live ONLY in the local credentials envelope.
	plain, err := encryption.OpenWithKey(f.CredentialsEnvelope, key)
	if err != nil {
		t.Fatalf("open credentials envelope: %v", err)
	}
	var creds map[string]Credentials
	if err := json.Unmarshal(plain, &creds); err != nil {
		t.Fatal(err)
	}
	if creds["google-drive"].AccessToken != "access-1" || creds["google-drive"].RefreshToken != "refresh-1" {
		t.Fatalf("keychain credentials not migrated: %+v", creds["google-drive"])
	}
	if creds["dropbox-x"].AccessToken != "access-2" || creds["dropbox-x"].RefreshToken != "refresh-2" {
		t.Fatalf("file credentials not merged: %+v", creds["dropbox-x"])
	}
	if strings.Contains(string(plain), "refresh-1") == false {
		t.Fatal("expected refresh token inside the local credentials envelope")
	}

	// Neither legacy source was touched.
	if got := store.data[store.key("ocode-cloud", "_all")]; got != beforeBlob {
		t.Fatal("legacy keychain blob was modified")
	}
	if store.sets != 0 || store.deletes != 0 {
		t.Fatalf("legacy store was written: sets=%d deletes=%d", store.sets, store.deletes)
	}
	afterFile, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterFile) != string(beforeFile) {
		t.Fatal("legacy cloudconnect-secrets.enc was modified")
	}
}

// TestMigrateV1ToV2NoV1File verifies migration refuses a project with no
// legacy config rather than silently writing an empty v2 file.
func TestMigrateV1ToV2NoV1File(t *testing.T) {
	if err := MigrateV1ToV2(t.TempDir()); err == nil {
		t.Fatal("expected an error when no v1 file exists")
	}
}

// TestMigrateV1ToV2IsIdempotent pins that re-running migration against a file
// already on v2 leaves its hub-mode state intact. v1 and v2 share one file path
// (discriminated by "version"), so a second run must not clobber live state.
func TestMigrateV1ToV2IsIdempotent(t *testing.T) {
	root := t.TempDir()
	live := &config.StorageConnectorsFileV2{
		Version: "v2",
		Central: config.CentralConnector{
			ConnectorID: "kakiit",
			Mode:        "hub",
			Connections: map[string]config.ConnectionMeta{
				"dropbox": {Provider: "dropbox", ConnectionID: "hub-conn", Status: "hub"},
			},
		},
	}
	if err := config.SaveV2(root, live); err != nil {
		t.Fatal(err)
	}

	if err := MigrateV1ToV2(root); err != nil {
		t.Fatalf("MigrateV1ToV2 on a v2 file: %v", err)
	}
	got, err := config.LoadV2(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Central.Mode != "hub" {
		t.Fatalf("Mode = %q, want hub (v2 state was clobbered)", got.Central.Mode)
	}
	if c, ok := got.Central.Connections["dropbox"]; !ok || c.ConnectionID != "hub-conn" {
		t.Fatalf("hub connection lost: %+v", got.Central.Connections)
	}
}

// TestMigrateV1ToV2CorruptSecretsAborts pins that a corrupt ENCv1 secrets file
// aborts the migration instead of writing an empty envelope over the v1 file.
func TestMigrateV1ToV2CorruptSecretsAborts(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()
	if err := config.SaveStorageConnectors(root, &config.StorageConnectorsFile{
		Version:  "v1",
		Settings: map[string]config.ConnectorSettings{"k": {Provider: "dropbox", RootPath: "/x"}},
	}); err != nil {
		t.Fatal(err)
	}
	secretsPath := filepath.Join(root, ".ocode", "cloudconnect-secrets.enc")
	if err := os.MkdirAll(filepath.Dir(secretsPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretsPath, []byte("not-an-envelope"), 0600); err != nil {
		t.Fatal(err)
	}

	oldDir, oldKey, oldStore, oldPW := cloudDataDir, resolveCloudKey, newLegacySecretStore, migrateMasterPassword
	cloudDataDir = func() (string, error) { return dataDir, nil }
	resolveCloudKey = func() ([]byte, bool, error) { return fixedKey(), true, nil }
	newLegacySecretStore = func(string, string) keychain.Store {
		return &memorySecretStore{data: map[string]string{}} // Get -> ErrNotFound
	}
	migrateMasterPassword = func() string { return "pw" }
	defer func() {
		cloudDataDir, resolveCloudKey, newLegacySecretStore, migrateMasterPassword = oldDir, oldKey, oldStore, oldPW
	}()

	v1Path := filepath.Join(root, ".ocode", "storage-connectors.json")
	before, _ := os.ReadFile(v1Path)
	if err := MigrateV1ToV2(root); err == nil {
		t.Fatal("expected an error for a corrupt secrets file")
	}
	after, _ := os.ReadFile(v1Path)
	if string(after) != string(before) {
		t.Fatal("v1 file was overwritten despite the migration error")
	}
}

// TestMigrateV1ToV2PreservesStaticKeys pins that S3/GCS static-key fields in a
// legacy SecretBundle survive into the v2 local credentials envelope.
func TestMigrateV1ToV2PreservesStaticKeys(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()
	if err := config.SaveStorageConnectors(root, &config.StorageConnectorsFile{
		Version:  "v1",
		Settings: map[string]config.ConnectorSettings{"s3-main": {Provider: "s3", RootPath: "bucket"}},
	}); err != nil {
		t.Fatal(err)
	}
	store := &memorySecretStore{data: map[string]string{}}
	blob, _ := json.Marshal(map[string]SecretBundle{
		"s3-main": {
			AccessKey: "AKIA...", SecretKey: "secret", Region: "us-east-1",
			ServiceAccountJSON: `{"type":"service_account"}`, ClientID: "cid",
		},
	})
	if err := store.Set("ocode-cloud", "_all", string(blob)); err != nil {
		t.Fatal(err)
	}

	key := fixedKey()
	oldDir, oldKey, oldStore, oldPW := cloudDataDir, resolveCloudKey, newLegacySecretStore, migrateMasterPassword
	cloudDataDir = func() (string, error) { return dataDir, nil }
	resolveCloudKey = func() ([]byte, bool, error) { return key, true, nil }
	newLegacySecretStore = func(string, string) keychain.Store { return store }
	migrateMasterPassword = func() string { return "" }
	defer func() {
		cloudDataDir, resolveCloudKey, newLegacySecretStore, migrateMasterPassword = oldDir, oldKey, oldStore, oldPW
	}()

	if err := MigrateV1ToV2(root); err != nil {
		t.Fatal(err)
	}
	f, err := config.LoadV2(root)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := encryption.OpenWithKey(f.CredentialsEnvelope, key)
	if err != nil {
		t.Fatal(err)
	}
	var creds map[string]Credentials
	if err := json.Unmarshal(plain, &creds); err != nil {
		t.Fatal(err)
	}
	// Migration keys credentials by provider, not by the old connector id.
	got := creds["s3"]
	if got.SecretKey != "secret" || got.Region != "us-east-1" || got.ServiceAccountJSON == "" || got.ClientID != "cid" {
		t.Fatalf("static keys not preserved: %+v", got)
	}
}
