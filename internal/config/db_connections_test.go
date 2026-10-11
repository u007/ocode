package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeDBTestConfig(t *testing.T, home, body string) string {
	t.Helper()
	dir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ocodeconfig.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// addDB verifies against the currently saved set, as the handler does, then adds.
func addDB(t *testing.T, conn DBConnection) error {
	t.Helper()
	cur, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	return AddDBConnection(conn, cur.DB.Connections)
}

func TestAddDBConnectionPersistsAndKeepsOtherKeys(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	path := writeDBTestConfig(t, home, `{"editor":"vim"}`)

	if err := addDB(t, DBConnection{Name: "prod", Driver: DBDriverPostgres, EncryptedURL: "ENCv1:argon2:a:b:c"}); err != nil {
		t.Fatalf("AddDBConnection: %v", err)
	}
	if err := addDB(t, DBConnection{Name: "stage", Driver: DBDriverPostgres, EncryptedURL: "ENCv1:argon2:d:e:f"}); err != nil {
		t.Fatalf("AddDBConnection second: %v", err)
	}

	cfg, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	if len(cfg.DB.Connections) != 2 || cfg.DB.Connections[0].Name != "prod" || cfg.DB.Connections[1].Name != "stage" {
		t.Fatalf("reloaded connections = %+v", cfg.DB.Connections)
	}
	if cfg.Editor != "vim" {
		t.Fatalf("editor = %q, want vim (save clobbered another key)", cfg.Editor)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		DB struct {
			Connections []struct {
				URL string `json:"url"`
			} `json:"connections"`
		} `json:"db"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse saved file: %v", err)
	}
	if len(raw.DB.Connections) != 2 || raw.DB.Connections[0].URL != "ENCv1:argon2:a:b:c" {
		t.Fatalf("saved db section = %+v", raw.DB)
	}
}

func TestAddDBConnectionRejectsDuplicateAndBadInput(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	writeDBTestConfig(t, home, `{}`)

	ok := DBConnection{Name: "prod", Driver: DBDriverPostgres, EncryptedURL: "ENCv1:argon2:a:b:c"}
	if err := addDB(t, ok); err != nil {
		t.Fatalf("AddDBConnection: %v", err)
	}
	if err := addDB(t, ok); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if err := addDB(t, DBConnection{Name: "x", Driver: "mysql", EncryptedURL: "ENCv1:argon2:a:b:c"}); err == nil {
		t.Fatal("unsupported driver accepted")
	}
	if err := addDB(t, DBConnection{Name: "", Driver: DBDriverPostgres, EncryptedURL: "ENCv1:argon2:a:b:c"}); err == nil {
		t.Fatal("empty name accepted")
	}
	if err := addDB(t, DBConnection{Name: "y", Driver: DBDriverPostgres}); err == nil {
		t.Fatal("empty encrypted url accepted")
	}
}

// A verified set that no longer matches the saved set must not write: the
// caller checked the password against a set that has since changed.
func TestAddDBConnectionRefusesStaleVerifiedSet(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	writeDBTestConfig(t, home, `{}`)

	if err := addDB(t, DBConnection{Name: "prod", Driver: DBDriverPostgres, EncryptedURL: "ENCv1:argon2:a:b:c"}); err != nil {
		t.Fatalf("AddDBConnection prod: %v", err)
	}
	stale := []DBConnection{} // verified while nothing was saved

	err := AddDBConnection(DBConnection{Name: "stage", Driver: DBDriverPostgres, EncryptedURL: "ENCv1:argon2:d:e:f"}, stale)
	if !errors.Is(err, ErrDBConnectionsChanged) {
		t.Fatalf("stale add err = %v, want ErrDBConnectionsChanged", err)
	}
	cfg, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	if len(cfg.DB.Connections) != 1 || cfg.DB.Connections[0].Name != "prod" {
		t.Fatalf("after refused add = %+v, want only prod", cfg.DB.Connections)
	}
}

func TestRemoveDBConnection(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	writeDBTestConfig(t, home, `{}`)

	for _, name := range []string{"prod", "stage"} {
		if err := addDB(t, DBConnection{Name: name, Driver: DBDriverPostgres, EncryptedURL: "ENCv1:argon2:a:b:c"}); err != nil {
			t.Fatalf("AddDBConnection %s: %v", name, err)
		}
	}
	if err := RemoveDBConnection("prod"); err != nil {
		t.Fatalf("RemoveDBConnection: %v", err)
	}
	cfg, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	if len(cfg.DB.Connections) != 1 || cfg.DB.Connections[0].Name != "stage" {
		t.Fatalf("after remove = %+v", cfg.DB.Connections)
	}
	if err := RemoveDBConnection("missing"); err == nil {
		t.Fatal("removing an unknown name succeeded")
	}
}
